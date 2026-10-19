package postgres

import (
	"testing"

	"community-waste/internal/domain"
	"community-waste/internal/service"
	"community-waste/internal/testutil"
)

func TestReportRepositoryEmpty(t *testing.T) {
	report := service.NewReport(NewReport(testutil.NewDatabase(t)))
	waste, err := report.WasteSummary(t.Context())
	if err != nil || waste == nil || len(waste) != 0 {
		t.Fatalf("empty waste: %+v %v", waste, err)
	}
	payment, err := report.PaymentSummary(t.Context())
	if err != nil || len(payment.Statuses) != 3 || payment.TotalRevenue != 0 {
		t.Fatalf("empty payment: %+v %v", payment, err)
	}
	for _, row := range payment.Statuses {
		if row.Count != 0 || row.Amount != 0 {
			t.Fatalf("nonzero: %+v", payment)
		}
	}
}

func TestReportRepositoryGroupsAndExactTotals(t *testing.T) {
	pool, _, hh := pickupFixture(t)
	_, err := pool.Exec(t.Context(), `INSERT INTO pickups(id,household_id,type,status,safety_check)
 SELECT gen_random_uuid(),$1,type,status,safety FROM (VALUES
 ('organic','pending',NULL::boolean),('organic','pending',NULL::boolean),
 ('plastic','completed',NULL::boolean),('paper','canceled',NULL::boolean),('electronic','scheduled',true)) AS v(type,status,safety)`, hh)
	if err != nil {
		t.Fatal(err)
	}
	report := service.NewReport(NewReport(pool))
	rows, err := report.WasteSummary(t.Context())
	if err != nil || len(rows) != 4 {
		t.Fatalf("groups: %+v %v", rows, err)
	}
	for i, want := range []domain.WasteSummary{{Type: domain.WasteElectronic, Status: domain.PickupScheduled, Count: 1}, {Type: domain.WasteOrganic, Status: domain.PickupPending, Count: 2}, {Type: domain.WastePaper, Status: domain.PickupCanceled, Count: 1}, {Type: domain.WastePlastic, Status: domain.PickupCompleted, Count: 1}} {
		if rows[i] != want {
			t.Fatalf("position%d: %+v want%+v", i, rows[i], want)
		}
	}
	_, err = pool.Exec(t.Context(), `INSERT INTO payments(id,household_id,waste_id,amount,status)
 SELECT gen_random_uuid(),household_id,id,CASE WHEN type='electronic' THEN 100000 ELSE 50000 END,
 CASE WHEN type='plastic' THEN 'paid' WHEN type='paper' THEN 'failed' ELSE 'pending' END
 FROM pickups WHERE type<>'organic'`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := report.PaymentSummary(t.Context())
	if err != nil || got.TotalRevenue != 5000000 || got.Statuses[0].Amount != 10000000 || got.Statuses[1].Amount != 5000000 || got.Statuses[2].Amount != 5000000 {
		t.Fatalf("totals: %+v %v", got, err)
	}
	for _, row := range got.Statuses {
		if row.Count != 1 {
			t.Fatalf("count: %+v", got)
		}
	}
}

func TestReportAggregateExceedsSinglePaymentRange(t *testing.T) {
	pool, _, hh := pickupFixture(t)
	// Exercise the database's decimal range, independently of API tariff validation.
	_, err := pool.Exec(t.Context(), `WITH inserted AS (
 INSERT INTO pickups(id,household_id,type,status)
 SELECT gen_random_uuid(),$1,'paper','completed' FROM generate_series(1,2) RETURNING id,household_id)
 INSERT INTO payments(id,household_id,waste_id,amount,status)
 SELECT gen_random_uuid(),household_id,id,9999999999.99,'paid' FROM inserted`, hh)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.NewReport(NewReport(pool)).PaymentSummary(t.Context())
	if err != nil || got.TotalRevenue.String() != "19999999999.98" || got.Statuses[1].Count != 2 {
		t.Fatalf("large exact total: %+v %v", got, err)
	}
}
