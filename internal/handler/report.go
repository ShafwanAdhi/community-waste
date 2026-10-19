package handler

import (
	"context"
	"log/slog"
	"net/http"

	"community-waste/internal/domain"
)

type ReportService interface {
	WasteSummary(context.Context) ([]domain.WasteSummary, error)
	PaymentSummary(context.Context) (domain.PaymentSummary, error)
}

type reportHandler struct {
	service ReportService
	logger  *slog.Logger
}

func (h reportHandler) waste(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.WasteSummary(r.Context())
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	if summary == nil {
		summary = []domain.WasteSummary{}
	}
	writeJSON(w, http.StatusOK, response{Data: summary})
}

func (h reportHandler) payment(w http.ResponseWriter, r *http.Request) {
	summary, err := h.service.PaymentSummary(r.Context())
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: summary})
}
