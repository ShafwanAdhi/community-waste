package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/service"
	"community-waste/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

func pickupFixture(t *testing.T) (*pgxpool.Pool, *service.Pickup, string) {
	t.Helper()
	pool := testutil.NewDatabase(t)
	household, err := NewHousehold(pool).Create(t.Context(), "Pickup test", "Jl. Merdeka")
	if err != nil {
		t.Fatal(err)
	}
	return pool, service.NewPickup(NewPickup(pool)), household.ID
}

func createPickup(t *testing.T, svc *service.Pickup, household string, kind domain.WasteType, safety *bool) domain.Pickup {
	t.Helper()
	p, err := svc.Create(t.Context(), household, kind, safety)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func insertPickupPayment(t *testing.T, pool *pgxpool.Pool, p domain.Pickup, status string) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `INSERT INTO payments(id,household_id,waste_id,amount,status) VALUES(gen_random_uuid(),$1,$2,50000,$3)`, p.HouseholdID, p.ID, status)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPickupRepositoryLifecycle(t *testing.T) {
	pool, svc, hh := pickupFixture(t)
	safety := false
	p := createPickup(t, svc, hh, domain.WasteElectronic, &safety)
	if p.Status != domain.PickupPending || p.SafetyCheck == nil || *p.SafetyCheck || p.PickupDate != nil || p.CreatedAt.IsZero() {
		t.Fatalf("created: %+v", p)
	}
	date := time.Now().Add(time.Hour).Truncate(time.Microsecond).In(time.FixedZone("WIB", 7*3600))
	if _, err := svc.Schedule(t.Context(), p.ID, date, nil); !errors.Is(err, domain.ErrSafetyCheckRequired) {
		t.Fatalf("unsafe schedule: %v", err)
	}
	safety = true
	scheduled, err := svc.Schedule(t.Context(), p.ID, date, &safety)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled.Status != domain.PickupScheduled || !scheduled.PickupDate.Equal(date) || scheduled.PickupDate.Location() != time.UTC || !*scheduled.SafetyCheck {
		t.Fatalf("scheduled: %+v", scheduled)
	}
	if _, err := svc.Schedule(t.Context(), p.ID, date, nil); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("reschedule: %v", err)
	}
	insertPickupPayment(t, pool, p, "pending")
	if _, err := svc.Create(t.Context(), hh, domain.WastePaper, nil); !errors.Is(err, domain.ErrPendingPayment) {
		t.Fatalf("pending payment: %v", err)
	}
	canceled, err := svc.Cancel(t.Context(), p.ID)
	if err != nil || canceled.Status != domain.PickupCanceled {
		t.Fatalf("cancel: %+v %v", canceled, err)
	}
	var status string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM payments WHERE waste_id=$1", p.ID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("invoice: %s %v", status, err)
	}
	if _, err := svc.Cancel(t.Context(), p.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("recancel: %v", err)
	}
	createPickup(t, svc, hh, domain.WastePaper, nil)
	if err := NewHousehold(pool).Delete(t.Context(), hh); !errors.Is(err, domain.ErrHouseholdInUse) {
		t.Fatalf("delete history: %v", err)
	}
}

func TestPickupRepositoryMissingAndPaid(t *testing.T) {
	pool, svc, hh := pickupFixture(t)
	const absent = "00000000-0000-4000-8000-000000000099"
	if _, err := svc.Create(t.Context(), absent, domain.WasteOrganic, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing household: %v", err)
	}
	if _, err := svc.Cancel(t.Context(), absent); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing pickup: %v", err)
	}
	p := createPickup(t, svc, hh, domain.WasteOrganic, nil)
	insertPickupPayment(t, pool, p, "paid")
	if _, err := svc.Cancel(t.Context(), p.ID); !errors.Is(err, domain.ErrPaymentAlreadyPaid) {
		t.Fatalf("paid cancellation: %v", err)
	}
	var status string
	if err := pool.QueryRow(t.Context(), "SELECT status FROM pickups WHERE id=$1", p.ID).Scan(&status); err != nil || status != "pending" {
		t.Fatalf("paid pickup changed: %s %v", status, err)
	}
}

