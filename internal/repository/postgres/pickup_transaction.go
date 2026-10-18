package postgres

import (
	"context"
	"errors"
	"fmt"

	"community-waste/internal/domain"
	"github.com/jackc/pgx/v5"
)

type pickupTransaction struct{ tx pgx.Tx }

func (r pickupTransaction) LockHousehold(ctx context.Context, id string) error {
	var locked string
	err := r.tx.QueryRow(ctx, "SELECT id::text FROM households WHERE id=$1 FOR UPDATE", id).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock household: %w", err)
	}
	return nil
}

func (r pickupTransaction) PickupForUpdate(ctx context.Context, id string) (domain.Pickup, error) {
	p, err := scanPickup(r.tx.QueryRow(ctx, "SELECT "+pickupColumns+" FROM pickups WHERE id=$1 FOR UPDATE", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Pickup{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Pickup{}, fmt.Errorf("lock pickup: %w", err)
	}
	return p, nil
}

func (r pickupTransaction) HasPendingPayment(ctx context.Context, householdID string) (bool, error) {
	var pending bool
	err := r.tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM payments WHERE household_id=$1 AND status='pending')", householdID).Scan(&pending)
	if err != nil {
		return false, fmt.Errorf("check pending payment: %w", err)
	}
	return pending, nil
}

func (r pickupTransaction) InsertPickup(ctx context.Context, householdID string, kind domain.WasteType, safety *bool) (domain.Pickup, error) {
	p, err := scanPickup(r.tx.QueryRow(ctx, `INSERT INTO pickups(id,household_id,type,safety_check) VALUES(gen_random_uuid(),$1,$2,$3) RETURNING `+pickupColumns, householdID, kind, safety))
	if err != nil {
		return domain.Pickup{}, fmt.Errorf("create pickup: %w", err)
	}
	return p, nil
}

func (r pickupTransaction) UpdatePickup(ctx context.Context, p domain.Pickup) (domain.Pickup, error) {
	updated, err := scanPickup(r.tx.QueryRow(ctx, `UPDATE pickups SET status=$2,pickup_date=$3,safety_check=$4,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+pickupColumns, p.ID, p.Status, p.PickupDate, p.SafetyCheck))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Pickup{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Pickup{}, fmt.Errorf("update pickup: %w", err)
	}
	return updated, nil
}

func (r pickupTransaction) PaymentStatusForUpdate(ctx context.Context, pickupID string) (domain.PaymentStatus, error) {
	var status domain.PaymentStatus
	err := r.tx.QueryRow(ctx, "SELECT status FROM payments WHERE waste_id=$1 FOR UPDATE", pickupID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lock pickup payment: %w", err)
	}
	return status, nil
}

func (r pickupTransaction) FailPendingPayment(ctx context.Context, pickupID string) error {
	_, err := r.tx.Exec(ctx, "UPDATE payments SET status='failed',updated_at=clock_timestamp() WHERE waste_id=$1 AND status='pending'", pickupID)
	if err != nil {
		return fmt.Errorf("fail canceled pickup payment: %w", err)
	}
	return nil
}
