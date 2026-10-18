package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"community-waste/internal/domain"
)

const pickupHouseholdID = "10000000-0000-4000-8000-000000000001"
const testPickupID = "20000000-0000-4000-8000-000000000001"

type pickupRepositoryStub struct {
	tx    PickupTx
	owner func(context.Context, string) (string, error)
	list  func(context.Context, domain.PickupFilter) (domain.PickupPage, error)
}

func (r pickupRepositoryStub) HouseholdID(ctx context.Context, id string) (string, error) {
	return r.owner(ctx, id)
}
func (r pickupRepositoryStub) List(ctx context.Context, filter domain.PickupFilter) (domain.PickupPage, error) {
	return r.list(ctx, filter)
}
func (r pickupRepositoryStub) WithinTransaction(ctx context.Context, fn func(PickupTx) error) error {
	return fn(r.tx)
}

type pickupTxStub struct {
	lock        func(context.Context, string) error
	pickup      func(context.Context, string) (domain.Pickup, error)
	pending     func(context.Context, string) (bool, error)
	insert      func(context.Context, string, domain.WasteType, *bool) (domain.Pickup, error)
	update      func(context.Context, domain.Pickup) (domain.Pickup, error)
	payment     func(context.Context, string) (domain.PaymentStatus, error)
	failPayment func(context.Context, string) error
}

func (tx pickupTxStub) LockHousehold(ctx context.Context, id string) error { return tx.lock(ctx, id) }
func (tx pickupTxStub) PickupForUpdate(ctx context.Context, id string) (domain.Pickup, error) {
	return tx.pickup(ctx, id)
}
func (tx pickupTxStub) HasPendingPayment(ctx context.Context, id string) (bool, error) {
	return tx.pending(ctx, id)
}
func (tx pickupTxStub) InsertPickup(ctx context.Context, id string, kind domain.WasteType, safety *bool) (domain.Pickup, error) {
	return tx.insert(ctx, id, kind, safety)
}
func (tx pickupTxStub) UpdatePickup(ctx context.Context, pickup domain.Pickup) (domain.Pickup, error) {
	return tx.update(ctx, pickup)
}
func (tx pickupTxStub) PaymentStatusForUpdate(ctx context.Context, id string) (domain.PaymentStatus, error) {
	return tx.payment(ctx, id)
}
func (tx pickupTxStub) FailPendingPayment(ctx context.Context, id string) error {
	return tx.failPayment(ctx, id)
}

