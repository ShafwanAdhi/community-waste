package handler

import (
	"context"
	"log/slog"
	"net/http"
)

func New(check func(context.Context) error, logger *slog.Logger, households HouseholdService) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler(check, logger))
	mux.HandleFunc("/healthz", methodNotAllowed("GET, HEAD"))
	h := householdHandler{service: households, logger: logger}
	mux.HandleFunc("POST /api/households", h.create)
	mux.HandleFunc("GET /api/households", h.list)
	mux.HandleFunc("GET /api/households/{id}", h.get)
	mux.HandleFunc("DELETE /api/households/{id}", h.delete)
	mux.HandleFunc("/api/households", methodNotAllowed("GET, HEAD, POST"))
	mux.HandleFunc("/api/households/{id}", methodNotAllowed("DELETE, GET, HEAD"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint was not found")
	})
	return mux
}
