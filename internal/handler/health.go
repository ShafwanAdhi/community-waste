package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

func healthHandler(check func(context.Context) error, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := check(ctx); err != nil {
			logger.WarnContext(r.Context(), "database health check failed", "error", err)
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "Database is unavailable")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"status": "ok"}})
	}
}