func pickupFixture() domain.Pickup {
	return domain.Pickup{ID: testPickupID, HouseholdID: pickupHouseholdID, Type: domain.WasteOrganic, Status: domain.PickupPending,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func pickupService(t *testing.T, pickup domain.Pickup) (*Pickup, *pickupTxStub) {
	t.Helper()
	tx := &pickupTxStub{
		lock: func(ctx context.Context, id string) error {
			if id != pickupHouseholdID {
				t.Fatalf("wrong lock: %s", id)
			}
			return nil
		},
		pickup: func(ctx context.Context, id string) (domain.Pickup, error) {
			if id != testPickupID {
				t.Fatalf("wrong pickup: %s", id)
			}
			return pickup, nil
		},
		pending: func(context.Context, string) (bool, error) { return false, nil },
		insert: func(ctx context.Context, id string, kind domain.WasteType, safety *bool) (domain.Pickup, error) {
			return domain.Pickup{ID: testPickupID, HouseholdID: id, Type: kind, Status: domain.PickupPending, SafetyCheck: safety}, nil
		},
		update:  func(ctx context.Context, pickup domain.Pickup) (domain.Pickup, error) { return pickup, nil },
		payment: func(context.Context, string) (domain.PaymentStatus, error) { return "", nil },
	}
	repo := pickupRepositoryStub{tx: tx, owner: func(context.Context, string) (string, error) { return pickupHouseholdID, nil }}
	svc := NewPickup(repo)
	svc.now = func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	return svc, tx
}

func TestCreatePickupValidation(t *testing.T) {
	for _, tc := range []struct {
		name, household string
		kind            domain.WasteType
		field           string
	}{
		{"missing household", "", domain.WastePaper, "household_id"},
		{"bad household", "invalid", domain.WastePaper, "household_id"},
		{"missing type", pickupHouseholdID, "", "type"},
		{"bad type", pickupHouseholdID, "metal", "type"},
		{"missing electronic check", pickupHouseholdID, domain.WasteElectronic, "safety_check"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewPickup(pickupRepositoryStub{}).Create(t.Context(), tc.household, tc.kind, nil)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || validation.Fields[tc.field] == "" {
				t.Fatalf("expected %s error, got %v", tc.field, err)
			}
		})
	}
}

func TestCreatePickupTypesAndSafety(t *testing.T) {
	unchecked := false
	for _, kind := range []domain.WasteType{domain.WasteOrganic, domain.WastePlastic, domain.WastePaper, domain.WasteElectronic} {
		t.Run(string(kind), func(t *testing.T) {
			svc, _ := pickupService(t, pickupFixture())
			var safety *bool
			if kind == domain.WasteElectronic {
				safety = &unchecked
			}
			pickup, err := svc.Create(t.Context(), pickupHouseholdID, kind, safety)
			if err != nil {
				t.Fatal(err)
			}
			if pickup.Status != domain.PickupPending || pickup.Type != kind || pickup.PickupDate != nil {
				t.Fatalf("bad initial pickup: %+v", pickup)
			}
			if kind == domain.WasteElectronic && (pickup.SafetyCheck == nil || *pickup.SafetyCheck) {
				t.Fatal("explicit false was not retained")
			}
		})
	}
}

func TestCreatePickupBlocksPendingPayment(t *testing.T) {
	svc, tx := pickupService(t, pickupFixture())
	tx.pending = func(context.Context, string) (bool, error) { return true, nil }
	tx.insert = func(context.Context, string, domain.WasteType, *bool) (domain.Pickup, error) {
		t.Fatal("pickup inserted despite pending payment")
		return domain.Pickup{}, nil
	}
	if _, err := svc.Create(t.Context(), pickupHouseholdID, domain.WastePaper, nil); !errors.Is(err, domain.ErrPendingPayment) {
		t.Fatalf("got %v", err)
	}
	tx.lock = func(context.Context, string) error { return domain.ErrNotFound }
	if _, err := svc.Create(t.Context(), pickupHouseholdID, domain.WastePaper, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing household: %v", err)
	}
}

func TestSchedulePickupRules(t *testing.T) {
	for _, tc := range []struct {
		name             string
		status           domain.PickupStatus
		kind             domain.WasteType
		stored, supplied *bool
		want             error
	}{
		{"organic", domain.PickupPending, domain.WasteOrganic, nil, nil, nil},
		{"electronic unchecked", domain.PickupPending, domain.WasteElectronic, boolPointer(false), nil, domain.ErrSafetyCheckRequired},
		{"electronic override", domain.PickupPending, domain.WasteElectronic, boolPointer(false), boolPointer(true), nil},
		{"electronic stored check", domain.PickupPending, domain.WasteElectronic, boolPointer(true), nil, nil},
		{"electronic failed override", domain.PickupPending, domain.WasteElectronic, boolPointer(true), boolPointer(false), domain.ErrSafetyCheckRequired},
		{"scheduled", domain.PickupScheduled, domain.WasteOrganic, nil, nil, domain.ErrInvalidTransition},
		{"completed", domain.PickupCompleted, domain.WasteOrganic, nil, nil, domain.ErrInvalidTransition},
		{"canceled", domain.PickupCanceled, domain.WasteOrganic, nil, nil, domain.ErrInvalidTransition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pickup := pickupFixture()
			pickup.Type, pickup.Status, pickup.SafetyCheck = tc.kind, tc.status, tc.stored
			svc, _ := pickupService(t, pickup)
			date := time.Date(2026, 1, 1, 8, 0, 0, 0, time.FixedZone("WIB", 7*3600))
			got, err := svc.Schedule(t.Context(), testPickupID, date, tc.supplied)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == nil && (got.Status != domain.PickupScheduled || got.PickupDate == nil || got.PickupDate.Format(time.RFC3339) != "2026-01-01T01:00:00Z") {
				t.Fatalf("bad scheduled pickup: %+v", got)
			}
			if tc.want == nil && tc.kind == domain.WasteElectronic && (got.SafetyCheck == nil || !*got.SafetyCheck) {
				t.Fatal("safety check not saved")
			}
		})
	}
}

