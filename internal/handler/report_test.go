package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"community-waste/internal/domain"
	"community-waste/internal/repository/postgres"
	"community-waste/internal/service"
	"community-waste/internal/testutil"
)

type reportServiceStub struct {
	waste   func(context.Context) ([]domain.WasteSummary, error)
	payment func(context.Context) (domain.PaymentSummary, error)
}

func (s reportServiceStub) WasteSummary(c context.Context) ([]domain.WasteSummary, error) {
	return s.waste(c)
}
func (s reportServiceStub) PaymentSummary(c context.Context) (domain.PaymentSummary, error) {
	return s.payment(c)
}
func reportRouter(s ReportService) http.Handler {
	return New(func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil, nil, s)
}

func TestReportHTTP(t *testing.T) {
	h := reportRouter(reportServiceStub{waste: func(context.Context) ([]domain.WasteSummary, error) {
		return []domain.WasteSummary{{Type: domain.WasteOrganic, Status: domain.PickupPending, Count: 2}}, nil
	}, payment: func(context.Context) (domain.PaymentSummary, error) {
		return domain.PaymentSummary{Statuses: []domain.PaymentStatusSummary{{Status: domain.PaymentPending}, {Status: domain.PaymentPaid, Count: 1, Amount: 5000000}, {Status: domain.PaymentFailed}}, TotalRevenue: 5000000}, nil
	}})
	for _, tc := range []struct{ path, contains string }{{"/api/reports/waste-summary", `"count":2`}, {"/api/reports/payment-summary", `"total_revenue":"50000.00"`}} {
		res := householdRequest(h, "GET", tc.path, "", "")
		if res.Code != 200 || !strings.Contains(res.Body.String(), tc.contains) || strings.Contains(res.Body.String(), `"meta"`) {
			t.Fatalf("%s: %d %s", tc.path, res.Code, res.Body.String())
		}
		res = householdRequest(h, "POST", tc.path, "", "")
		if res.Code != 405 || res.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("method: %d %s", res.Code, res.Body.String())
		}
	}
	failure := errors.New("private SQL connection")
	h = reportRouter(reportServiceStub{waste: func(context.Context) ([]domain.WasteSummary, error) { return nil, failure }, payment: func(context.Context) (domain.PaymentSummary, error) { return domain.PaymentSummary{}, failure }})
	for _, path := range []string{"/api/reports/waste-summary", "/api/reports/payment-summary"} {
		res := householdRequest(h, "GET", path, "", "")
		if res.Code != 500 || strings.Contains(res.Body.String(), "private") || !strings.Contains(res.Body.String(), "internal_error") {
			t.Fatalf("failure: %d %s", res.Code, res.Body.String())
		}
	}
}

func TestReportHTTPEmptyDatabase(t *testing.T) {
	pool := testutil.NewDatabase(t)
	h := reportRouter(service.NewReport(postgres.NewReport(pool)))
	res := householdRequest(h, "GET", "/api/reports/waste-summary", "", "")
	if res.Code != 200 || strings.TrimSpace(res.Body.String()) != `{"data":[]}` {
		t.Fatalf("empty waste: %d %s", res.Code, res.Body.String())
	}
	res = householdRequest(h, "GET", "/api/reports/payment-summary", "", "")
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"total_revenue":"0.00"`) {
		t.Fatalf("empty payment: %d %s", res.Code, res.Body.String())
	}
}
