package seed

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"community-waste/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed proof.pdf
var proof []byte

const advisoryLockKey int64 = 0x435741535445

var ErrConflict = errors.New("reserved seed identity conflicts with existing data")

type ProofStore interface {
	Save(context.Context, []byte, string) (string, error)
	Delete(string) error
}

type Result struct{ Households, Pickups, Payments int64 }

func Run(ctx context.Context, pool *pgxpool.Pool, store ProofStore) (Result, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin seed: %w", err)
	}
	defer tx.Rollback(context.Background())
	var paths []string
	result, err := populate(ctx, tx, store, &paths)
	if err == nil {
		if commitErr := tx.Commit(ctx); commitErr != nil {
			var pgErr *pgconn.PgError
			if errors.Is(commitErr, pgx.ErrTxCommitRollback) || errors.As(commitErr, &pgErr) {
				err = fmt.Errorf("commit seed: %w", commitErr)
			} else {
				err = errors.Join(domain.ErrCommitUncertain, commitErr)
			}
		}
	}
	if err != nil {
		if errors.Is(err, domain.ErrCommitUncertain) {
			return Result{}, fmt.Errorf("%w; retained seed proofs: %v", err, paths)
		}
		for _, path := range paths {
			err = errors.Join(err, store.Delete(path))
		}
		return Result{}, err
	}
	return result, nil
}

func populate(ctx context.Context, tx pgx.Tx, store ProofStore, paths *[]string) (Result, error) {
	var result Result
	// Serialize repeat seed commands; API writers still use household-first locks.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey); err != nil {
		return Result{}, fmt.Errorf("lock seed: %w", err)
	}
	for _, row := range households() {
		tag, err := tx.Exec(ctx, `INSERT INTO households(id,owner_name,address,created_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(id) DO NOTHING`, row.id, row.name, row.address, createdAt)
		if err != nil {
			return Result{}, fmt.Errorf("seed household: %w", err)
		}
		var name, address string
		if err := tx.QueryRow(ctx, "SELECT owner_name,address FROM households WHERE id=$1 FOR UPDATE", row.id).Scan(&name, &address); err != nil {
			return Result{}, fmt.Errorf("lock seed household: %w", err)
		}
		if name != row.name || address != row.address {
			return Result{}, fmt.Errorf("%w: household %s", ErrConflict, row.id)
		}
		result.Households += tag.RowsAffected()
	}
	for _, row := range pickups() {
		updated := snapshotAt
		if row.status == domain.PickupPending {
			updated = createdAt
		}
		tag, err := tx.Exec(ctx, `INSERT INTO pickups(id,household_id,type,status,pickup_date,safety_check,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(id) DO NOTHING`, row.id, row.household, row.kind, row.status, row.date, row.safety, createdAt, updated)
		if err != nil {
			return Result{}, fmt.Errorf("seed pickup: %w", err)
		}
		var household string
		var kind domain.WasteType
		if err := tx.QueryRow(ctx, "SELECT household_id::text,type FROM pickups WHERE id=$1 FOR UPDATE", row.id).Scan(&household, &kind); err != nil {
			return Result{}, fmt.Errorf("lock seed pickup: %w", err)
		}
		if household != row.household || kind != row.kind {
			return Result{}, fmt.Errorf("%w: pickup %s", ErrConflict, row.id)
		}
		result.Pickups += tag.RowsAffected()
	}
	for _, row := range payments() {
		// Identity is immutable through the API. Do not lock an unrelated owner's row.
		var reservedHousehold, reservedPickup, reservedAmount string
		identityErr := tx.QueryRow(ctx, "SELECT household_id::text,waste_id::text,amount::text FROM payments WHERE id=$1", row.id).Scan(&reservedHousehold, &reservedPickup, &reservedAmount)
		if identityErr != nil && !errors.Is(identityErr, pgx.ErrNoRows) {
			return Result{}, fmt.Errorf("check reserved seed payment: %w", identityErr)
		}
		if identityErr == nil && (reservedHousehold != row.household || reservedPickup != row.pickup || reservedAmount != row.amount.String()) {
			return Result{}, fmt.Errorf("%w: payment %s", ErrConflict, row.id)
		}
		var household, amount string
		err := tx.QueryRow(ctx, "SELECT household_id::text,amount::text FROM payments WHERE waste_id=$1 FOR UPDATE", row.pickup).Scan(&household, &amount)
		if err == nil {
			if household != row.household || amount != row.amount.String() {
				return Result{}, fmt.Errorf("%w: payment for pickup %s", ErrConflict, row.pickup)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return Result{}, fmt.Errorf("check seed payment: %w", err)
		}
		var path *string
		var date *time.Time
		updated := snapshotAt
		created := snapshotAt
		if row.status == domain.PaymentPaid {
			saved, err := store.Save(ctx, proof, ".pdf")
			if err != nil {
				return Result{}, fmt.Errorf("save seed proof: %w", err)
			}
			*paths = append(*paths, saved)
			path = &saved
			paidAt := snapshotAt.Add(time.Hour)
			date = &paidAt
			updated = paidAt
		}
		if row.status == domain.PaymentFailed {
			created = createdAt
		}
		_, err = tx.Exec(ctx, `INSERT INTO payments(id,household_id,waste_id,amount,status,payment_date,proof_file_url,created_at,updated_at) VALUES($1,$2,$3,$4::numeric,$5,$6,$7,$8,$9)`, row.id, row.household, row.pickup, row.amount.String(), row.status, date, path, created, updated)
		if err != nil {
			return Result{}, fmt.Errorf("seed payment: %w", err)
		}
		result.Payments++
	}
	return result, nil
}
