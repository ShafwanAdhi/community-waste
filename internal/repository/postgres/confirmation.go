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

type Confirmation struct{ pool *pgxpool.Pool }

func NewConfirmation(pool *pgxpool.Pool) *Confirmation { return &Confirmation{pool: pool} }

func (r *Confirmation) Reference(ctx context.Context, id string) (domain.PaymentReference, error) {
	var reference domain.PaymentReference
	err := r.pool.QueryRow(ctx, "SELECT household_id::text,waste_id::text FROM payments WHERE id=$1", id).Scan(&reference.HouseholdID, &reference.WasteID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.PaymentReference{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.PaymentReference{}, fmt.Errorf("get payment reference: %w", err)
	}
	return reference, nil
}

func (r *Confirmation) WithinTransaction(ctx context.Context, fn func(service.ConfirmationTx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin confirmation: %w", err)
	}
	defer tx.Rollback(context.Background())
	if err := fn(confirmationTransaction{pickupTransaction: pickupTransaction{tx: tx}}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		// A server rejection proves rollback. A transport failure may follow a commit.
		var pgErr *pgconn.PgError
		if errors.Is(err, pgx.ErrTxCommitRollback) || errors.As(err, &pgErr) {
			return fmt.Errorf("commit confirmation: %w", err)
		}
		return errors.Join(domain.ErrCommitUncertain, err)
	}
	return nil
}

type confirmationTransaction struct{ pickupTransaction }

func (r confirmationTransaction) PaymentByIDForUpdate(ctx context.Context, id string) (domain.Payment, error) {
	p, err := scanPayment(r.tx.QueryRow(ctx, "SELECT "+paymentColumns+" FROM payments WHERE id=$1 FOR UPDATE", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Payment{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Payment{}, fmt.Errorf("lock payment by id: %w", err)
	}
	return p, nil
}

func (r confirmationTransaction) MarkPaid(ctx context.Context, id, path string) (domain.Payment, error) {
	p, err := scanPayment(r.tx.QueryRow(ctx, `UPDATE payments SET status='paid',payment_date=clock_timestamp(),proof_file_url=$2,updated_at=clock_timestamp() WHERE id=$1 RETURNING `+paymentColumns, id, path))
	if err != nil {
		return domain.Payment{}, fmt.Errorf("confirm payment: %w", err)
	}
	return p, nil
}
