package handler

import (
	"context"
	"log/slog"
	"net/http"

	"community-waste/internal/domain"
)

type HouseholdService interface {
	Create(context.Context, string, string) (domain.Household, error)
	List(context.Context, domain.Pagination) (domain.HouseholdPage, error)
	Get(context.Context, string) (domain.Household, error)
	Delete(context.Context, string) error
}

type householdHandler struct {
	service HouseholdService
	logger  *slog.Logger
}

func (h householdHandler) create(w http.ResponseWriter, r *http.Request) {
	var input struct {
		OwnerName string `json:"owner_name"`
		Address   string `json:"address"`
	}
	if err := readJSON(w, r, &input); err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	household, err := h.service.Create(r.Context(), input.OwnerName, input.Address)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	w.Header().Set("Location", "/api/households/"+household.ID)
	writeJSON(w, http.StatusCreated, response{Data: household})
}

func (h householdHandler) list(w http.ResponseWriter, r *http.Request) {
	pagination, err := parsePagination(r.URL.RawQuery)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	page, err := h.service.List(r.Context(), pagination)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	if page.Items == nil {
		page.Items = []domain.Household{}
	}
	writeJSON(w, http.StatusOK, response{
		Data: page.Items,
		Meta: &paginationMeta{Page: pagination.Page, Limit: pagination.Limit, Total: page.Total},
	})
}

func (h householdHandler) get(w http.ResponseWriter, r *http.Request) {
	household, err := h.service.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: household})
}

func (h householdHandler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
