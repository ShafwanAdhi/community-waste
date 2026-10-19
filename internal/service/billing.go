package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"community-waste/internal/domain"
)

type BillingRepository interface {
	HouseholdID(context.Context, string) (string, error)
	List(context.Context, domain.PaymentFilter) (domain.PaymentPage, error)
	WithinTransaction(context.Context, func(BillingTx) error) error
}

type BillingTx interface {
	LockHousehold(context.Context, string) error
	PickupForUpdate(context.Context, string) (domain.Pickup, error)
	UpdatePickup(context.Context, domain.Pickup) (domain.Pickup, error)
	PaymentForUpdate(context.Context, string) (domain.Payment, error)
	InsertPayment(context.Context, domain.Pickup, domain.Money) (domain.Payment, error)
}

type Billing struct{ repository BillingRepository }

func NewBilling(repository BillingRepository) *Billing { return &Billing{repository: repository} }

func (s *Billing) Create(ctx context.Context, householdID, wasteID, amount string) (domain.Payment, error) {
	fields := make(map[string]string)
	if !validUUID(householdID) {
		fields["household_id"] = "Household ID must be a hyphenated UUID"
	}
	if !validUUID(wasteID) {
		fields["waste_id"] = "Waste ID must be a hyphenated UUID"
	}
	money, err := domain.ParseMoney(amount)
	if err != nil {
		fields["amount"] = "Amount must be a positive decimal string with at most two fractional digits and ten integer digits"
	}
	if len(fields) > 0 {
		return domain.Payment{}, &domain.ValidationError{Fields: fields}
	}
	var payment domain.Payment
	err = s.withLockedPickup(ctx, wasteID, func(tx BillingTx, p domain.Pickup) error {
		if !strings.EqualFold(p.HouseholdID, householdID) {
			return domain.ErrHouseholdMismatch
		}
		if p.Status != domain.PickupPending && p.Status != domain.PickupScheduled {
			return domain.ErrInvalidTransition
		}
		tariff, err := pickupTariff(p.Type)
		if err != nil {
			return err
		}
		if money != tariff {
			return &domain.ValidationError{Fields: map[string]string{"amount": "Amount must match the pickup tariff: " + tariff.String()}}
		}
		if _, err := tx.PaymentForUpdate(ctx, p.ID); err == nil {
			return domain.ErrPaymentExists
		} else if !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		payment, err = tx.InsertPayment(ctx, p, money)
		return err
	})
	if err != nil {
		return domain.Payment{}, err
	}
	return payment, nil
}

func (s *Billing) Complete(ctx context.Context, id string) (domain.Pickup, error) {
	if !validUUID(id) {
		return domain.Pickup{}, domain.ErrInvalidID
	}
	var completed domain.Pickup
	err := s.withLockedPickup(ctx, id, func(tx BillingTx, p domain.Pickup) error {
		if p.Status != domain.PickupScheduled {
			return domain.ErrInvalidTransition
		}
		tariff, err := pickupTariff(p.Type)
		if err != nil {
			return err
		}
		payment, err := tx.PaymentForUpdate(ctx, p.ID)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			if _, err := tx.InsertPayment(ctx, p, tariff); err != nil {
				return err
			}
		case err != nil:
			return err
		case payment.Amount != tariff || (payment.Status != domain.PaymentPending && payment.Status != domain.PaymentPaid):
			return domain.ErrPaymentConflict
		}
		p.Status = domain.PickupCompleted
		completed, err = tx.UpdatePickup(ctx, p)
		return err
	})
	if err != nil {
		return domain.Pickup{}, err
	}
	return completed, nil
}

func (s *Billing) List(ctx context.Context, filter domain.PaymentFilter) (domain.PaymentPage, error) {
	if !validPagination(filter.Pagination) {
		return domain.PaymentPage{}, domain.ErrInvalidPagination
	}
	if (filter.Status != "" && !validPaymentStatus(filter.Status)) || (filter.HouseholdID != "" && !validUUID(filter.HouseholdID)) {
		return domain.PaymentPage{}, domain.ErrInvalidFilter
	}
	if filter.DateFrom != nil && filter.DateTo != nil && !filter.DateFrom.Before(*filter.DateTo) {
		return domain.PaymentPage{}, domain.ErrInvalidFilter
	}
	if filter.DateFrom != nil {
		date := paymentDateBound(*filter.DateFrom)
		filter.DateFrom = &date
	}
	if filter.DateTo != nil {
		date := paymentDateBound(*filter.DateTo)
		filter.DateTo = &date
	}
	return s.repository.List(ctx, filter)
}

func (s *Billing) withLockedPickup(ctx context.Context, id string, change func(BillingTx, domain.Pickup) error) error {
	householdID, err := s.repository.HouseholdID(ctx, id)
	if err != nil {
		return err
	}
	return s.repository.WithinTransaction(ctx, func(tx BillingTx) error {
		if err := tx.LockHousehold(ctx, householdID); err != nil {
			return err
		}
		p, err := tx.PickupForUpdate(ctx, id)
		if err != nil {
			return err
		}
		return change(tx, p)
	})
}

func pickupTariff(kind domain.WasteType) (domain.Money, error) {
	switch kind {
	case domain.WasteOrganic, domain.WastePlastic, domain.WastePaper:
		return 5000000, nil
	case domain.WasteElectronic:
		return 10000000, nil
	default:
		return 0, domain.ErrPaymentConflict
	}
}

func validPaymentStatus(status domain.PaymentStatus) bool {
	switch status {
	case domain.PaymentPending, domain.PaymentPaid, domain.PaymentFailed:
		return true
	default:
		return false
	}
}

// Stored timestamps are discrete microseconds. Ceiling preserves both >= and <
// comparisons when a caller supplies a finer, nanosecond boundary.
func paymentDateBound(date time.Time) time.Time {
	date = date.UTC()
	bound := date.Truncate(time.Microsecond)
	if bound.Before(date) {
		bound = bound.Add(time.Microsecond)
	}
	return bound
}
