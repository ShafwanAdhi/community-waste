package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"community-waste/internal/domain"
)

type BillingService interface {
	Create(context.Context, string, string, string) (domain.Payment, error)
	List(context.Context, domain.PaymentFilter) (domain.PaymentPage, error)
	Complete(context.Context, string) (domain.Pickup, error)
}

type billingHandler struct {
	service BillingService
	logger  *slog.Logger
}

func (h billingHandler) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		HouseholdID string `json:"household_id"`
		WasteID     string `json:"waste_id"`
		Amount      string `json:"amount"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	payment, err := h.service.Create(r.Context(), input.HouseholdID, input.WasteID, input.Amount)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusCreated, response{Data: payment})
}

func (h billingHandler) list(w http.ResponseWriter, r *http.Request) {
	pagination, err := parsePagination(r.URL.RawQuery)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	query, _ := url.ParseQuery(r.URL.RawQuery)
	filter := domain.PaymentFilter{Pagination: pagination}
	for _, name := range []string{"status", "household_id"} {
		values, present := query[name]
		if !present {
			continue
		}
		if len(values) != 1 || values[0] == "" {
			writeFailure(w, r, domain.ErrInvalidFilter, h.logger)
			return
		}
		if name == "status" {
			filter.Status = domain.PaymentStatus(values[0])
		} else {
			filter.HouseholdID = values[0]
		}
	}
	for _, bound := range []struct {
		name   string
		target **time.Time
	}{{"date_from", &filter.DateFrom}, {"date_to", &filter.DateTo}} {
		values, present := query[bound.name]
		if !present {
			continue
		}
		if len(values) != 1 || values[0] == "" {
			writeFailure(w, r, domain.ErrInvalidFilter, h.logger)
			return
		}
		date, err := parseTimestamp(values[0])
		if err != nil {
			writeFailure(w, r, domain.ErrInvalidFilter, h.logger)
			return
		}
		utc := date.UTC()
		*bound.target = &utc
	}
	page, err := h.service.List(r.Context(), filter)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	if page.Items == nil {
		page.Items = []domain.Payment{}
	}
	writeJSON(w, http.StatusOK, response{Data: page.Items, Meta: &paginationMeta{Page: pagination.Page, Limit: pagination.Limit, Total: page.Total}})
}

func (h billingHandler) complete(w http.ResponseWriter, r *http.Request) {
	pickup, err := h.service.Complete(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: pickup})
}
