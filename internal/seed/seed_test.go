package seed

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/repository/postgres"

	"community-waste/internal/service"
	"community-waste/internal/storage"
	"community-waste/internal/testutil"
)

func TestSeedSnapshotRepeatAndPreserveChanges(t *testing.T) {
	pool := testutil.NewDatabase(t)
	root := t.TempDir()
	store := storage.NewLocalProof(root)
	sentinel, err := postgres.NewHousehold(pool).Create(t.Context(), "Unrelated", "Do not change")
	if err != nil {
		t.Fatal(err)
	}
	first, err := Run(t.Context(), pool, store)
	if err != nil || first.Households != 4 || first.Pickups != 16 || first.Payments != 6 {
		t.Fatalf("first: %+v %v", first, err)
	}
	report := service.NewReport(postgres.NewReport(pool))
	waste, err := report.WasteSummary(t.Context())
	if err != nil || len(waste) != 16 {
		t.Fatalf("waste: %+v %v", waste, err)
	}
	for _, row := range waste {
		if row.Count != 1 {
			t.Fatalf("combination: %+v", row)
		}
	}
	payments, err := report.PaymentSummary(t.Context())
	if err != nil || payments.TotalRevenue.String() != "100000.00" {
		t.Fatalf("payments: %+v %v", payments, err)
	}
	for i, amount := range []domain.Money{15000000, 10000000, 15000000} {
		if payments.Statuses[i].Count != 2 || payments.Statuses[i].Amount != amount {
			t.Fatalf("status: %+v", payments)
		}
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 2 {
		t.Fatalf("proofs: %v %v", files, err)
	}
	rows, err := pool.Query(t.Context(), "SELECT proof_file_url FROM payments WHERE status='paid'")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.Base(path)))
		if err != nil || len(data) < 5 || string(data[:5]) != "%PDF-" {
			t.Fatalf("proof: %s %v", path, err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	// Confirm an existing pending demo invoice, then preserve its new status/proof.
	var pending string
	if err := pool.QueryRow(t.Context(), "SELECT id::text FROM payments WHERE status='pending' ORDER BY id LIMIT 1").Scan(&pending); err != nil {
		t.Fatal(err)
	}
	proof, err := os.ReadFile("proof.pdf")
	if err != nil {
		t.Fatal(err)
	}
	paid, err := service.NewConfirmation(postgres.NewConfirmation(pool), store).Confirm(t.Context(), pending, proof)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(t.Context(), pool, store)
	if err != nil || second != (Result{}) {
		t.Fatalf("repeat: %+v %v", second, err)
	}
	var path, status string
	if err := pool.QueryRow(t.Context(), "SELECT status,proof_file_url FROM payments WHERE id=$1", pending).Scan(&status, &path); err != nil || status != "paid" || path != *paid.ProofFileURL {
		t.Fatalf("overwritten: %s %s %v", status, path, err)
	}
	files, err = os.ReadDir(root)
	if err != nil || len(files) != 3 {
		t.Fatalf("duplicate proofs: %v %v", files, err)
	}
	got, err := postgres.NewHousehold(pool).Get(t.Context(), sentinel.ID)
	if err != nil || got.OwnerName != sentinel.OwnerName || got.Address != sentinel.Address {
		t.Fatalf("sentinel: %+v %v", got, err)
	}
}

func TestSeedCollisionDoesNotModifyExistingData(t *testing.T) {
	pool := testutil.NewDatabase(t)
	root := t.TempDir()
	_, err := pool.Exec(t.Context(), "INSERT INTO households(id,owner_name,address) VALUES('10000000-0000-4000-8000-000000000102','Existing owner','Existing address')")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), pool, storage.NewLocalProof(root)); !errors.Is(err, ErrConflict) {
		t.Fatalf("collision: %v", err)
	}
	var households, pickups, payments int
	if err := pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM households),(SELECT count(*) FROM pickups),(SELECT count(*) FROM payments)").Scan(&households, &pickups, &payments); err != nil || households != 1 || pickups != 0 || payments != 0 {
		t.Fatalf("partial seed: %d/%d/%d %v", households, pickups, payments, err)
	}
}

