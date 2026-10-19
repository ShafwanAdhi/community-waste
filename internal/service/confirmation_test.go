package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"community-waste/internal/domain"
)

const confirmPaymentID = "30000000-0000-4000-8000-000000000001"

var pdfProof = []byte("%PDF-1.4\nproof fixture\n%%EOF\n")

type confirmationRepositoryStub struct {
	tx        ConfirmationTx
	reference func(context.Context, string) (domain.PaymentReference, error)
	finish    func(error) error
}

func (r confirmationRepositoryStub) Reference(c context.Context, id string) (domain.PaymentReference, error) {
	return r.reference(c, id)
}
func (r confirmationRepositoryStub) WithinTransaction(c context.Context, fn func(ConfirmationTx) error) error {
	err := fn(r.tx)
	if r.finish != nil {
		return r.finish(err)
	}
	return err
}

type confirmationTxStub struct {
	*pickupTxStub
	payment func(context.Context, string) (domain.Payment, error)
	mark    func(context.Context, string, string) (domain.Payment, error)
}

func (r confirmationTxStub) PaymentByIDForUpdate(c context.Context, id string) (domain.Payment, error) {
	return r.payment(c, id)
}
func (r confirmationTxStub) MarkPaid(c context.Context, id, path string) (domain.Payment, error) {
	return r.mark(c, id, path)
}

type proofStoreStub struct {
	save   func(context.Context, []byte, string) (string, error)
	delete func(string) error
}

func (s proofStoreStub) Save(c context.Context, b []byte, e string) (string, error) {
	return s.save(c, b, e)
}
func (s proofStoreStub) Delete(p string) error { return s.delete(p) }
func confirmationFixture(t *testing.T) (confirmationRepositoryStub, *confirmationTxStub, proofStoreStub) {
	t.Helper()
	p := pickupFixture()
	_, pickupTx := pickupService(t, p)
	tx := &confirmationTxStub{pickupTxStub: pickupTx, payment: func(context.Context, string) (domain.Payment, error) {
		return domain.Payment{ID: confirmPaymentID, HouseholdID: pickupHouseholdID, WasteID: testPickupID, Status: domain.PaymentPending}, nil
	}, mark: func(_ context.Context, id, path string) (domain.Payment, error) {
		now := time.Now().UTC()
		return domain.Payment{ID: id, Status: domain.PaymentPaid, ProofFileURL: &path, PaymentDate: &now}, nil
	}}
	repo := confirmationRepositoryStub{tx: tx, reference: func(context.Context, string) (domain.PaymentReference, error) {
		return domain.PaymentReference{HouseholdID: pickupHouseholdID, WasteID: testPickupID}, nil
	}}
	store := proofStoreStub{save: func(_ context.Context, b []byte, ext string) (string, error) {
		if string(b) != string(pdfProof) || ext != ".pdf" {
			t.Fatal("wrong proof")
		}
		return "uploads/new.pdf", nil
	}, delete: func(string) error { t.Fatal("unexpected cleanup"); return nil }}
	return repo, tx, store
}

func TestConfirmationValidationAndSuccess(t *testing.T) {
	repo, _, store := confirmationFixture(t)
	svc := NewConfirmation(repo, store)
	payment, err := svc.Confirm(t.Context(), confirmPaymentID, pdfProof)
	if err != nil || payment.Status != domain.PaymentPaid || payment.PaymentDate == nil || payment.ProofFileURL == nil {
		t.Fatalf("confirmed: %+v %v", payment, err)
	}
	for _, tc := range []struct {
		data []byte
		want error
	}{{[]byte("not PDF"), domain.ErrUnsupportedProof}, {[]byte(strings.Repeat("x", domain.MaxProofSize+1)), domain.ErrProofTooLarge}} {
		if _, err := NewConfirmation(confirmationRepositoryStub{}, proofStoreStub{}).Confirm(t.Context(), confirmPaymentID, tc.data); !errors.Is(err, tc.want) {
			t.Fatalf("validation: %v", err)
		}
	}
	if _, err := svc.Confirm(t.Context(), "bad", pdfProof); !errors.Is(err, domain.ErrInvalidID) {
		t.Fatalf("id: %v", err)
	}
	_, err = svc.Confirm(t.Context(), confirmPaymentID, nil)
	var validation *domain.ValidationError
	if !errors.As(err, &validation) || validation.Fields["proof_file"] == "" {
		t.Fatalf("missing: %v", err)
	}
}

func TestConfirmationRejectsTerminalWithoutSaving(t *testing.T) {
	for _, status := range []domain.PaymentStatus{domain.PaymentPaid, domain.PaymentFailed} {
		repo, tx, _ := confirmationFixture(t)
		tx.payment = func(context.Context, string) (domain.Payment, error) { return domain.Payment{Status: status}, nil }
		_, err := NewConfirmation(repo, proofStoreStub{}).Confirm(t.Context(), confirmPaymentID, pdfProof)
		if !errors.Is(err, domain.ErrPaymentNotPending) {
			t.Fatalf("%s: %v", status, err)
		}
	}
	repo, tx, _ := confirmationFixture(t)
	tx.pickup = func(context.Context, string) (domain.Pickup, error) {
		p := pickupFixture()
		p.Status = domain.PickupCanceled
		return p, nil
	}
	if _, err := NewConfirmation(repo, proofStoreStub{}).Confirm(t.Context(), confirmPaymentID, pdfProof); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("canceled: %v", err)
	}
}

