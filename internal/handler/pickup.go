package handler

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"community-waste/internal/domain"
)

type PickupService interface {
	Create(context.Context, string, domain.WasteType, *bool) (domain.Pickup, error)
	List(context.Context, domain.PickupFilter) (domain.PickupPage, error)
	Schedule(context.Context, string, time.Time, *bool) (domain.Pickup, error)
	Cancel(context.Context, string) (domain.Pickup, error)
}

type pickupHandler struct {
	service PickupService
	logger  *slog.Logger
}

func (h pickupHandler) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		HouseholdID string           `json:"household_id"`
		Type        domain.WasteType `json:"type"`
		SafetyCheck *bool            `json:"safety_check"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	pickup, err := h.service.Create(r.Context(), input.HouseholdID, input.Type, input.SafetyCheck)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusCreated, response{Data: pickup})
}

func (h pickupHandler) list(w http.ResponseWriter, r *http.Request) {
	pagination, err := parsePagination(r.URL.RawQuery)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	// parsePagination has already rejected malformed query escaping.
	query, _ := url.ParseQuery(r.URL.RawQuery)
	filter := domain.PickupFilter{Pagination: pagination}
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
			filter.Status = domain.PickupStatus(values[0])
		} else {
			filter.HouseholdID = values[0]
		}
	}
	page, err := h.service.List(r.Context(), filter)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	if page.Items == nil {
		page.Items = []domain.Pickup{}
	}
	writeJSON(w, http.StatusOK, response{Data: page.Items, Meta: &paginationMeta{Page: pagination.Page, Limit: pagination.Limit, Total: page.Total}})
}

func (h pickupHandler) schedule(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PickupDate  string `json:"pickup_date"`
		SafetyCheck *bool  `json:"safety_check"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	date, err := parseTimestamp(input.PickupDate)
	if err != nil {
		writeFailure(w, r, &domain.ValidationError{Fields: map[string]string{"pickup_date": "Pickup date must use RFC3339"}}, h.logger)
		return
	}
	pickup, err := h.service.Schedule(r.Context(), r.PathValue("id"), date, input.SafetyCheck)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: pickup})
}

func (h pickupHandler) cancel(w http.ResponseWriter, r *http.Request) {
	pickup, err := h.service.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: pickup})
}
