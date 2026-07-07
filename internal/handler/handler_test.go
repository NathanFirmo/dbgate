package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mongoexecutor "dbgate/internal/executor/mongo"
	mysqlexecutor "dbgate/internal/executor/mysql"
)

type stubMySQLExecutor struct {
	execStatement  string
	queryStatement string
	execErr        error
	queryErr       error
	queryRows      []map[string]any
}

func (s *stubMySQLExecutor) Exec(_ context.Context, _ string, statement string) error {
	s.execStatement = statement
	return s.execErr
}

func (s *stubMySQLExecutor) Query(_ context.Context, _ string, statement string) ([]map[string]any, error) {
	s.queryStatement = statement
	return s.queryRows, s.queryErr
}

type stubMongoExecutor struct{}

func (s *stubMongoExecutor) Exec(context.Context, string, mongoexecutor.ExecRequest) (map[string]any, error) {
	return nil, nil
}

func (s *stubMongoExecutor) Query(context.Context, string, mongoexecutor.QueryRequest) ([]map[string]any, error) {
	return nil, nil
}

func TestExecFileRunsMySQLStatement(t *testing.T) {
	mysql := &stubMySQLExecutor{}
	server := NewWithExecutors(mysql, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/exec-file?db=mysql:platform", strings.NewReader("DELETE FROM foo;\nINSERT INTO foo VALUES (1);"))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if mysql.execStatement != "DELETE FROM foo;\nINSERT INTO foo VALUES (1);" {
		t.Fatalf("unexpected statement: %q", mysql.execStatement)
	}
}

func TestQueryAcceptsMySQLJSONBody(t *testing.T) {
	mysql := &stubMySQLExecutor{
		queryRows: []map[string]any{{"ok": 1}},
	}
	server := NewWithExecutors(mysql, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(`{"db":"mysql:platform","sql":"SELECT 1 AS ok"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", response.Code, response.Body.String())
	}
	if mysql.queryStatement != "SELECT 1 AS ok" {
		t.Fatalf("unexpected query statement: %q", mysql.queryStatement)
	}
}

func TestExecAcceptsMySQLJSONBody(t *testing.T) {
	mysql := &stubMySQLExecutor{}
	server := NewWithExecutors(mysql, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"db":"mysql:platform","sql":"DELETE FROM foo"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", response.Code, response.Body.String())
	}
	if mysql.execStatement != "DELETE FROM foo" {
		t.Fatalf("unexpected exec statement: %q", mysql.execStatement)
	}
}

func TestQueryFileReturnsRows(t *testing.T) {
	mysql := &stubMySQLExecutor{
		queryRows: []map[string]any{{"total": 2}},
	}
	server := NewWithExecutors(mysql, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/query-file?db=mysql:platform", strings.NewReader("SELECT COUNT(*) AS total FROM foo;"))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}

	var payload struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(payload.Rows))
	}
}

func TestExecFileRejectsNonMySQLTarget(t *testing.T) {
	server := NewWithExecutors(&stubMySQLExecutor{}, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/exec-file?db=mongo:notifications", strings.NewReader("ignored"))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func TestExecFilePropagatesDatabaseErrors(t *testing.T) {
	mysql := &stubMySQLExecutor{
		execErr: errors.New("boom"),
	}
	server := NewWithExecutors(mysql, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/exec-file?db=mysql:platform", strings.NewReader("DELETE FROM foo;"))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", response.Code)
	}
}

func TestExecFileRejectsOversizedBody(t *testing.T) {
	server := NewWithExecutors(&stubMySQLExecutor{}, &stubMongoExecutor{})

	request := httptest.NewRequest(
		http.MethodPost,
		"/exec-file?db=mysql:platform",
		strings.NewReader(strings.Repeat("x", maxSQLBodyBytes+1)),
	)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}

func TestExecFileReturnsBadRequestForUnknownDatabase(t *testing.T) {
	mysql := &stubMySQLExecutor{
		execErr: mysqlexecutor.ErrUnknownDatabase,
	}
	server := NewWithExecutors(mysql, &stubMongoExecutor{})

	request := httptest.NewRequest(http.MethodPost, "/exec-file?db=mysql:missing", strings.NewReader("DELETE FROM foo;"))
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}