func TestSchedulePickupRejectsInvalidDateAndID(t *testing.T) {
	svc, tx := pickupService(t, pickupFixture())
	for _, date := range []time.Time{{}, svc.now(), svc.now().Add(-time.Second)} {
		_, err := svc.Schedule(t.Context(), testPickupID, date, nil)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Fields["pickup_date"] == "" {
			t.Fatalf("expected date validation, got %v", err)
		}
	}
	if _, err := svc.Schedule(t.Context(), "bad", svc.now().Add(time.Hour), nil); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("bad ID: %v", err)
	}
	date := svc.now().Add(time.Hour)
	tx.lock = func(context.Context, string) error {
		svc.now = func() time.Time { return date.Add(time.Second) }
		return nil
	}
	_, err := svc.Schedule(t.Context(), testPickupID, date, nil)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("date expired while waiting for lock: %v", err)
	}
}

func TestCancelPickupRules(t *testing.T) {
	for _, tc := range []struct {
		status  domain.PickupStatus
		payment domain.PaymentStatus
		want    error
	}{
		{domain.PickupPending, "", nil}, {domain.PickupScheduled, "", nil},
		{domain.PickupPending, domain.PaymentPending, nil}, {domain.PickupPending, domain.PaymentFailed, nil},
		{domain.PickupScheduled, domain.PaymentPaid, domain.ErrPaymentAlreadyPaid},
		{domain.PickupCompleted, "", domain.ErrInvalidTransition}, {domain.PickupCanceled, "", domain.ErrInvalidTransition},
	} {
		t.Run(string(tc.status)+"/"+string(tc.payment), func(t *testing.T) {
			pickup := pickupFixture()
			pickup.Status = tc.status
			svc, tx := pickupService(t, pickup)
			tx.payment = func(context.Context, string) (domain.PaymentStatus, error) { return tc.payment, nil }
			failed := false
			tx.failPayment = func(context.Context, string) error { failed = true; return nil }
			got, err := svc.Cancel(t.Context(), testPickupID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == nil && got.Status != domain.PickupCanceled {
				t.Fatalf("not canceled: %+v", got)
			}
			if failed != (tc.want == nil && tc.payment == domain.PaymentPending) {
				t.Fatal("wrong pending payment side effect")
			}
		})
	}
}

func TestPickupListValidation(t *testing.T) {
	for _, filter := range []domain.PickupFilter{
		{Pagination: domain.Pagination{Page: 0, Limit: 20}},
		{Pagination: domain.Pagination{Page: 1, Limit: 20}, Status: "lost"},
		{Pagination: domain.Pagination{Page: 1, Limit: 20}, HouseholdID: "bad"},
	} {
		_, err := NewPickup(pickupRepositoryStub{}).List(t.Context(), filter)
		if err == nil {
			t.Fatal("invalid filter accepted")
		}
	}
	for _, status := range []domain.PickupStatus{"", domain.PickupPending, domain.PickupScheduled, domain.PickupCompleted, domain.PickupCanceled} {
		svc := NewPickup(pickupRepositoryStub{list: func(ctx context.Context, filter domain.PickupFilter) (domain.PickupPage, error) {
			return domain.PickupPage{Items: []domain.Pickup{}, Total: 0}, nil
		}})
		if _, err := svc.List(t.Context(), domain.PickupFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
}

func boolPointer(value bool) *bool { return &value }
