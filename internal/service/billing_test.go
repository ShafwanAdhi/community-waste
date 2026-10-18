package service

import (
	"context"
	"errors"
	"testing"

	"community-waste/internal/domain"
)

type billingRepositoryStub struct {
	tx    BillingTx
	owner func(context.Context, string) (string, error)
	list  func(context.Context, domain.PaymentFilter) (domain.PaymentPage, error)
}

func (r billingRepositoryStub) HouseholdID(c context.Context, id string) (string, error) {
	return r.owner(c, id)
}
func (r billingRepositoryStub) WithinTransaction(c context.Context, fn func(BillingTx) error) error {
	return fn(r.tx)
}
func (r billingRepositoryStub) List(c context.Context, f domain.PaymentFilter) (domain.PaymentPage, error) {
	return r.list(c, f)
}

type billingTxStub struct {
	*pickupTxStub
	payment       func(context.Context, string) (domain.Payment, error)
	insertPayment func(context.Context, domain.Pickup, domain.Money) (domain.Payment, error)
}

func (r billingTxStub) PaymentForUpdate(c context.Context, id string) (domain.Payment, error) {
	return r.payment(c, id)
}
func (r billingTxStub) InsertPayment(c context.Context, p domain.Pickup, m domain.Money) (domain.Payment, error) {
	return r.insertPayment(c, p, m)
}
func billingService(t *testing.T, p domain.Pickup) (*Billing, *billingTxStub) {
	t.Helper()
	_, pickupTx := pickupService(t, p)
	tx := &billingTxStub{pickupTxStub: pickupTx, payment: func(context.Context, string) (domain.Payment, error) { return domain.Payment{}, domain.ErrNotFound }, insertPayment: func(_ context.Context, p domain.Pickup, m domain.Money) (domain.Payment, error) {
		return domain.Payment{ID: "invoice", HouseholdID: p.HouseholdID, WasteID: p.ID, Amount: m, Status: domain.PaymentPending}, nil
	}}
	return NewBilling(billingRepositoryStub{tx: tx, owner: func(context.Context, string) (string, error) { return p.HouseholdID, nil }}), tx
}

func TestBillingTariffsAndManualPayment(t *testing.T) {
	for _, tc := range []struct {
		kind   domain.WasteType
		amount string
		money  domain.Money
	}{{domain.WasteOrganic, "50000.00", 5000000}, {domain.WastePlastic, "50000", 5000000}, {domain.WastePaper, "50000.0", 5000000}, {domain.WasteElectronic, "100000.00", 10000000}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			p := pickupFixture()
			p.Type = tc.kind
			s, _ := billingService(t, p)
			got, err := s.Create(t.Context(), pickupHouseholdID, testPickupID, tc.amount)
			if err != nil || got.Amount != tc.money || got.Status != domain.PaymentPending || got.WasteID != p.ID {
				t.Fatalf("manual: %+v %v", got, err)
			}
			p.Status = domain.PickupScheduled
			s, tx := billingService(t, p)
			tx.insertPayment = func(_ context.Context, _ domain.Pickup, m domain.Money) (domain.Payment, error) {
				if m != tc.money {
					t.Fatalf("tariff: %v", m)
				}
				return domain.Payment{Amount: m}, nil
			}
			completed, err := s.Complete(t.Context(), p.ID)
			if err != nil || completed.Status != domain.PickupCompleted {
				t.Fatalf("complete: %+v %v", completed, err)
			}
		})
	}
}

