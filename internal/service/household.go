package service

import (
	"context"
	"encoding/hex"
	"math"
	"strings"

	"community-waste/internal/domain"
)

type HouseholdRepository interface {
	Create(context.Context, string, string) (domain.Household, error)
	List(context.Context, domain.Pagination) (domain.HouseholdPage, error)
	Get(context.Context, string) (domain.Household, error)
	Delete(context.Context, string) error
}

type Household struct {
	repository HouseholdRepository
}

func NewHousehold(repository HouseholdRepository) *Household {
	return &Household{repository: repository}
}

func (s *Household) Create(ctx context.Context, ownerName, address string) (domain.Household, error) {
	ownerName = strings.TrimSpace(ownerName)
	address = strings.TrimSpace(address)
	fields := make(map[string]string)
	if ownerName == "" {
		fields["owner_name"] = "Owner name is required"
	} else if strings.ContainsRune(ownerName, '\x00') {
		fields["owner_name"] = "Owner name must not contain null characters"
	}
	if address == "" {
		fields["address"] = "Address is required"
	} else if strings.ContainsRune(address, '\x00') {
		fields["address"] = "Address must not contain null characters"
	}
	if len(fields) > 0 {
		return domain.Household{}, &domain.ValidationError{Fields: fields}
	}
	return s.repository.Create(ctx, ownerName, address)
}

func (s *Household) List(ctx context.Context, pagination domain.Pagination) (domain.HouseholdPage, error) {
	if pagination.Page < 1 || pagination.Limit < 1 || pagination.Limit > 100 ||
		pagination.Page-1 > math.MaxInt64/pagination.Limit {
		return domain.HouseholdPage{}, domain.ErrInvalidPagination
	}
	return s.repository.List(ctx, pagination)
}

func (s *Household) Get(ctx context.Context, id string) (domain.Household, error) {
	if !validUUID(id) {
		return domain.Household{}, domain.ErrInvalidID
	}
	return s.repository.Get(ctx, id)
}

func (s *Household) Delete(ctx context.Context, id string) error {
	if !validUUID(id) {
		return domain.ErrInvalidID
	}
	return s.repository.Delete(ctx, id)
}

func validUUID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(id, "-", ""))
	return err == nil && len(decoded) == 16
}
