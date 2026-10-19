package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"community-waste/internal/domain"
)

type ConfirmationService interface {
	Confirm(context.Context, string, []byte) (domain.Payment, error)
}

type confirmationHandler struct {
	service ConfirmationService
	logger  *slog.Logger
}

func (h confirmationHandler) confirm(w http.ResponseWriter, r *http.Request) {
	content, err := readProof(w, r)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	payment, err := h.service.Confirm(r.Context(), r.PathValue("id"), content)
	if err != nil {
		writeFailure(w, r, err, h.logger)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: payment})
}

func readProof(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "multipart/form-data" {
		return nil, &requestError{415, "unsupported_media_type", "Content-Type must be multipart/form-data"}
	}
	const maxBody = domain.MaxProofSize + 64*1024
	if r.ContentLength > maxBody {
		return nil, &requestError{413, "body_too_large", "Multipart body exceeds allowed size"}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	defer r.Body.Close()
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, &requestError{400, "invalid_multipart", "Invalid multipart body or boundary"}
	}
	var content []byte
	found := false
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, multipartFailure(err)
		}
		if part.FormName() != "proof_file" || found {
			return nil, &requestError{400, "invalid_multipart", "Provide exactly one proof_file file and no other fields"}
		}
		if part.FileName() == "" {
			return nil, &domain.ValidationError{Fields: map[string]string{"proof_file": "Proof file must be a file upload"}}
		}
		found = true
		content, err = io.ReadAll(io.LimitReader(part, domain.MaxProofSize+1))
		if err != nil {
			return nil, multipartFailure(err)
		}
		if len(content) > domain.MaxProofSize {
			return nil, domain.ErrProofTooLarge
		}
		if err := part.Close(); err != nil {
			return nil, multipartFailure(err)
		}
	}
	// Multipart EOF can precede an epilogue. Consume it under the same body cap.
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return nil, multipartFailure(err)
	}
	if !found || len(content) == 0 {
		return nil, &domain.ValidationError{Fields: map[string]string{"proof_file": "Proof file is required"}}
	}
	return content, nil
}

func multipartFailure(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return &requestError{413, "body_too_large", "Multipart body exceeds allowed size"}
	}
	return &requestError{400, "invalid_multipart", "Invalid or incomplete multipart body"}
}
