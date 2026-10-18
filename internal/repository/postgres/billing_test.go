package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/service"
)

func TestBillingRepositoryCreateListAndReuse(t *testing.T) {
	pool, pickups, hh := pickupFixture(t)
	billing := service.NewBilling(NewBilling(pool))
	p := createPickup(t, pickups, hh, domain.WasteOrganic, nil)
	invoice, err := billing.Create(t.Context(), hh, p.ID, "50000")
	if err != nil {
		t.Fatal(err)
	}
	if invoice.Amount != 5000000 || invoice.Status != domain.PaymentPending || invoice.PaymentDate != nil || invoice.ProofFileURL != nil || invoice.CreatedAt.IsZero() {
		t.Fatalf("invoice: %+v", invoice)
	}
	if _, err := billing.Create(t.Context(), hh, p.ID, "50000"); !errors.Is(err, domain.ErrPaymentExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := pickups.Schedule(t.Context(), p.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := billing.Complete(t.Context(), p.ID); err != nil {
		t.Fatal(err)
	}
	page, err := billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, HouseholdID: hh, Status: domain.PaymentPending})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != invoice.ID || page.Items[0].Amount != invoice.Amount {
		t.Fatalf("reused: %+v %v", page, err)
	}
	if _, err := billing.Complete(t.Context(), p.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("repeat: %v", err)
	}
}

func TestBillingRepositoryAutomaticTariffs(t *testing.T) {
	for _, tc := range []struct {
		kind   domain.WasteType
		amount domain.Money
	}{{domain.WasteOrganic, 5000000}, {domain.WastePlastic, 5000000}, {domain.WastePaper, 5000000}, {domain.WasteElectronic, 10000000}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			pool, pickups, hh := pickupFixture(t)
			billing := service.NewBilling(NewBilling(pool))
			safe := true
			p := createPickup(t, pickups, hh, tc.kind, &safe)
			if _, err := pickups.Schedule(t.Context(), p.ID, time.Now().Add(time.Hour), nil); err != nil {
				t.Fatal(err)
			}
			got, err := billing.Complete(t.Context(), p.ID)
			if err != nil || got.Status != domain.PickupCompleted {
				t.Fatalf("complete: %+v %v", got, err)
			}
			page, err := billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}})
			if err != nil || len(page.Items) != 1 || page.Items[0].Amount != tc.amount || page.Items[0].WasteID != p.ID {
				t.Fatalf("tariff: %+v %v", page, err)
			}
			if _, err := pickups.Create(t.Context(), hh, domain.WastePaper, nil); !errors.Is(err, domain.ErrPendingPayment) {
				t.Fatalf("new pickup: %v", err)
			}
		})
	}
}

func TestBillingRepositoryRollback(t *testing.T) {
	for _, target := range []string{"payments", "pickups"} {
		t.Run(target, func(t *testing.T) {
			pool, pickups, hh := pickupFixture(t)
			billing := service.NewBilling(NewBilling(pool))
			p := createPickup(t, pickups, hh, domain.WastePaper, nil)
			if _, err := pickups.Schedule(t.Context(), p.ID, time.Now().Add(time.Hour), nil); err != nil {
				t.Fatal(err)
			}
			event := "INSERT"
			if target == "pickups" {
				event = "UPDATE"
			}
			_, err := pool.Exec(t.Context(), `CREATE FUNCTION reject_billing_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected billing failure'; END $$;
    CREATE TRIGGER reject_billing_write BEFORE `+event+` ON `+target+` FOR EACH ROW EXECUTE FUNCTION reject_billing_write()`)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := billing.Complete(t.Context(), p.ID); err == nil {
				t.Fatal("expected injected failure")
			}
			var status string
			var count int
			if err := pool.QueryRow(t.Context(), "SELECT status,(SELECT count(*) FROM payments) FROM pickups WHERE id=$1", p.ID).Scan(&status, &count); err != nil || status != "scheduled" || count != 0 {
				t.Fatalf("partial commit: %s %d %v", status, count, err)
			}
		})
	}
}

