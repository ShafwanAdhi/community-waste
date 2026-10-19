package service

import (
	"context"
	"errors"
	"testing"

	"community-waste/internal/domain"
)

type reportRepositoryStub struct {
	waste   func(context.Context) ([]domain.WasteSummary, error)
	payment func(context.Context) ([]domain.PaymentStatusSummary, error)
}

func (r reportRepositoryStub) WasteSummary(c context.Context) ([]domain.WasteSummary, error) {
	return r.waste(c)
}
func (r reportRepositoryStub) PaymentSummary(c context.Context) ([]domain.PaymentStatusSummary, error) {
	return r.payment(c)
}

func TestReportPaymentStatusesAndRevenue(t *testing.T) {
	for _, rows := range [][]domain.PaymentStatusSummary{nil, {{Status: domain.PaymentFailed, Count: 2, Amount: 15000000}, {Status: domain.PaymentPaid, Count: 1, Amount: 5000000}, {Status: domain.PaymentPending, Count: 3, Amount: 20000000}}} {
		report := NewReport(reportRepositoryStub{payment: func(context.Context) ([]domain.PaymentStatusSummary, error) { return rows, nil }})
		got, err := report.PaymentSummary(t.Context())
		if err != nil || len(got.Statuses) != 3 {
			t.Fatalf("summary: %+v %v", got, err)
		}
		for i, status := range []domain.PaymentStatus{domain.PaymentPending, domain.PaymentPaid, domain.PaymentFailed} {
			if got.Statuses[i].Status != status {
				t.Fatalf("order: %+v", got)
			}
		}
		if rows == nil {
			if got.TotalRevenue != 0 || got.Statuses[0].Count != 0 || got.Statuses[1].Amount != 0 {
				t.Fatalf("empty: %+v", got)
			}
		} else if got.TotalRevenue != 5000000 || got.Statuses[0].Amount != 20000000 || got.Statuses[2].Count != 2 {
			t.Fatalf("wrong totals: %+v", got)
		}
	}
}

func TestReportWasteAndFailures(t *testing.T) {
	failure := errors.New("repository failure")
	report := NewReport(reportRepositoryStub{waste: func(context.Context) ([]domain.WasteSummary, error) { return nil, nil }})
	rows, err := report.WasteSummary(t.Context())
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatalf("empty: %+v %v", rows, err)
	}
	report = NewReport(reportRepositoryStub{waste: func(context.Context) ([]domain.WasteSummary, error) { return nil, failure }, payment: func(context.Context) ([]domain.PaymentStatusSummary, error) { return nil, failure }})
	if _, err := report.WasteSummary(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if _, err := report.PaymentSummary(t.Context()); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
