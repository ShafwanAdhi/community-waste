package postgres

import (
	"context"
	"errors"
	"fmt"

	"community-waste/internal/domain"
	"community-waste/internal/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Pickup struct{ pool *pgxpool.Pool }

func NewPickup(pool *pgxpool.Pool) *Pickup { return &Pickup{pool: pool} }

const pickupColumns = `id::text, household_id::text, type, status, pickup_date, safety_check, created_at, updated_at`

func (r *Pickup) HouseholdID(ctx context.Context, id string) (string, error) {
	var householdID string
	err := r.pool.QueryRow(ctx, "SELECT household_id::text FROM pickups WHERE id=$1", id).Scan(&householdID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get pickup household: %w", err)
	}
	return householdID, nil
}

func (r *Pickup) WithinTransaction(ctx context.Context, fn func(service.PickupTx) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error { return fn(pickupTransaction{tx: tx}) })
}

func (r *Pickup) List(ctx context.Context, filter domain.PickupFilter) (domain.PickupPage, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.PickupPage{}, fmt.Errorf("begin pickup list: %w", err)
	}
	defer tx.Rollback(context.Background())
	var household any
	if filter.HouseholdID != "" {
		household = filter.HouseholdID
	}
	const where = ` WHERE ($1::text='' OR status=$1) AND ($2::uuid IS NULL OR household_id=$2)`
	page := domain.PickupPage{Items: make([]domain.Pickup, 0)}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM pickups"+where, string(filter.Status), household).Scan(&page.Total); err != nil {
		return domain.PickupPage{}, fmt.Errorf("count pickups: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+pickupColumns+" FROM pickups"+where+" ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4", string(filter.Status), household, filter.Pagination.Limit, (filter.Pagination.Page-1)*filter.Pagination.Limit)
	if err != nil {
		return domain.PickupPage{}, fmt.Errorf("list pickups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPickup(rows)
		if err != nil {
			return domain.PickupPage{}, fmt.Errorf("read pickup: %w", err)
		}
		page.Items = append(page.Items, p)
	}
	if err := rows.Err(); err != nil {
		return domain.PickupPage{}, fmt.Errorf("read pickups: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PickupPage{}, fmt.Errorf("commit pickup list: %w", err)
	}
	return page, nil
}

func scanPickup(row rowScanner) (domain.Pickup, error) {
	var p domain.Pickup
	err := row.Scan(&p.ID, &p.HouseholdID, &p.Type, &p.Status, &p.PickupDate, &p.SafetyCheck, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return domain.Pickup{}, err
	}
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if p.PickupDate != nil {
		date := p.PickupDate.UTC()
		p.PickupDate = &date
	}
	return p, nil
}
