package postgres

import (
	"context"
	"errors"
	"fmt"

	"community-waste/internal/domain"
	"community-waste/internal/service"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Billing struct {
	pool    *pgxpool.Pool
	pickups *Pickup
}

func NewBilling(pool *pgxpool.Pool) *Billing {
	return &Billing{pool: pool, pickups: NewPickup(pool)}
}

func (r *Billing) HouseholdID(ctx context.Context, id string) (string, error) {
	return r.pickups.HouseholdID(ctx, id)
}

func (r *Billing) WithinTransaction(ctx context.Context, fn func(service.BillingTx) error) error {
	return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		return fn(billingTransaction{pickupTransaction: pickupTransaction{tx: tx}})
	})
}

const paymentColumns = `id::text,household_id::text,waste_id::text,amount::text,payment_date,status,proof_file_url,created_at,updated_at`

func (r *Billing) List(ctx context.Context, filter domain.PaymentFilter) (domain.PaymentPage, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return domain.PaymentPage{}, fmt.Errorf("begin payment list: %w", err)
	}
	defer tx.Rollback(context.Background())
	var household any
	if filter.HouseholdID != "" {
		household = filter.HouseholdID
	}
	const where = ` WHERE ($1::text='' OR status=$1) AND ($2::uuid IS NULL OR household_id=$2)`
	page := domain.PaymentPage{Items: make([]domain.Payment, 0)}
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM payments"+where, string(filter.Status), household).Scan(&page.Total); err != nil {
		return domain.PaymentPage{}, fmt.Errorf("count payments: %w", err)
	}
	rows, err := tx.Query(ctx, "SELECT "+paymentColumns+" FROM payments"+where+" ORDER BY created_at DESC,id DESC LIMIT $3 OFFSET $4", string(filter.Status), household, filter.Pagination.Limit, (filter.Pagination.Page-1)*filter.Pagination.Limit)
	if err != nil {
		return domain.PaymentPage{}, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return domain.PaymentPage{}, fmt.Errorf("read payment: %w", err)
		}
		page.Items = append(page.Items, p)
	}
	if err := rows.Err(); err != nil {
		return domain.PaymentPage{}, fmt.Errorf("read payments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.PaymentPage{}, fmt.Errorf("commit payment list: %w", err)
	}
	return page, nil
}

func scanPayment(row rowScanner) (domain.Payment, error) {
	var p domain.Payment
	var amount string
	if err := row.Scan(&p.ID, &p.HouseholdID, &p.WasteID, &amount, &p.PaymentDate, &p.Status, &p.ProofFileURL, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return domain.Payment{}, err
	}
	money, err := domain.ParseMoney(amount)
	if err != nil {
		return domain.Payment{}, fmt.Errorf("decode payment amount: %w", err)
	}
	p.Amount = money
	p.CreatedAt = p.CreatedAt.UTC()
	p.UpdatedAt = p.UpdatedAt.UTC()
	if p.PaymentDate != nil {
		date := p.PaymentDate.UTC()
		p.PaymentDate = &date
	}
	return p, nil
}

type billingTransaction struct{ pickupTransaction }

func (r billingTransaction) PaymentForUpdate(ctx context.Context, wasteID string) (domain.Payment, error) {
	p, err := scanPayment(r.tx.QueryRow(ctx, "SELECT "+paymentColumns+" FROM payments WHERE waste_id=$1 FOR UPDATE", wasteID))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Payment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, fmt.Errorf("lock payment: %w", err)
	}
	return p, nil
}

func (r billingTransaction) InsertPayment(ctx context.Context, pickup domain.Pickup, amount domain.Money) (domain.Payment, error) {
	p, err := scanPayment(r.tx.QueryRow(ctx, `INSERT INTO payments(id,household_id,waste_id,amount) VALUES(gen_random_uuid(),$1,$2,$3::numeric) RETURNING `+paymentColumns, pickup.HouseholdID, pickup.ID, amount.String()))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "payments_waste_id_key" {
			return domain.Payment{}, domain.ErrPaymentExists
		}
		return domain.Payment{}, fmt.Errorf("create payment: %w", err)
	}
	return p, nil
}
