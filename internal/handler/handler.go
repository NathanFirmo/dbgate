package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	mongoexecutor "dbgate/internal/executor/mongo"
	mysqlexecutor "dbgate/internal/executor/mysql"
)

type Handler struct {
	mysql *mysqlexecutor.Executor
	mongo *mongoexecutor.Executor
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
	handler := &Handler{mysql: mysql, mongo: mongo}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handler.handleHealth)
	mux.HandleFunc("/exec", handler.handleExec)
	mux.HandleFunc("/query", handler.handleQuery)

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

	body, err := readBody(r)
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

	body, err := readBody(r)
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
	var request databaseRequest
	if err := decodeJSON(body, &request); err != nil {
		return targetDatabase{}, err
	}

	parts := strings.SplitN(strings.TrimSpace(request.DB), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return targetDatabase{}, fmt.Errorf("db must be in the format type:name")
	}

	return targetDatabase{
		engine: strings.ToLower(parts[0]),
		name:   parts[1],
	}, nil
}

func readBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	return body, nil
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
