package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/repository/postgres"
	"community-waste/internal/service"
	"community-waste/internal/testutil"
)

type billingServiceStub struct {
	create   func(context.Context, string, string, string) (domain.Payment, error)
	list     func(context.Context, domain.PaymentFilter) (domain.PaymentPage, error)
	complete func(context.Context, string) (domain.Pickup, error)
}

func (s billingServiceStub) Create(c context.Context, h, w, a string) (domain.Payment, error) {
	return s.create(c, h, w, a)
}
func (s billingServiceStub) List(c context.Context, f domain.PaymentFilter) (domain.PaymentPage, error) {
	return s.list(c, f)
}
func (s billingServiceStub) Complete(c context.Context, id string) (domain.Pickup, error) {
	return s.complete(c, id)
}
func billingRouter(s BillingService) http.Handler {
	return New(func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, s, nil)
}

func TestBillingHTTPContract(t *testing.T) {
	h := billingRouter(billingServiceStub{
		create: func(_ context.Context, hh, waste, amount string) (domain.Payment, error) {
			if hh != householdID || waste != householdID || amount != "50000.00" {
				t.Fatalf("create fields: %s %s %s", hh, waste, amount)
			}
			return domain.Payment{ID: householdID, HouseholdID: hh, WasteID: waste, Amount: 5000000, Status: domain.PaymentPending}, nil
		},
		list: func(_ context.Context, f domain.PaymentFilter) (domain.PaymentPage, error) {
			if f.Status != domain.PaymentPaid || f.HouseholdID != householdID || f.Pagination.Page != 2 || f.Pagination.Limit != 5 {
				t.Fatalf("filter: %+v", f)
			}
			return domain.PaymentPage{Total: 3}, nil
		},
		complete: func(_ context.Context, id string) (domain.Pickup, error) {
			return domain.Pickup{ID: id, Status: domain.PickupCompleted}, nil
		},
	})
	for _, tc := range []struct {
		method, path, body string
		status             int
		contains           string
	}{
		{"POST", "/api/payments", `{"household_id":"` + householdID + `","waste_id":"` + householdID + `","amount":"50000.00"}`, 201, `"amount":"50000.00"`},
		{"GET", "/api/payments?status=paid&household_id=" + householdID + "&page=2&limit=5", "", 200, `"data":[]`},
		{"PUT", "/api/pickups/" + householdID + "/complete", "", 200, `"status":"completed"`},
	} {
		res := householdRequest(h, tc.method, tc.path, tc.body, "application/json")
		if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.contains) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}

func TestBillingHTTPFailures(t *testing.T) {
	h := billingRouter(billingServiceStub{})
	for _, tc := range []struct {
		method, path, body, media string
		status                    int
		code                      string
	}{
		{"POST", "/api/payments", `{"amount":50000}`, "application/json", 400, "invalid_json"},
		{"POST", "/api/payments", `{"amount":"50000","status":"paid"}`, "application/json", 400, "invalid_json"},
		{"POST", "/api/payments", `{`, "application/json", 400, "invalid_json"},
		{"POST", "/api/payments", `{}`, "text/plain", 415, "unsupported_media_type"},
		{"GET", "/api/payments?status=", "", "", 400, "invalid_filter"},
		{"GET", "/api/payments?status=paid&status=pending", "", "", 400, "invalid_filter"},
		{"GET", "/api/payments?household_id=", "", "", 400, "invalid_filter"},
		{"GET", "/api/payments?page=x", "", "", 400, "invalid_pagination"},
		{"GET", "/api/payments?status=%zz", "", "", 400, "invalid_pagination"},
		{"DELETE", "/api/payments", "", "", 405, "method_not_allowed"},
		{"POST", "/api/pickups/" + householdID + "/complete", "", "", 405, "method_not_allowed"},
	} {
		res := householdRequest(h, tc.method, tc.path, tc.body, tc.media)
		if res.Code != tc.status || !strings.Contains(res.Body.String(), `"code":"`+tc.code+`"`) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{{domain.ErrHouseholdMismatch, 409, "household_mismatch"}, {domain.ErrPaymentExists, 409, "payment_exists"}, {domain.ErrPaymentConflict, 409, "payment_conflict"}, {errors.New("private SQL credentials"), 500, "internal_error"}} {
		h := billingRouter(billingServiceStub{complete: func(context.Context, string) (domain.Pickup, error) { return domain.Pickup{}, tc.err }})
		res := householdRequest(h, "PUT", "/api/pickups/"+householdID+"/complete", "", "")
		if res.Code != tc.status || !strings.Contains(res.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(res.Body.String(), "private") {
			t.Fatalf("%v: %d %s", tc.err, res.Code, res.Body.String())
		}
	}
}

func TestBillingHTTPIntegration(t *testing.T) {
	pool := testutil.NewDatabase(t)
	hh, err := postgres.NewHousehold(pool).Create(t.Context(), "Billing HTTP", "Address")
	if err != nil {
		t.Fatal(err)
	}
	pickups := service.NewPickup(postgres.NewPickup(pool))
	p, err := pickups.Create(t.Context(), hh.ID, domain.WastePaper, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := billingRouter(service.NewBilling(postgres.NewBilling(pool)))
	payload := `{"household_id":"` + hh.ID + `","waste_id":"` + p.ID + `","amount":"50000.00"}`
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/payments", `{}`, 422},
		{"POST", "/api/payments", strings.Replace(payload, `"50000.00"`, `null`, 1), 422},
		{"POST", "/api/payments", strings.Replace(payload, "50000.00", "50000.001", 1), 422},
		{"POST", "/api/payments", strings.Replace(payload, "50000.00", "1.00", 1), 422},
		{"PUT", "/api/pickups/bad/complete", "", 400},
		{"PUT", "/api/pickups/00000000-0000-4000-8000-000000000099/complete", "", 404},
		{"PUT", "/api/pickups/" + p.ID + "/complete", "", 409},
		{"GET", "/api/payments?limit=101", "", 400},
		{"GET", "/api/payments?status=unknown", "", 400},
		{"GET", "/api/payments?household_id=bad", "", 400},
		{"POST", "/api/payments", payload, 201},
		{"POST", "/api/payments", payload, 409},
	} {
		res := householdRequest(h, tc.method, tc.path, tc.body, "application/json")
		if res.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
	if _, err := pickups.Schedule(t.Context(), p.ID, time.Now().Add(time.Hour), nil); err != nil {
		t.Fatal(err)
	}
	res := householdRequest(h, "PUT", "/api/pickups/"+p.ID+"/complete", "", "")
	if res.Code != 200 {
		t.Fatalf("complete: %d %s", res.Code, res.Body.String())
	}
	res = householdRequest(h, "GET", "/api/payments?household_id="+hh.ID+"&status=pending", "", "")
	var result struct {
		Data []struct {
			Amount  string `json:"amount"`
			WasteID string `json:"waste_id"`
		}
		Meta struct{ Total int64 }
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || result.Meta.Total != 1 || len(result.Data) != 1 || result.Data[0].Amount != "50000.00" || result.Data[0].WasteID != p.ID {
		t.Fatalf("invoice list: %d %s", res.Code, res.Body.String())
	}
}
