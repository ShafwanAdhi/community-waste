package handler

import (
	"context"
	"log/slog"
	"net/http"
)

func New(check func(context.Context) error, logger *slog.Logger, households HouseholdService, pickups PickupService, billing BillingService, confirmation ConfirmationService) http.Handler {
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
	p := pickupHandler{service: pickups, logger: logger}
	mux.HandleFunc("POST /api/pickups", p.create)
	mux.HandleFunc("GET /api/pickups", p.list)
	mux.HandleFunc("PUT /api/pickups/{id}/schedule", p.schedule)
	mux.HandleFunc("PUT /api/pickups/{id}/cancel", p.cancel)
	mux.HandleFunc("/api/pickups", methodNotAllowed("GET, HEAD, POST"))
	mux.HandleFunc("/api/pickups/{id}/schedule", methodNotAllowed("PUT"))
	mux.HandleFunc("/api/pickups/{id}/cancel", methodNotAllowed("PUT"))
	b := billingHandler{service: billing, logger: logger}
	mux.HandleFunc("POST /api/payments", b.create)
	mux.HandleFunc("GET /api/payments", b.list)
	mux.HandleFunc("PUT /api/pickups/{id}/complete", b.complete)
	mux.HandleFunc("/api/payments", methodNotAllowed("GET, HEAD, POST"))
	mux.HandleFunc("/api/pickups/{id}/complete", methodNotAllowed("PUT"))
	c := confirmationHandler{service: confirmation, logger: logger}
	mux.HandleFunc("PUT /api/payments/{id}/confirm", c.confirm)
	mux.HandleFunc("/api/payments/{id}/confirm", methodNotAllowed("PUT"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint was not found")
	})
	return mux
}
