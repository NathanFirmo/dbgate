package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	mysqlDriver "github.com/go-sql-driver/mysql"

	"dbgate/internal/config"
)

var ErrUnknownDatabase = errors.New("unknown mysql database")

type Executor struct {
	dbs map[string]*sql.DB
}

func New(databases []config.Database) (*Executor, error) {
	executor := &Executor{dbs: make(map[string]*sql.DB)}

	for _, database := range databases {
		if database.Type != "mysql" {
			continue
		}

		dsn, err := normalizeDSN(database.DSN)
		if err != nil {
			_ = executor.Close()
			return nil, fmt.Errorf("normalize dsn for %s: %w", database.Identifier(), err)
		}

		db, err := sql.Open("mysql", dsn)
		if err != nil {
			_ = executor.Close()
			return nil, fmt.Errorf("open %s: %w", database.Identifier(), err)
		}

		db.SetConnMaxLifetime(5 * time.Minute)
		db.SetMaxIdleConns(5)
		db.SetMaxOpenConns(10)

		executor.dbs[database.Name] = db
	}

	return executor, nil
}

func (e *Executor) Exec(ctx context.Context, name string, statement string) error {
	db, err := e.database(name)
	if err != nil {
		return err
	}

	if _, err := db.ExecContext(ctx, statement); err != nil {
		return fmt.Errorf("execute mysql statement: %w", err)
	}

	return nil
}

func (e *Executor) Query(ctx context.Context, name string, statement string) ([]map[string]any, error) {
	db, err := e.database(name)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, statement)
	if err != nil {
		return nil, fmt.Errorf("query mysql: %w", err)
	}
	defer rows.Close()

	resultRows, err := scanRows(rows)
	if err != nil {
		return nil, err
	}

	for rows.NextResultSet() {
		nextRows, err := scanRows(rows)
		if err != nil {
			return nil, err
		}
		resultRows = append(resultRows, nextRows...)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mysql rows: %w", err)
	}

	return resultRows, nil
}

func (e *Executor) Close() error {
	var closeErr error
	for _, db := range e.dbs {
		if err := db.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (e *Executor) database(name string) (*sql.DB, error) {
	db, ok := e.dbs[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownDatabase, name)
	}
	return db, nil
}

func normalizeDSN(raw string) (string, error) {
	cfg, err := mysqlDriver.ParseDSN(raw)
	if err != nil {
		return "", err
	}

	cfg.MultiStatements = true
	cfg.ParseTime = true

	return cfg.FormatDSN(), nil
}

func scanRows(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("read mysql columns: %w", err)
	}

	resultRows := make([]map[string]any, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}

		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan mysql row: %w", err)
		}

		rowMap := make(map[string]any, len(columns))
		for i, column := range columns {
			rowMap[column] = normalizeValue(values[i])
		}
		resultRows = append(resultRows, rowMap)
	}

	return resultRows, nil
}

func normalizeValue(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	default:
		return typed
	}
}
