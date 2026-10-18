package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"community-waste/internal/domain"
)

type response struct {
	Data any             `json:"data"`
	Meta *paginationMeta `json:"meta,omitempty"`
}

type paginationMeta struct {
	Page  int64 `json:"page"`
	Limit int64 `json:"limit"`
	Total int64 `json:"total"`
}

type errorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message}})
}

func writeFailure(w http.ResponseWriter, r *http.Request, err error, logger *slog.Logger) {
	var validation *domain.ValidationError
	var request *requestError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusUnprocessableEntity, errorResponse{Error: apiError{
			Code: "validation_failed", Message: "Input validation failed", Fields: validation.Fields,
		}})
	case errors.As(err, &request):
		writeError(w, request.status, request.code, request.message)
	case errors.Is(err, domain.ErrInvalidID):
		writeError(w, http.StatusBadRequest, "invalid_id", "ID must be a hyphenated UUID")
	case errors.Is(err, domain.ErrInvalidPagination):
		writeError(w, http.StatusBadRequest, "invalid_pagination", "Page and limit must be positive integers; limit must not exceed 100 and offset must fit int64")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Resource was not found")
	case errors.Is(err, domain.ErrHouseholdInUse):
		writeError(w, http.StatusConflict, "household_in_use", "Household has related pickups or payments")
	default:
		logger.ErrorContext(r.Context(), "request failed", "method", r.Method, "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "An internal error occurred")
	}
}

func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method is not allowed")
	}
}
