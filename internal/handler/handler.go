package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	mongoexecutor "dbgate/internal/executor/mongo"
	mysqlexecutor "dbgate/internal/executor/mysql"
)

const (
	maxJSONBodyBytes = 1 << 20
	maxSQLBodyBytes  = 16 << 20
)

type mysqlExecutor interface {
	Exec(ctx context.Context, name string, statement string) error
	Query(ctx context.Context, name string, statement string) ([]map[string]any, error)
}

type mongoExecutor interface {
	Exec(ctx context.Context, name string, request mongoexecutor.ExecRequest) (map[string]any, error)
	Query(ctx context.Context, name string, request mongoexecutor.QueryRequest) ([]map[string]any, error)
}

type Handler struct {
	mysql mysqlExecutor
	mongo mongoExecutor
}

type databaseRequest struct {
	DB string `json:"db"`
}

type mysqlRequest struct {
	DB  string `json:"db"`
	SQL string `json:"sql"`
}

type mongoExecRequest struct {
	DB         string           `json:"db"`
	Collection string           `json:"collection"`
	Op         string           `json:"op"`
	Filter     map[string]any   `json:"filter"`
	Document   map[string]any   `json:"document"`
	Documents  []map[string]any `json:"documents"`
	Update     map[string]any   `json:"update"`
}

type mongoQueryRequest struct {
	DB         string         `json:"db"`
	Collection string         `json:"collection"`
	Filter     map[string]any `json:"filter"`
}

func New(mysql *mysqlexecutor.Executor, mongo *mongoexecutor.Executor) http.Handler {
	return NewWithExecutors(mysql, mongo)
}

func NewWithExecutors(mysql mysqlExecutor, mongo mongoExecutor) http.Handler {
	handler := &Handler{mysql: mysql, mongo: mongo}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.handleHealth)
	mux.HandleFunc("/exec", handler.handleExec)
	mux.HandleFunc("/query", handler.handleQuery)
	mux.HandleFunc("/exec-file", handler.handleExecFile)
	mux.HandleFunc("/query-file", handler.handleQueryFile)

	return mux
}

func (h *Handler) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	body, err := readBody(r, maxJSONBodyBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	target, err := decodeDatabaseRequest(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	switch target.engine {
	case "mysql":
		var request mysqlRequest
		if err := decodeJSON(body, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if strings.TrimSpace(request.SQL) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sql is required"})
			return
		}
		if err := h.mysql.Exec(r.Context(), target.name, request.SQL); err != nil {
			writeExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "mongo":
		var request mongoExecRequest
		if err := decodeJSON(body, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		result, err := h.mongo.Exec(r.Context(), target.name, mongoexecutor.ExecRequest{
			Collection: request.Collection,
			Op:         request.Op,
			Filter:     request.Filter,
			Document:   request.Document,
			Documents:  request.Documents,
			Update:     request.Update,
		})
		if err != nil {
			writeExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("unsupported db type %q", target.engine)})
	}
}

func (h *Handler) handleQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	body, err := readBody(r, maxJSONBodyBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	target, err := decodeDatabaseRequest(body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	switch target.engine {
	case "mysql":
		var request mysqlRequest
		if err := decodeJSON(body, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		if strings.TrimSpace(request.SQL) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sql is required"})
			return
		}
		rows, err := h.mysql.Query(r.Context(), target.name, request.SQL)
		if err != nil {
			writeExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
	case "mongo":
		var request mongoQueryRequest
		if err := decodeJSON(body, &request); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		docs, err := h.mongo.Query(r.Context(), target.name, mongoexecutor.QueryRequest{
			Collection: request.Collection,
			Filter:     request.Filter,
		})
		if err != nil {
			writeExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"docs": docs})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("unsupported db type %q", target.engine)})
	}
}

type targetDatabase struct {
	engine string
	name   string
}

func decodeDatabaseRequest(body []byte) (targetDatabase, error) {
	var envelope map[string]json.RawMessage
	if err := decodeJSON(body, &envelope); err != nil {
		return targetDatabase{}, err
	}

	rawDB, ok := envelope["db"]
	if !ok {
		return targetDatabase{}, fmt.Errorf("db must be in the format type:name")
	}

	var request databaseRequest
	if err := json.Unmarshal(rawDB, &request.DB); err != nil {
		return targetDatabase{}, fmt.Errorf("invalid json body: json: cannot unmarshal db field: %w", err)
	}

	return decodeDatabaseTarget(request.DB)
}

func (h *Handler) handleExecFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	target, err := decodeDatabaseTarget(r.URL.Query().Get("db"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if target.engine != "mysql" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "file-based SQL endpoints support only mysql databases"})
		return
	}

	statement, err := readSQLBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if err := h.mysql.Exec(r.Context(), target.name, statement); err != nil {
		writeExecutionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleQueryFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}

	target, err := decodeDatabaseTarget(r.URL.Query().Get("db"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	if target.engine != "mysql" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "file-based SQL endpoints support only mysql databases"})
		return
	}

	statement, err := readSQLBody(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	rows, err := h.mysql.Query(r.Context(), target.name, statement)
	if err != nil {
		writeExecutionError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rows": rows})
}

func readBody(r *http.Request, maxBytes int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxBytes)
	}
	return body, nil
}

func readSQLBody(r *http.Request) (string, error) {
	body, err := readBody(r, maxSQLBodyBytes)
	if err != nil {
		return "", err
	}
	statement := strings.TrimSpace(string(body))
	if statement == "" {
		return "", fmt.Errorf("sql body is required")
	}
	return statement, nil
}

func decodeJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid json body: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("invalid json body: multiple JSON values are not allowed")
	}
	return nil
}

func decodeDatabaseTarget(raw string) (targetDatabase, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return targetDatabase{}, fmt.Errorf("db must be in the format type:name")
	}

	return targetDatabase{
		engine: strings.ToLower(parts[0]),
		name:   parts[1],
	}, nil
}

func writeExecutionError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, mysqlexecutor.ErrUnknownDatabase) ||
		errors.Is(err, mongoexecutor.ErrUnknownDatabase) ||
		errors.Is(err, mongoexecutor.ErrInvalidRequest) {
		status = http.StatusBadRequest
	}

	writeJSON(w, status, map[string]any{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
