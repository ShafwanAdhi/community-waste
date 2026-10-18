package postgres

import (
	"context"
	"errors"
	"fmt"

	"community-waste/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Household struct {
	pool *pgxpool.Pool
}

func NewHousehold(pool *pgxpool.Pool) *Household {
	return &Household{pool: pool}
}

func (r *Household) Create(ctx context.Context, ownerName, address string) (domain.Household, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO households (id, owner_name, address)
		VALUES (gen_random_uuid(), $1, $2)
		RETURNING id::text, owner_name, address, created_at, updated_at`, ownerName, address)
	household, err := scanHousehold(row)
	if err != nil {
		return domain.Household{}, fmt.Errorf("create household: %w", err)
	}
	return household, nil
}

func (r *Household) Get(ctx context.Context, id string) (domain.Household, error) {
	household, err := scanHousehold(r.pool.QueryRow(ctx, `
		SELECT id::text, owner_name, address, created_at, updated_at
		FROM households WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Household{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Household{}, fmt.Errorf("get household: %w", err)
	}
	return household, nil
}

func (r *Household) List(ctx context.Context, pagination domain.Pagination) (domain.HouseholdPage, error) {
	// Count and rows must describe the same snapshot, including out-of-range pages.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.HouseholdPage{}, fmt.Errorf("begin household list: %w", err)
	}
	defer tx.Rollback(context.Background())
	page := domain.HouseholdPage{Items: make([]domain.Household, 0)}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM households").Scan(&page.Total); err != nil {
		return domain.HouseholdPage{}, fmt.Errorf("count households: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, owner_name, address, created_at, updated_at
		FROM households ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2`,
		pagination.Limit, (pagination.Page-1)*pagination.Limit)
	if err != nil {
		return domain.HouseholdPage{}, fmt.Errorf("list households: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		household, err := scanHousehold(rows)
		if err != nil {
			return domain.HouseholdPage{}, fmt.Errorf("read household: %w", err)
		}
		page.Items = append(page.Items, household)
	}
	if err := rows.Err(); err != nil {
		return domain.HouseholdPage{}, fmt.Errorf("read households: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.HouseholdPage{}, fmt.Errorf("commit household list: %w", err)
	}
	return page, nil
}

func (r *Household) Delete(ctx context.Context, id string) error {
	result, err := r.pool.Exec(ctx, "DELETE FROM households WHERE id = $1", id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "23001" || pgErr.Code == "23503") {
			return domain.ErrHouseholdInUse
		}
		return fmt.Errorf("delete household: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanHousehold(row rowScanner) (domain.Household, error) {
	var household domain.Household
	err := row.Scan(&household.ID, &household.OwnerName, &household.Address, &household.CreatedAt, &household.UpdatedAt)
	if err != nil {
		return domain.Household{}, err
	}
	household.CreatedAt = household.CreatedAt.UTC()
	household.UpdatedAt = household.UpdatedAt.UTC()
	return household, nil
}
