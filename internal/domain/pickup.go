package domain

import "time"

type WasteType string

const (
	WasteOrganic    WasteType = "organic"
	WastePlastic    WasteType = "plastic"
	WastePaper      WasteType = "paper"
	WasteElectronic WasteType = "electronic"
)

type PickupStatus string

const (
	PickupPending   PickupStatus = "pending"
	PickupScheduled PickupStatus = "scheduled"
	PickupCompleted PickupStatus = "completed"
	PickupCanceled  PickupStatus = "canceled"
)

type Pickup struct {
	ID          string       `json:"id"`
	HouseholdID string       `json:"household_id"`
	Type        WasteType    `json:"type"`
	Status      PickupStatus `json:"status"`
	PickupDate  *time.Time   `json:"pickup_date"`
	SafetyCheck *bool        `json:"safety_check"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type PickupFilter struct {
	Pagination  Pagination
	Status      PickupStatus
	HouseholdID string
}

type PickupPage struct {
	Items []Pickup
	Total int64
}