func TestPickupRepositoryList(t *testing.T) {
	pool, svc, hh := pickupFixture(t)
	other, err := NewHousehold(pool).Create(t.Context(), "Other", "Address")
	if err != nil {
		t.Fatal(err)
	}
	a := createPickup(t, svc, hh, domain.WasteOrganic, nil)
	b := createPickup(t, svc, hh, domain.WastePlastic, nil)
	createPickup(t, svc, other.ID, domain.WastePaper, nil)
	if _, err := svc.Cancel(t.Context(), b.ID); err != nil {
		t.Fatal(err)
	}
	// Equal creation timestamps exercise the UUID tie breaker.
	if _, err := pool.Exec(t.Context(), "UPDATE pickups SET created_at='2026-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	page, err := svc.List(t.Context(), domain.PickupFilter{Pagination: domain.Pagination{Page: 1, Limit: 1}, HouseholdID: hh})
	if err != nil {
		t.Fatal(err)
	}
	want := a.ID
	if b.ID > want {
		want = b.ID
	}
	if page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != want {
		t.Fatalf("tie order: %+v", page)
	}
	for _, tc := range []struct {
		status             domain.PickupStatus
		household          string
		page, total, count int64
	}{
		{domain.PickupPending, hh, 1, 1, 1}, {domain.PickupCanceled, hh, 1, 1, 1}, {domain.PickupCompleted, "", 1, 0, 0}, {"", hh, 10, 2, 0}, {"", "", 1, 3, 3},
	} {
		got, err := svc.List(t.Context(), domain.PickupFilter{Pagination: domain.Pagination{Page: tc.page, Limit: 20}, Status: tc.status, HouseholdID: tc.household})
		if err != nil || got.Total != tc.total || got.Items == nil || int64(len(got.Items)) != tc.count {
			t.Fatalf("filter %+v: %+v %v", tc, got, err)
		}
	}
}

func TestPickupCancelRollsBackInvoice(t *testing.T) {
	pool, svc, hh := pickupFixture(t)
	p := createPickup(t, svc, hh, domain.WasteOrganic, nil)
	insertPickupPayment(t, pool, p, "pending")
	_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_pickup_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected write failure'; END $$;
 CREATE TRIGGER reject_pickup_update BEFORE UPDATE ON pickups FOR EACH ROW EXECUTE FUNCTION reject_pickup_update()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(t.Context(), p.ID); err == nil {
		t.Fatal("expected injected write failure")
	}
	var pickupStatus, paymentStatus string
	err = pool.QueryRow(t.Context(), `SELECT w.status,p.status FROM pickups w JOIN payments p ON p.waste_id=w.id WHERE w.id=$1`, p.ID).Scan(&pickupStatus, &paymentStatus)
	if err != nil || pickupStatus != "pending" || paymentStatus != "pending" {
		t.Fatalf("rollback: %s/%s %v", pickupStatus, paymentStatus, err)
	}
}

// queuedPickupChanges proves both requests reach PostgreSQL lock waits before
// releasing the household. The first request is queued before the second starts.
func queuedPickupChanges(t *testing.T, pool *pgxpool.Pool, householdID string, first, second func(context.Context) error) (error, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(ctx, "SELECT id FROM households WHERE id=$1 FOR UPDATE", householdID); err != nil {
		t.Fatal(err)
	}
	blockerPID := blocker.Conn().PgConn().PID()
	waitForBlocked := func(want int) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var count int
			err := pool.QueryRow(ctx, `WITH RECURSIVE waiters AS (
    SELECT pid FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))
    UNION
    SELECT a.pid FROM pg_stat_activity a JOIN waiters w ON w.pid=ANY(pg_blocking_pids(a.pid))
   ) SELECT count(*) FROM waiters`, blockerPID).Scan(&count)
			if err != nil {
				t.Fatal(err)
			}
			if count == want {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("wanted %d blocked transactions, observed %d: %v", want, count, ctx.Err())
			case <-ticker.C:
			}
		}
	}
	firstResult, secondResult := make(chan error, 1), make(chan error, 1)
	go func() { firstResult <- first(ctx) }()
	waitForBlocked(1)
	go func() { secondResult <- second(ctx) }()
	waitForBlocked(2)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return <-firstResult, <-secondResult
}

func TestPickupConcurrentScheduling(t *testing.T) {
	pool, svc, hh := pickupFixture(t)
	p := createPickup(t, svc, hh, domain.WasteOrganic, nil)
	schedule := func(ctx context.Context) error {
		_, err := svc.Schedule(ctx, p.ID, time.Now().Add(time.Hour), nil)
		return err
	}
	first, second := queuedPickupChanges(t, pool, hh, schedule, schedule)
	if first != nil || !errors.Is(second, domain.ErrInvalidTransition) {
		t.Fatalf("first=%v second=%v", first, second)
	}
}

func TestPickupConcurrentScheduleAndCancel(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		name := "schedule first"
		if cancelFirst {
			name = "cancel first"
		}
		t.Run(name, func(t *testing.T) {
			pool, svc, hh := pickupFixture(t)
			p := createPickup(t, svc, hh, domain.WasteOrganic, nil)
			schedule := func(ctx context.Context) error {
				_, err := svc.Schedule(ctx, p.ID, time.Now().Add(time.Hour), nil)
				return err
			}
			cancel := func(ctx context.Context) error { _, err := svc.Cancel(ctx, p.ID); return err }
			firstChange, secondChange := schedule, cancel
			if cancelFirst {
				firstChange, secondChange = cancel, schedule
			}
			first, second := queuedPickupChanges(t, pool, hh, firstChange, secondChange)
			if first != nil {
				t.Fatal(first)
			}
			if cancelFirst && !errors.Is(second, domain.ErrInvalidTransition) {
				t.Fatalf("schedule after cancellation: %v", second)
			}
			if !cancelFirst && second != nil {
				t.Fatalf("cancel after scheduling: %v", second)
			}
			var status string
			if err := pool.QueryRow(t.Context(), "SELECT status FROM pickups WHERE id=$1", p.ID).Scan(&status); err != nil || status != "canceled" {
				t.Fatalf("final state: %s %v", status, err)
			}
		})
	}
}
