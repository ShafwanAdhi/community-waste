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

type pickupServiceStub struct {
	create   func(context.Context, string, domain.WasteType, *bool) (domain.Pickup, error)
	list     func(context.Context, domain.PickupFilter) (domain.PickupPage, error)
	schedule func(context.Context, string, time.Time, *bool) (domain.Pickup, error)
	cancel   func(context.Context, string) (domain.Pickup, error)
}

func (s pickupServiceStub) Create(ctx context.Context, h string, k domain.WasteType, b *bool) (domain.Pickup, error) {
	return s.create(ctx, h, k, b)
}
func (s pickupServiceStub) List(ctx context.Context, f domain.PickupFilter) (domain.PickupPage, error) {
	return s.list(ctx, f)
}
func (s pickupServiceStub) Schedule(ctx context.Context, id string, d time.Time, b *bool) (domain.Pickup, error) {
	return s.schedule(ctx, id, d, b)
}
func (s pickupServiceStub) Cancel(ctx context.Context, id string) (domain.Pickup, error) {
	return s.cancel(ctx, id)
}
func pickupRouter(s PickupService) http.Handler {
	return New(func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, s, nil, nil, nil)
}

func TestPickupHTTPContract(t *testing.T) {
	h := pickupRouter(pickupServiceStub{
		create: func(_ context.Context, hh string, k domain.WasteType, b *bool) (domain.Pickup, error) {
			if hh != householdID || k != domain.WasteElectronic || b == nil || *b {
				t.Fatalf("create input: %s %s %v", hh, k, b)
			}
			return domain.Pickup{ID: householdID, HouseholdID: hh, Type: k, Status: domain.PickupPending, SafetyCheck: b}, nil
		},
		list: func(_ context.Context, f domain.PickupFilter) (domain.PickupPage, error) {
			if f.HouseholdID != householdID || f.Status != domain.PickupScheduled || f.Pagination.Page != 2 || f.Pagination.Limit != 5 {
				t.Fatalf("filter: %+v", f)
			}
			return domain.PickupPage{Total: 3}, nil
		},
		schedule: func(_ context.Context, id string, d time.Time, b *bool) (domain.Pickup, error) {
			if id != householdID || d.Format(time.RFC3339) != "2027-01-01T08:00:00+07:00" || b == nil || !*b {
				t.Fatalf("schedule: %s %v %v", id, d, b)
			}
			utc := d.UTC()
			return domain.Pickup{ID: id, Status: domain.PickupScheduled, PickupDate: &utc}, nil
		},
		cancel: func(_ context.Context, id string) (domain.Pickup, error) {
			return domain.Pickup{ID: id, Status: domain.PickupCanceled}, nil
		},
	})
	for _, tc := range []struct {
		method, path, body string
		status             int
		contains           string
	}{
		{"POST", "/api/pickups", `{"household_id":"` + householdID + `","type":"electronic","safety_check":false}`, 201, `"safety_check":false`},
		{"GET", "/api/pickups?page=2&limit=5&status=scheduled&household_id=" + householdID, "", 200, `"data":[]`},
		{"PUT", "/api/pickups/" + householdID + "/schedule", `{"pickup_date":"2027-01-01T08:00:00+07:00","safety_check":true}`, 200, `"pickup_date":"2027-01-01T01:00:00Z"`},
		{"PUT", "/api/pickups/" + householdID + "/cancel", "", 200, `"status":"canceled"`},
	} {
		res := householdRequest(h, tc.method, tc.path, tc.body, "application/json")
		if res.Code != tc.status || !strings.Contains(res.Body.String(), tc.contains) {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}

func TestPickupHTTPFailures(t *testing.T) {
	h := pickupRouter(pickupServiceStub{})
	for _, tc := range []struct {
		method, path, body, media string
		status                    int
		code                      string
	}{
		{"POST", "/api/pickups", `{"type":"paper","extra":true}`, "application/json", 400, "invalid_json"},
		{"POST", "/api/pickups", `{"safety_check":"false"}`, "application/json", 400, "invalid_json"},
		{"POST", "/api/pickups", "null", "application/json", 400, "invalid_json"},
		{"POST", "/api/pickups", "{}", "text/plain", 415, "unsupported_media_type"},
		{"PUT", "/api/pickups/" + householdID + "/schedule", `{}`, "application/json", 422, "validation_failed"},
		{"PUT", "/api/pickups/" + householdID + "/schedule", `{"pickup_date":42}`, "application/json", 400, "invalid_json"},
		{"PUT", "/api/pickups/" + householdID + "/schedule", `{"pickup_date":"tomorrow"}`, "application/json", 422, "validation_failed"},
		{"GET", "/api/pickups?status=", "", "", 400, "invalid_filter"},
		{"GET", "/api/pickups?status=pending&status=scheduled", "", "", 400, "invalid_filter"},
		{"GET", "/api/pickups?household_id=", "", "", 400, "invalid_filter"},
		{"GET", "/api/pickups?limit=x", "", "", 400, "invalid_pagination"},
		{"GET", "/api/pickups?status=%zz", "", "", 400, "invalid_pagination"},
		{"DELETE", "/api/pickups", "", "", 405, "method_not_allowed"},
		{"POST", "/api/pickups/" + householdID + "/schedule", "", "", 405, "method_not_allowed"},
		{"GET", "/api/pickups/" + householdID + "/cancel", "", "", 405, "method_not_allowed"},
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
	}{
		{domain.ErrPendingPayment, 409, "pending_payment"}, {domain.ErrInvalidTransition, 409, "invalid_transition"}, {domain.ErrSafetyCheckRequired, 409, "safety_check_required"}, {domain.ErrPaymentAlreadyPaid, 409, "payment_already_paid"}, {domain.ErrNotFound, 404, "not_found"}, {domain.ErrInvalidID, 400, "invalid_id"}, {domain.ErrInvalidFilter, 400, "invalid_filter"}, {errors.New("private database error"), 500, "internal_error"},
	} {
		h := pickupRouter(pickupServiceStub{cancel: func(context.Context, string) (domain.Pickup, error) { return domain.Pickup{}, tc.err }})
		res := householdRequest(h, "PUT", "/api/pickups/"+householdID+"/cancel", "", "")
		if res.Code != tc.status || !strings.Contains(res.Body.String(), `"code":"`+tc.code+`"`) || strings.Contains(res.Body.String(), "private") {
			t.Fatalf("%v: %d %s", tc.err, res.Code, res.Body.String())
		}
	}
}

func TestPickupHTTPIntegration(t *testing.T) {
	pool := testutil.NewDatabase(t)
	hh, err := postgres.NewHousehold(pool).Create(t.Context(), "Pickup HTTP", "Address")
	if err != nil {
		t.Fatal(err)
	}
	h := pickupRouter(service.NewPickup(postgres.NewPickup(pool)))
	for _, body := range []string{`{}`, `{"household_id":"` + hh.ID + `","type":"electronic"}`, `{"household_id":"` + hh.ID + `","type":"electronic","safety_check":null}`} {
		res := householdRequest(h, "POST", "/api/pickups", body, "application/json")
		if res.Code != 422 {
			t.Fatalf("invalid create: %d %s", res.Code, res.Body.String())
		}
	}
	res := householdRequest(h, "POST", "/api/pickups", `{"household_id":"`+hh.ID+`","type":"paper"}`, "application/json")
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Data domain.Pickup `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	id := body.Data.ID
	for _, tc := range []struct {
		method, path, payload string
		status                int
	}{
		{"GET", "/api/pickups?status=unknown", "", 400}, {"GET", "/api/pickups?household_id=bad", "", 400}, {"GET", "/api/pickups?limit=101", "", 400},
		{"PUT", "/api/pickups/bad/cancel", "", 400}, {"PUT", "/api/pickups/00000000-0000-4000-8000-000000000099/cancel", "", 404},
		{"PUT", "/api/pickups/" + id + "/schedule", `{"pickup_date":"2000-01-01T00:00:00Z"}`, 422},
		{"PUT", "/api/pickups/" + id + "/schedule", `{"pickup_date":"` + time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `"}`, 200},
		{"PUT", "/api/pickups/" + id + "/cancel", "", 200}, {"PUT", "/api/pickups/" + id + "/cancel", "", 409},
	} {
		res := householdRequest(h, tc.method, tc.path, tc.payload, "application/json")
		if res.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, res.Code, res.Body.String())
		}
	}
}
