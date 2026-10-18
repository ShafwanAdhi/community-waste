package service

import (
	"context"
	"time"

	"community-waste/internal/domain"
)

type PickupRepository interface {
	HouseholdID(context.Context, string) (string, error)
	List(context.Context, domain.PickupFilter) (domain.PickupPage, error)
	WithinTransaction(context.Context, func(PickupTx) error) error
}

type PickupTx interface {
	LockHousehold(context.Context, string) error
	PickupForUpdate(context.Context, string) (domain.Pickup, error)
	HasPendingPayment(context.Context, string) (bool, error)
	InsertPickup(context.Context, string, domain.WasteType, *bool) (domain.Pickup, error)
	UpdatePickup(context.Context, domain.Pickup) (domain.Pickup, error)
	// An empty status means that the pickup has no payment.
	PaymentStatusForUpdate(context.Context, string) (domain.PaymentStatus, error)
	FailPendingPayment(context.Context, string) error
}

type Pickup struct {
	repository PickupRepository
	now        func() time.Time
}

func NewPickup(repository PickupRepository) *Pickup {
	return &Pickup{repository: repository, now: time.Now}
}

func (s *Pickup) Create(ctx context.Context, householdID string, kind domain.WasteType, safety *bool) (domain.Pickup, error) {
	fields := make(map[string]string)
	if !validUUID(householdID) {
		fields["household_id"] = "Household ID must be a hyphenated UUID"
	}
	if !validWasteType(kind) {
		fields["type"] = "Type must be organic, plastic, paper, or electronic"
	}
	if kind == domain.WasteElectronic && safety == nil {
		fields["safety_check"] = "Safety check is required for electronic pickups"
	}
	if len(fields) > 0 {
		return domain.Pickup{}, &domain.ValidationError{Fields: fields}
	}
	var pickup domain.Pickup
	err := s.repository.WithinTransaction(ctx, func(tx PickupTx) error {
		if err := tx.LockHousehold(ctx, householdID); err != nil {
			return err
		}
		pending, err := tx.HasPendingPayment(ctx, householdID)
		if err != nil {
			return err
		}
		if pending {
			return domain.ErrPendingPayment
		}
		pickup, err = tx.InsertPickup(ctx, householdID, kind, safety)
		return err
	})
	if err != nil {
		return domain.Pickup{}, err
	}
	return pickup, nil
}

func (s *Pickup) List(ctx context.Context, filter domain.PickupFilter) (domain.PickupPage, error) {
	if !validPagination(filter.Pagination) {
		return domain.PickupPage{}, domain.ErrInvalidPagination
	}
	if (filter.Status != "" && !validPickupStatus(filter.Status)) ||
		(filter.HouseholdID != "" && !validUUID(filter.HouseholdID)) {
		return domain.PickupPage{}, domain.ErrInvalidFilter
	}
	return s.repository.List(ctx, filter)
}

func (s *Pickup) Schedule(ctx context.Context, id string, date time.Time, safety *bool) (domain.Pickup, error) {
	if !validUUID(id) {
		return domain.Pickup{}, domain.ErrInvalidID
	}
	if err := s.validatePickupDate(date); err != nil {
		return domain.Pickup{}, err
	}
	return s.withLockedPickup(ctx, id, func(tx PickupTx, pickup domain.Pickup) (domain.Pickup, error) {
		if err := s.validatePickupDate(date); err != nil {
			return domain.Pickup{}, err
		}
		if pickup.Status != domain.PickupPending {
			return domain.Pickup{}, domain.ErrInvalidTransition
		}
		if safety != nil {
			pickup.SafetyCheck = safety
		}
		if pickup.Type == domain.WasteElectronic && (pickup.SafetyCheck == nil || !*pickup.SafetyCheck) {
			return domain.Pickup{}, domain.ErrSafetyCheckRequired
		}
		utcDate := date.UTC()
		pickup.PickupDate = &utcDate
		pickup.Status = domain.PickupScheduled
		return tx.UpdatePickup(ctx, pickup)
	})
}

func (s *Pickup) Cancel(ctx context.Context, id string) (domain.Pickup, error) {
	if !validUUID(id) {
		return domain.Pickup{}, domain.ErrInvalidID
	}
	return s.withLockedPickup(ctx, id, func(tx PickupTx, pickup domain.Pickup) (domain.Pickup, error) {
		if pickup.Status != domain.PickupPending && pickup.Status != domain.PickupScheduled {
			return domain.Pickup{}, domain.ErrInvalidTransition
		}
		status, err := tx.PaymentStatusForUpdate(ctx, id)
		if err != nil {
			return domain.Pickup{}, err
		}
		if status == domain.PaymentPaid {
			return domain.Pickup{}, domain.ErrPaymentAlreadyPaid
		}
		if status == domain.PaymentPending {
			if err := tx.FailPendingPayment(ctx, id); err != nil {
				return domain.Pickup{}, err
			}
		}
		pickup.Status = domain.PickupCanceled
		return tx.UpdatePickup(ctx, pickup)
	})
}

func (s *Pickup) withLockedPickup(ctx context.Context, id string, change func(PickupTx, domain.Pickup) (domain.Pickup, error)) (domain.Pickup, error) {
	householdID, err := s.repository.HouseholdID(ctx, id)
	if err != nil {
		return domain.Pickup{}, err
	}
	var result domain.Pickup
	err = s.repository.WithinTransaction(ctx, func(tx PickupTx) error {
		if err := tx.LockHousehold(ctx, householdID); err != nil {
			return err
		}
		pickup, err := tx.PickupForUpdate(ctx, id)
		if err != nil {
			return err
		}
		result, err = change(tx, pickup)
		return err
	})
	if err != nil {
		return domain.Pickup{}, err
	}
	return result, nil
}

func (s *Pickup) validatePickupDate(date time.Time) error {
	if date.IsZero() || !date.After(s.now()) {
		return &domain.ValidationError{Fields: map[string]string{"pickup_date": "Pickup date must be in the future"}}
	}
	return nil
}

func validWasteType(kind domain.WasteType) bool {
	switch kind {
	case domain.WasteOrganic, domain.WastePlastic, domain.WastePaper, domain.WasteElectronic:
		return true
	default:
		return false
	}
}

func validPickupStatus(status domain.PickupStatus) bool {
	switch status {
	case domain.PickupPending, domain.PickupScheduled, domain.PickupCompleted, domain.PickupCanceled:
		return true
	default:
		return false
	}
}
