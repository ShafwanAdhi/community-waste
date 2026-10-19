package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/service"
	"community-waste/internal/storage"
	"github.com/jackc/pgx/v5/pgxpool"
)

var confirmationPDF = []byte("%PDF-1.4\nproof\n%%EOF\n")

func confirmationFixture(t *testing.T) (*pgxpool.Pool, *service.Pickup, *service.Confirmation, domain.Payment, string) {
	t.Helper()
	pool, pickups, hh := pickupFixture(t)
	p := createPickup(t, pickups, hh, domain.WastePaper, nil)
	invoice, err := service.NewBilling(NewBilling(pool)).Create(t.Context(), hh, p.ID, "50000")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return pool, pickups, service.NewConfirmation(NewConfirmation(pool), storage.NewLocalProof(root)), invoice, root
}

func TestConfirmationRepositoryAndStorage(t *testing.T) {
	pool, pickups, confirmation, invoice, root := confirmationFixture(t)
	paid, err := confirmation.Confirm(t.Context(), invoice.ID, confirmationPDF)
	if err != nil || paid.Status != domain.PaymentPaid || paid.PaymentDate == nil || paid.ProofFileURL == nil {
		t.Fatalf("paid: %+v %v", paid, err)
	}
	file := filepath.Join(root, filepath.Base(*paid.ProofFileURL))
	data, err := os.ReadFile(file)
	if err != nil || string(data) != string(confirmationPDF) {
		t.Fatalf("file: %s %v", data, err)
	}
	if _, err := confirmation.Confirm(t.Context(), invoice.ID, confirmationPDF); !errors.Is(err, domain.ErrPaymentNotPending) {
		t.Fatalf("repeat: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("overwritten proof: %v %v", entries, err)
	}
	if _, err := pickups.Cancel(t.Context(), invoice.WasteID); !errors.Is(err, domain.ErrPaymentAlreadyPaid) {
		t.Fatalf("paid cancel: %v", err)
	}
	createPickup(t, pickups, invoice.HouseholdID, domain.WasteOrganic, nil)
	var path string
	var status string
	if err := pool.QueryRow(t.Context(), "SELECT status,proof_file_url FROM payments WHERE id=$1", invoice.ID).Scan(&status, &path); err != nil || status != "paid" || path != *paid.ProofFileURL {
		t.Fatalf("database: %s %s %v", status, path, err)
	}
}

func TestConfirmationDatabaseFailureCleansFile(t *testing.T) {
	pool, _, confirmation, invoice, root := confirmationFixture(t)
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_confirmation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected confirmation failure'; END $$;
 CREATE TRIGGER reject_confirmation BEFORE UPDATE ON payments FOR EACH ROW EXECUTE FUNCTION reject_confirmation()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := confirmation.Confirm(t.Context(), invoice.ID, confirmationPDF); err == nil {
		t.Fatal("expected DB failure")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("orphan files: %v %v", entries, err)
	}
	var status string
	var date *time.Time
	var path *string
	err = pool.QueryRow(t.Context(), "SELECT status,payment_date,proof_file_url FROM payments WHERE id=$1", invoice.ID).Scan(&status, &date, &path)
	if err != nil || status != "pending" || date != nil || path != nil {
		t.Fatalf("partial DB update: %s %v %v %v", status, date, path, err)
	}
}

func TestConfirmationStorageFailurePreservesPending(t *testing.T) {
	pool, _, _, invoice, root := confirmationFixture(t)
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	confirmation := service.NewConfirmation(NewConfirmation(pool), storage.NewLocalProof(blocked))
	if _, err := confirmation.Confirm(t.Context(), invoice.ID, confirmationPDF); err == nil {
		t.Fatal("expected storage failure")
	}
	var status string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM payments WHERE id=$1", invoice.ID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("status %s %v", status, err)
	}
}

