package domain

import "time"

type Household struct {
	ID        string    `json:"id"`
	OwnerName string    `json:"owner_name"`
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type HouseholdPage struct {
	Items []Household
	Total int64
}
