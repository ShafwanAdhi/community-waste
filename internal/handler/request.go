package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"community-waste/internal/domain"
)

type requestError struct {
	status  int
	code    string
	message string
}

func (e *requestError) Error() string {
	return e.message
}

func readJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &requestError{http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json"}
	}
	body := http.MaxBytesReader(w, r.Body, 64*1024)
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			return &requestError{http.StatusRequestEntityTooLarge, "body_too_large", "JSON body must not exceed 64 KiB"}
		}
		return &requestError{http.StatusBadRequest, "invalid_json", "Could not read JSON body"}
	}
	data = bytes.Trim(data, " \t\r\n")
	invalid := &requestError{http.StatusBadRequest, "invalid_json", "Body must contain one valid JSON object with known fields"}
	if len(data) == 0 || data[0] != '{' {
		return invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return invalid
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return invalid
	}
	return nil
}

func parsePagination(rawQuery string) (domain.Pagination, error) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return domain.Pagination{}, domain.ErrInvalidPagination
	}
	pagination := domain.Pagination{Page: 1, Limit: 20}
	for _, parameter := range []struct {
		name   string
		target *int64
	}{{"page", &pagination.Page}, {"limit", &pagination.Limit}} {
		values, present := query[parameter.name]
		if !present {
			continue
		}
		if len(values) != 1 || values[0] == "" {
			return domain.Pagination{}, domain.ErrInvalidPagination
		}
		value, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil {
			return domain.Pagination{}, domain.ErrInvalidPagination
		}
		*parameter.target = value
	}
	return pagination, nil
}