func TestSeedRollbackCleansProofs(t *testing.T) {
	for _, failure := range []string{"storage", "insert", "commit"} {
		t.Run(failure, func(t *testing.T) {
			pool := testutil.NewDatabase(t)
			root := t.TempDir()
			storeRoot := root
			if failure == "storage" {
				storeRoot = filepath.Join(root, "file")
				if err := os.WriteFile(storeRoot, []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				timing := "BEFORE INSERT"
				trigger := "CREATE TRIGGER reject_seed " + timing + " ON payments"
				if failure == "commit" {
					trigger = "CREATE CONSTRAINT TRIGGER reject_seed AFTER INSERT ON payments DEFERRABLE INITIALLY DEFERRED"
				}
				_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_seed() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected seed failure'; END $$; `+trigger+` FOR EACH ROW EXECUTE FUNCTION reject_seed()`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Run(t.Context(), pool, storage.NewLocalProof(storeRoot)); err == nil {
				t.Fatal("expected failure")
			}
			var count int
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM households").Scan(&count); err != nil || count != 0 {
				t.Fatalf("partial seed: %d %v", count, err)
			}
			files, err := os.ReadDir(root)
			want := 0
			if failure == "storage" {
				want = 1
			}
			if err != nil || len(files) != want {
				t.Fatalf("orphan proofs: %v %v", files, err)
			}
		})
	}
}

func TestConcurrentSeedRuns(t *testing.T) {
	pool := testutil.NewDatabase(t)
	root := t.TempDir()
	store := storage.NewLocalProof(root)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey); err != nil {
		t.Fatal(err)
	}
	waiters := func(want int) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var count int
			err := pool.QueryRow(ctx, `WITH RECURSIVE waiters AS (
 SELECT pid FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))
 UNION SELECT a.pid FROM pg_stat_activity a JOIN waiters w ON w.pid=ANY(pg_blocking_pids(a.pid))
 ) SELECT count(*) FROM waiters`, blocker.Conn().PgConn().PID()).Scan(&count)
			if err != nil {
				t.Fatal(err)
			}
			if count == want {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("blocked seed runs=%d want=%d: %v", count, want, ctx.Err())
			case <-ticker.C:
			}
		}
	}
	results := make(chan error, 2)
	go func() { _, err := Run(ctx, pool, store); results <- err }()
	waiters(1)
	go func() { _, err := Run(ctx, pool, store); results <- err }()
	waiters(2)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM payments").Scan(&count); err != nil || count != 6 {
		t.Fatalf("count: %d %v", count, err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 2 {
		t.Fatalf("duplicate proofs: %v %v", files, err)
	}
}

func TestAlternateInvoiceReuseAndReservedIDCollision(t *testing.T) {
	pool := testutil.NewDatabase(t)
	root := t.TempDir()
	store := storage.NewLocalProof(root)
	if _, err := Run(t.Context(), pool, store); err != nil {
		t.Fatal(err)
	}
	reserved := payments()[0]
	var alternate string
	// Arrange an existing invoice under a non-seed identity, as an imported/API-created invoice could be.
	if err := pool.QueryRow(t.Context(), "UPDATE payments SET id=gen_random_uuid() WHERE id=$1 RETURNING id::text", reserved.id).Scan(&alternate); err != nil {
		t.Fatal(err)
	}
	if result, err := Run(t.Context(), pool, store); err != nil || result != (Result{}) {
		t.Fatalf("alternate reuse: %+v %v", result, err)
	}
	hh, err := postgres.NewHousehold(pool).Create(t.Context(), "Unrelated invoice owner", "Preserve address")
	if err != nil {
		t.Fatal(err)
	}
	p, err := service.NewPickup(postgres.NewPickup(pool)).Create(t.Context(), hh.ID, domain.WastePaper, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO payments(id,household_id,waste_id,amount) VALUES($1,$2,$3,50000)", reserved.id, hh.ID, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(t.Context(), pool, store); !errors.Is(err, ErrConflict) {
		t.Fatalf("reserved ID collision was ignored: %v", err)
	}
	var original, unrelated string
	if err := pool.QueryRow(t.Context(), "SELECT waste_id::text FROM payments WHERE id=$1", alternate).Scan(&original); err != nil || original != reserved.pickup {
		t.Fatalf("reused invoice changed: %s %v", original, err)
	}
	if err := pool.QueryRow(t.Context(), "SELECT waste_id::text FROM payments WHERE id=$1", reserved.id).Scan(&unrelated); err != nil || unrelated != p.ID {
		t.Fatalf("unrelated invoice changed: %s %v", unrelated, err)
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 2 {
		t.Fatalf("changed proofs: %v %v", files, err)
	}
}
