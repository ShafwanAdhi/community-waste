package domain

import "errors"

var (
	ErrCommitUncertain     = errors.New("confirmation commit outcome is uncertain")
	ErrProofTooLarge       = errors.New("proof file exceeds 5 MiB")
	ErrUnsupportedProof    = errors.New("proof must be JPEG, PNG, or PDF")
	ErrPaymentNotPending   = errors.New("only pending payment can be confirmed")
	ErrHouseholdMismatch   = errors.New("payment household does not own the pickup")
	ErrPaymentExists       = errors.New("pickup already has a payment")
	ErrPaymentConflict     = errors.New("existing payment is inconsistent with pickup billing")
	ErrNotFound            = errors.New("resource not found")
	ErrHouseholdInUse      = errors.New("household has related records")
	ErrInvalidID           = errors.New("id must be a UUID")
	ErrInvalidPagination   = errors.New("invalid pagination")
	ErrInvalidFilter       = errors.New("invalid filter")
	ErrPendingPayment      = errors.New("household has pending payment")
	ErrInvalidTransition   = errors.New("pickup status does not allow this operation")
	ErrSafetyCheckRequired = errors.New("electronic pickup requires a successful safety check")
	ErrPaymentAlreadyPaid  = errors.New("paid pickup cannot be canceled")
)

type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	return "input validation failed"
}
