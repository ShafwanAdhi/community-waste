package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func New(check func(context.Context) error, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := check(ctx); err != nil {
			logger.WarnContext(r.Context(), "database health check failed", "error", err)
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "Database is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint was not found")
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message}})
}