func TestConfirmationStorageFailureAndRollbackCleanup(t *testing.T) {
	failure := errors.New("injected failure")
	repo, tx, store := confirmationFixture(t)
	tx.mark = func(context.Context, string, string) (domain.Payment, error) {
		t.Fatal("updated despite failed storage")
		return domain.Payment{}, nil
	}
	store.save = func(context.Context, []byte, string) (string, error) { return "", failure }
	if _, err := NewConfirmation(repo, store).Confirm(t.Context(), confirmPaymentID, pdfProof); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	for _, stage := range []string{"update", "transaction"} {
		repo, tx, store := confirmationFixture(t)
		deleted := false
		store.delete = func(path string) error { deleted = path == "uploads/new.pdf"; return nil }
		if stage == "update" {
			tx.mark = func(context.Context, string, string) (domain.Payment, error) { return domain.Payment{}, failure }
		} else {
			repo.finish = func(error) error { return failure }
		}
		if _, err := NewConfirmation(repo, store).Confirm(t.Context(), confirmPaymentID, pdfProof); !errors.Is(err, failure) || !deleted {
			t.Fatalf("%s: %v deleted=%v", stage, err, deleted)
		}
	}
	repo, _, store = confirmationFixture(t)
	repo.reference = func(context.Context, string) (domain.PaymentReference, error) {
		return domain.PaymentReference{}, domain.ErrNotFound
	}
	if _, err := NewConfirmation(repo, store).Confirm(t.Context(), confirmPaymentID, pdfProof); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
}

func TestPaymentDateRangeValidation(t *testing.T) {
	from := time.Date(2026, 1, 1, 7, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	to := from.Add(time.Hour)
	svc := NewBilling(billingRepositoryStub{list: func(_ context.Context, f domain.PaymentFilter) (domain.PaymentPage, error) {
		if f.DateFrom != nil && f.DateFrom.Location() != time.UTC {
			t.Fatal("date not UTC")
		}
		return domain.PaymentPage{}, nil
	}})
	for _, f := range []domain.PaymentFilter{{DateFrom: &from}, {DateTo: &to}, {DateFrom: &from, DateTo: &to}} {
		f.Pagination = domain.Pagination{Page: 1, Limit: 20}
		if _, err := svc.List(t.Context(), f); err != nil {
			t.Fatal(err)
		}
	}
	for _, end := range []time.Time{from, from.Add(-time.Hour)} {
		if _, err := svc.List(t.Context(), domain.PaymentFilter{Pagination: domain.Pagination{Page: 1, Limit: 20}, DateFrom: &from, DateTo: &end}); !errors.Is(err, domain.ErrInvalidFilter) {
			t.Fatalf("invalid range: %v", err)
		}
	}
}

func TestProofContentTypesAndSizeBoundary(t *testing.T) {
	for _, tc := range []struct {
		data      []byte
		extension string
	}{
		{[]byte{0xff, 0xd8, 0xff, 0xe0}, ".jpg"},
		{[]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, ".png"},
		{pdfProof, ".pdf"},
	} {
		extension, err := proofExtension(tc.data)
		if err != nil || extension != tc.extension {
			t.Fatalf("content %x: %s %v", tc.data, extension, err)
		}
	}
	exact := make([]byte, domain.MaxProofSize)
	copy(exact, pdfProof)
	if extension, err := proofExtension(exact); err != nil || extension != ".pdf" {
		t.Fatalf("exact limit: %s %v", extension, err)
	}
	if _, err := proofExtension(append(exact, 0)); !errors.Is(err, domain.ErrProofTooLarge) {
		t.Fatalf("over limit: %v", err)
	}
}

func TestConfirmationRetainsProofWhenCommitOutcomeIsUnknown(t *testing.T) {
	repo, _, store := confirmationFixture(t)
	repo.finish = func(error) error { return domain.ErrCommitUncertain }
	store.delete = func(string) error { t.Fatal("must retain potentially committed proof"); return nil }
	if _, err := NewConfirmation(repo, store).Confirm(t.Context(), confirmPaymentID, pdfProof); !errors.Is(err, domain.ErrCommitUncertain) {
		t.Fatalf("commit outcome: %v", err)
	}
}

func TestConfirmationCleanupAfterContextCancellation(t *testing.T) {
	repo, tx, store := confirmationFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	removed := false
	tx.mark = func(context.Context, string, string) (domain.Payment, error) {
		cancel()
		return domain.Payment{}, ctx.Err()
	}
	store.delete = func(path string) error { removed = path == "uploads/new.pdf"; return nil }
	if _, err := NewConfirmation(repo, store).Confirm(ctx, confirmPaymentID, pdfProof); !errors.Is(err, context.Canceled) || !removed {
		t.Fatalf("cleanup: %v removed=%v", err, removed)
	}
}