func TestConfirmationConcurrentMutations(t *testing.T) {
	for _, tc := range []struct {
		first, second string
		want          error
		files         int
	}{{"confirm", "confirm", domain.ErrPaymentNotPending, 1}, {"confirm", "cancel", domain.ErrPaymentAlreadyPaid, 1}, {"cancel", "confirm", domain.ErrPaymentNotPending, 0}, {"confirm", "pickup", nil, 1}, {"pickup", "confirm", domain.ErrPendingPayment, 1}} {
		name := tc.first + "/" + tc.second
		t.Run(name, func(t *testing.T) {
			pool, pickups, confirmation, invoice, root := confirmationFixture(t)
			action := func(name string) func(context.Context) error {
				return func(ctx context.Context) error {
					switch name {
					case "confirm":
						_, err := confirmation.Confirm(ctx, invoice.ID, confirmationPDF)
						return err
					case "cancel":
						_, err := pickups.Cancel(ctx, invoice.WasteID)
						return err
					case "pickup":
						_, err := pickups.Create(ctx, invoice.HouseholdID, domain.WasteOrganic, nil)
						return err
					default:
						panic("test action")
					}
				}
			}
			first, second := queuedPickupChanges(t, pool, invoice.HouseholdID, action(tc.first), action(tc.second))
			if tc.first == "pickup" {
				if !errors.Is(first, tc.want) || second != nil {
					t.Fatalf("first %v second %v", first, second)
				}
			} else if first != nil || !errors.Is(second, tc.want) {
				t.Fatalf("first %v second %v want %v", first, second, tc.want)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != tc.files {
				t.Fatalf("files: %v %v", entries, err)
			}
		})
	}
}

func TestPaymentDateFilters(t *testing.T) {
	pool, pickups, hh := pickupFixture(t)
	billing := service.NewBilling(NewBilling(pool))
	var invoices []domain.Payment
	// Create pickups before issuing any pending invoices.
	var requests []domain.Pickup
	for range 4 {
		requests = append(requests, createPickup(t, pickups, hh, domain.WastePaper, nil))
	}
	for _, p := range requests {
		invoice, err := billing.Create(t.Context(), hh, p.ID, "50000")
		if err != nil {
			t.Fatal(err)
		}
		invoices = append(invoices, invoice)
	}
	from := time.Date(2026, 10, 18, 8, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	to := from.Add(time.Hour)
	fromAfter := from.Add(time.Nanosecond)
	toAfter := to.Add(time.Nanosecond)
	narrowEnd := from.Add(2 * time.Nanosecond)
	for i, date := range []time.Time{from.Add(-time.Microsecond), from, to} {
		if _, err := pool.Exec(t.Context(), "UPDATE payments SET status='paid',payment_date=$2 WHERE id=$1", invoices[i].ID, date); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		from, to *time.Time
		status   domain.PaymentStatus
		total    int64
	}{{&fromAfter, &narrowEnd, "", 0}, {&fromAfter, &to, "", 0}, {&from, &toAfter, "", 2}, {&fromAfter, nil, "", 1}, {&from, &to, "", 1}, {&from, nil, "", 2}, {nil, &to, "", 2}, {nil, nil, "", 4}, {&from, &to, domain.PaymentPending, 0}, {&from, &to, domain.PaymentPaid, 1}} {
		page, err := billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, DateFrom: tc.from, DateTo: tc.to, HouseholdID: hh, Status: tc.status})
		if err != nil || page.Total != tc.total || int64(len(page.Items)) != tc.total {
			t.Fatalf("range %+v: %+v %v", tc, page, err)
		}
	}
}

func TestConfirmationCommitRejectionCleansFile(t *testing.T) {
	pool, _, confirmation, invoice, root := confirmationFixture(t)
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_confirmation_commit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected deferred rejection'; END $$;
 CREATE CONSTRAINT TRIGGER reject_confirmation_commit AFTER UPDATE ON payments DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_confirmation_commit()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := confirmation.Confirm(t.Context(), invoice.ID, confirmationPDF); err == nil || errors.Is(err, domain.ErrCommitUncertain) {
		t.Fatalf("commit rejection should be definite: %v", err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatalf("commit rejection left files: %v %v", files, err)
	}
	var status string
	var proof *string
	if err := pool.QueryRow(t.Context(), "SELECT status,proof_file_url FROM payments WHERE id=$1", invoice.ID).Scan(&status, &proof); err != nil || status != "pending" || proof != nil {
		t.Fatalf("partial commit: %s %v %v", status, proof, err)
	}
}
