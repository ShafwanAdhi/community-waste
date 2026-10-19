package domain

import "time"

type PaymentStatus string

const (
	PaymentPending PaymentStatus = "pending"
	PaymentPaid    PaymentStatus = "paid"
	PaymentFailed  PaymentStatus = "failed"
)

type Payment struct {
	ID           string        `json:"id"`
	HouseholdID  string        `json:"household_id"`
	WasteID      string        `json:"waste_id"`
	Amount       Money         `json:"amount"`
	PaymentDate  *time.Time    `json:"payment_date"`
	Status       PaymentStatus `json:"status"`
	ProofFileURL *string       `json:"proof_file_url"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

const MaxProofSize = 5 * 1024 * 1024

type PaymentReference struct {
	HouseholdID string
	WasteID     string
}

type PaymentFilter struct {
	DateFrom    *time.Time
	DateTo      *time.Time
	Pagination  Pagination
	Status      PaymentStatus
	HouseholdID string
}

type PaymentPage struct {
	Items []Payment
	Total int64
}
