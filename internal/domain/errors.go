package domain

import "errors"

var (
	ErrNotFound          = errors.New("resource not found")
	ErrHouseholdInUse    = errors.New("household has related records")
	ErrInvalidID         = errors.New("id must be a UUID")
	ErrInvalidPagination = errors.New("invalid pagination")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "input validation failed"
}