func TestBillingRepositoryPoliciesAndFilters(t *testing.T) {
	pool, pickups, hh := pickupFixture(t)
	billing := service.NewBilling(NewBilling(pool))
	const absent = "00000000-0000-4000-8000-000000000099"
	if _, err := billing.Complete(t.Context(), absent); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	p := createPickup(t, pickups, hh, domain.WastePaper, nil)
	if _, err := billing.Create(t.Context(), absent, p.ID, "50000"); !errors.Is(err, domain.ErrHouseholdMismatch) {
		t.Fatalf("owner: %v", err)
	}
	if _, err := billing.Create(t.Context(), hh, p.ID, "1"); err == nil {
		t.Fatal("wrong tariff accepted")
	}
	q := createPickup(t, pickups, hh, domain.WasteOrganic, nil)
	r := createPickup(t, pickups, hh, domain.WastePlastic, nil)
	for _, p := range []domain.Pickup{p, q, r} {
		if _, err := billing.Create(t.Context(), hh, p.ID, "50000"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(t.Context(), "UPDATE payments SET created_at='2026-01-01T00:00:00Z'"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE payments SET status='paid' WHERE waste_id=$1", q.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "UPDATE payments SET status='failed' WHERE waste_id=$1", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pickups.Schedule(t.Context(), q.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := billing.Complete(t.Context(), q.ID); err != nil {
		t.Fatalf("paid reuse: %v", err)
	}
	if _, err := pickups.Schedule(t.Context(), r.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := billing.Complete(t.Context(), r.ID); !errors.Is(err, domain.ErrPaymentConflict) {
		t.Fatalf("failed reuse: %v", err)
	}
	for _, status := range []domain.PaymentStatus{domain.PaymentPending, domain.PaymentPaid, domain.PaymentFailed} {
		page, err := billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, Status: status, HouseholdID: hh})
		if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Status != status {
			t.Fatalf("filter %s: %+v %v", status, page, err)
		}
	}
	page, err := billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 2}})
	if err != nil || page.Total != 3 || len(page.Items) != 2 || page.Items[0].ID < page.Items[1].ID {
		t.Fatalf("order: %+v %v", page, err)
	}
	page, err = billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 99, Limit: 20}})
	if err != nil || page.Total != 3 || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("empty page: %+v %v", page, err)
	}
	page, err = billing.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, HouseholdID: absent})
	if err != nil || page.Total != 0 || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("empty filter: %+v %v", page, err)
	}
}

func TestBillingConcurrentWriters(t *testing.T) {
	for _, tc := range []struct {
		first, second string
		wantSecond    error
		pickupCount   int
	}{
		{"manual", "manual", domain.ErrPaymentExists, 1},
		{"complete", "complete", domain.ErrInvalidTransition, 1},
		{"manual", "complete", nil, 1},
		{"complete", "manual", domain.ErrInvalidTransition, 1},
		{"manual", "pickup", domain.ErrPendingPayment, 1},
		{"pickup", "manual", nil, 2},
		{"complete", "pickup", domain.ErrPendingPayment, 1},
		{"cancel", "manual", domain.ErrInvalidTransition, 1},
		{"manual", "cancel", nil, 1},
	} {
		t.Run(tc.first+"/"+tc.second, func(t *testing.T) {
			pool, pickups, hh := pickupFixture(t)
			billing := service.NewBilling(NewBilling(pool))
			p := createPickup(t, pickups, hh, domain.WasteOrganic, nil)
			if _, err := pickups.Schedule(t.Context(), p.ID, time.Now().Add(time.Hour), nil); err != nil {
				t.Fatal(err)
			}
			action := func(name string) func(context.Context) error {
				return func(ctx context.Context) error {
					switch name {
					case "manual":
						_, err := billing.Create(ctx, hh, p.ID, "50000")
						return err
					case "complete":
						_, err := billing.Complete(ctx, p.ID)
						return err
					case "pickup":
						_, err := pickups.Create(ctx, hh, domain.WastePaper, nil)
						return err
					case "cancel":
						_, err := pickups.Cancel(ctx, p.ID)
						return err
					default:
						panic("invalid test action")
					}
				}
			}
			first, second := queuedPickupChanges(t, pool, hh, action(tc.first), action(tc.second))
			if first != nil || !errors.Is(second, tc.wantSecond) {
				t.Fatalf("first=%v second=%v want=%v", first, second, tc.wantSecond)
			}
			var count int
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM payments WHERE waste_id=$1", p.ID).Scan(&count); err != nil || count > 1 {
				t.Fatalf("invoice count %d %v", count, err)
			}
			wantCount := 1
			if tc.first == "cancel" {
				wantCount = 0
			}
			if count != wantCount {
				t.Fatalf("invoices=%d want=%d", count, wantCount)
			}
			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM pickups WHERE household_id=$1", hh).Scan(&count); err != nil || count != tc.pickupCount {
				t.Fatalf("pickup count %d %v", count, err)
			}
			if tc.second == "cancel" {
				var status string
				if err := pool.QueryRow(t.Context(), "SELECT status FROM payments WHERE waste_id=$1", p.ID).Scan(&status); err != nil || status != "failed" {
					t.Fatalf("cancel invoice %s %v", status, err)
				}
			}
		})
	}
}