func TestManualPaymentValidation(t *testing.T) {
	for _, tc := range []struct{ hh, id, amount, field string }{{"bad", testPickupID, "50000", "household_id"}, {pickupHouseholdID, "bad", "50000", "waste_id"}, {pickupHouseholdID, testPickupID, "0", "amount"}, {pickupHouseholdID, testPickupID, "5e4", "amount"}, {pickupHouseholdID, testPickupID, "50000.001", "amount"}} {
		_, err := NewBilling(billingRepositoryStub{}).Create(t.Context(), tc.hh, tc.id, tc.amount)
		var validation *domain.ValidationError
		if !errors.As(err, &validation) || validation.Fields[tc.field] == "" {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	s, _ := billingService(t, pickupFixture())
	_, err := s.Create(t.Context(), pickupHouseholdID, testPickupID, "49999.99")
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["amount"] == "" {
		t.Fatalf("tariff mismatch: %v", err)
	}
	if _, err := s.Create(t.Context(), "10000000-0000-4000-8000-000000000099", testPickupID, "50000"); !errors.Is(err, domain.ErrHouseholdMismatch) {
		t.Fatalf("ownership: %v", err)
	}
}

func TestManualPaymentPolicies(t *testing.T) {
	for _, status := range []domain.PickupStatus{domain.PickupPending, domain.PickupScheduled, domain.PickupCompleted, domain.PickupCanceled} {
		p := pickupFixture()
		p.Status = status
		s, _ := billingService(t, p)
		_, err := s.Create(t.Context(), pickupHouseholdID, p.ID, "50000")
		if status == domain.PickupPending || status == domain.PickupScheduled {
			if err != nil {
				t.Fatal(err)
			}
		} else if !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("%s: %v", status, err)
		}
	}
	for _, status := range []domain.PaymentStatus{domain.PaymentPending, domain.PaymentPaid, domain.PaymentFailed} {
		s, tx := billingService(t, pickupFixture())
		tx.payment = func(context.Context, string) (domain.Payment, error) {
			return domain.Payment{Status: status, Amount: 5000000}, nil
		}
		tx.insertPayment = func(context.Context, domain.Pickup, domain.Money) (domain.Payment, error) {
			t.Fatal("duplicate insert")
			return domain.Payment{}, nil
		}
		if _, err := s.Create(t.Context(), pickupHouseholdID, testPickupID, "50000"); !errors.Is(err, domain.ErrPaymentExists) {
			t.Fatalf("%s duplicate: %v", status, err)
		}
	}
}

func TestCompletePaymentReuseAndTransitions(t *testing.T) {
	for _, status := range []domain.PickupStatus{domain.PickupPending, domain.PickupCompleted, domain.PickupCanceled} {
		p := pickupFixture()
		p.Status = status
		s, _ := billingService(t, p)
		if _, err := s.Complete(t.Context(), p.ID); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("%s: %v", status, err)
		}
	}
	for _, tc := range []struct {
		status domain.PaymentStatus
		amount domain.Money
		want   error
	}{{domain.PaymentPending, 5000000, nil}, {domain.PaymentPaid, 5000000, nil}, {domain.PaymentFailed, 5000000, domain.ErrPaymentConflict}, {domain.PaymentPending, 1, domain.ErrPaymentConflict}} {
		p := pickupFixture()
		p.Status = domain.PickupScheduled
		s, tx := billingService(t, p)
		tx.payment = func(context.Context, string) (domain.Payment, error) {
			return domain.Payment{Status: tc.status, Amount: tc.amount}, nil
		}
		tx.insertPayment = func(context.Context, domain.Pickup, domain.Money) (domain.Payment, error) {
			t.Fatal("existing payment replaced")
			return domain.Payment{}, nil
		}
		_, err := s.Complete(t.Context(), p.ID)
		if !errors.Is(err, tc.want) {
			t.Fatalf("%+v: %v", tc, err)
		}
	}
	if _, err := NewBilling(billingRepositoryStub{}).Complete(t.Context(), "bad"); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("bad id: %v", err)
	}
}

func TestBillingPropagatesFailures(t *testing.T) {
	failure := errors.New("database failure")
	p := pickupFixture()
	p.Status = domain.PickupScheduled
	for _, stage := range []string{"parent", "payment", "insert", "update"} {
		s, tx := billingService(t, p)
		switch stage {
		case "parent":
			tx.lock = func(context.Context, string) error { return failure }
		case "payment":
			tx.payment = func(context.Context, string) (domain.Payment, error) { return domain.Payment{}, failure }
		case "insert":
			tx.insertPayment = func(context.Context, domain.Pickup, domain.Money) (domain.Payment, error) {
				return domain.Payment{}, failure
			}
		case "update":
			tx.update = func(context.Context, domain.Pickup) (domain.Pickup, error) { return domain.Pickup{}, failure }
		}
		if _, err := s.Complete(t.Context(), p.ID); !errors.Is(err, failure) {
			t.Fatalf("%s: %v", stage, err)
		}
	}
}

func TestPaymentListValidation(t *testing.T) {
	s := NewBilling(billingRepositoryStub{list: func(context.Context, domain.PaymentFilter) (domain.PaymentPage, error) {
		return domain.PaymentPage{}, nil
	}})
	for _, f := range []domain.PaymentFilter{{Pagination: domain.Pagination{Page: 0, Limit: 20}}, {Pagination: domain.Pagination{Page: 1, Limit: 20}, Status: "unknown"}, {Pagination: domain.Pagination{Page: 1, Limit: 20}, HouseholdID: "bad"}} {
		if _, err := s.List(t.Context(), f); err == nil {
			t.Fatalf("accepted %+v", f)
		}
	}
	for _, status := range []domain.PaymentStatus{"", domain.PaymentPending, domain.PaymentPaid, domain.PaymentFailed} {
		if _, err := s.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManualPaymentAcceptsUppercaseHouseholdUUID(t *testing.T) {
	p := pickupFixture()
	p.HouseholdID = "abcdefab-cdef-4abc-8abc-abcdefabcdef"
	billing, tx := billingService(t, p)
	tx.lock = func(_ context.Context, id string) error {
		if id != p.HouseholdID {
			t.Fatalf("must lock actual owner: %s", id)
		}
		return nil
	}
	payment, err := billing.Create(t.Context(), "ABCDEFAB-CDEF-4ABC-8ABC-ABCDEFABCDEF", p.ID, "50000")
	if err != nil || payment.HouseholdID != p.HouseholdID {
		t.Fatalf("uppercase owner: %+v %v", payment, err)
	}
}
