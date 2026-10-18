package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/repository/postgres"
	"community-waste/internal/service"
	"community-waste/internal/testutil"
)

const householdID = "10000000-0000-4000-8000-000000000001"

type householdRepositoryStub struct {
	create func(context.Context, string, string) (domain.Household, error)
	list   func(context.Context, domain.Pagination) (domain.HouseholdPage, error)
	get    func(context.Context, string) (domain.Household, error)
	delete func(context.Context, string) error
}

func (r householdRepositoryStub) Create(ctx context.Context, owner, address string) (domain.Household, error) {
	return r.create(ctx, owner, address)
}
func (r householdRepositoryStub) List(ctx context.Context, page domain.Pagination) (domain.HouseholdPage, error) {
	return r.list(ctx, page)
}
func (r householdRepositoryStub) Get(ctx context.Context, id string) (domain.Household, error) {
	return r.get(ctx, id)
}
func (r householdRepositoryStub) Delete(ctx context.Context, id string) error {
	return r.delete(ctx, id)
}

func householdRouter(repository service.HouseholdRepository) http.Handler {
	return New(func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), service.NewHousehold(repository), nil, nil)
}

func householdRequest(h http.Handler, method, path, body, contentType string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	return response
}

func TestCreateHouseholdHTTP(t *testing.T) {
	h := householdRouter(householdRepositoryStub{create: func(ctx context.Context, owner, address string) (domain.Household, error) {
		return domain.Household{ID: householdID, OwnerName: owner, Address: address, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, nil
	}})
	payload := "\n\t " + `{"owner_name":"  Ayu  ","address":" Jl. Merdeka 1 "}` + "\r\n"
	res := householdRequest(h, "POST", "/api/households", payload, "application/json; charset=utf-8")
	if res.Code != 201 || res.Header().Get("Location") != "/api/households/"+householdID {
		t.Fatalf("unexpected create: %d %s", res.Code, res.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["id"] != householdID || body.Data["owner_name"] != "Ayu" || body.Data["address"] != "Jl. Merdeka 1" || body.Data["created_at"] != "2026-01-01T00:00:00Z" || body.Data["updated_at"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("unexpected payload: %s", res.Body.String())
	}
}

func TestHouseholdHTTPFailures(t *testing.T) {
	private := errors.New("postgres://user:private-pass@db SELECT secret")
	repository := householdRepositoryStub{
		create: func(context.Context, string, string) (domain.Household, error) { return domain.Household{}, private },
		list: func(context.Context, domain.Pagination) (domain.HouseholdPage, error) {
			return domain.HouseholdPage{}, private
		},
		get:    func(context.Context, string) (domain.Household, error) { return domain.Household{}, domain.ErrNotFound },
		delete: func(context.Context, string) error { return domain.ErrHouseholdInUse },
	}
	h := householdRouter(repository)
	for _, tc := range []struct {
		name, method, path, body, media string
		status                          int
		code                            string
	}{
		{"malformed", "POST", "/api/households", "{", "application/json", 400, "invalid_json"},
		{"empty body", "POST", "/api/households", "", "application/json", 400, "invalid_json"},
		{"null", "POST", "/api/households", "null", "application/json", 400, "invalid_json"},
		{"invalid leading JSON whitespace", "POST", "/api/households", "\v" + `{"owner_name":"Ayu","address":"Address"}`, "application/json", 400, "invalid_json"},
		{"invalid trailing JSON whitespace", "POST", "/api/households", `{"owner_name":"Ayu","address":"Address"}` + "\v", "application/json", 400, "invalid_json"},
		{"array", "POST", "/api/households", "[]", "application/json", 400, "invalid_json"},
		{"wrong field type", "POST", "/api/households", `{"owner_name":42}`, "application/json", 400, "invalid_json"},
		{"unknown field", "POST", "/api/households", `{"owner_name":"Ayu","address":"Address","id":"ignored"}`, "application/json", 400, "invalid_json"},
		{"trailing JSON", "POST", "/api/households", `{"owner_name":"Ayu","address":"Address"} {}`, "application/json", 400, "invalid_json"},
		{"required fields", "POST", "/api/households", `{"owner_name":" \t","address":""}`, "application/json", 422, "validation_failed"},
		{"null in owner", "POST", "/api/households", `{"owner_name":"Ayu\u0000","address":"Address"}`, "application/json", 422, "validation_failed"},
		{"null in address", "POST", "/api/households", `{"owner_name":"Ayu","address":"Address\u0000"}`, "application/json", 422, "validation_failed"},
		{"wrong content type", "POST", "/api/households", `{}`, "text/plain", 415, "unsupported_media_type"},
		{"missing content type", "POST", "/api/households", `{}`, "", 415, "unsupported_media_type"},
		{"large body", "POST", "/api/households", `{"owner_name":"` + strings.Repeat("x", 64*1024) + `","address":"Address"}`, "application/json", 413, "body_too_large"},
		{"bad ID", "GET", "/api/households/not-a-uuid", "", "", 400, "invalid_id"},
		{"bad delete ID", "DELETE", "/api/households/not-a-uuid", "", "", 400, "invalid_id"},
		{"missing household", "GET", "/api/households/" + householdID, "", "", 404, "not_found"},
		{"referenced household", "DELETE", "/api/households/" + householdID, "", "", 409, "household_in_use"},
		{"zero page", "GET", "/api/households?page=0", "", "", 400, "invalid_pagination"},
		{"negative page", "GET", "/api/households?page=-1", "", "", 400, "invalid_pagination"},
		{"large limit", "GET", "/api/households?limit=101", "", "", 400, "invalid_pagination"},
		{"non-numeric page", "GET", "/api/households?page=abc", "", "", 400, "invalid_pagination"},
		{"empty page", "GET", "/api/households?page=", "", "", 400, "invalid_pagination"},
		{"duplicate page", "GET", "/api/households?page=1&page=2", "", "", 400, "invalid_pagination"},
		{"malformed query encoding", "GET", "/api/households?page=%zz", "", "", 400, "invalid_pagination"},
		{"offset overflow", "GET", "/api/households?page=9223372036854775807&limit=100", "", "", 400, "invalid_pagination"},
		{"backend failure", "POST", "/api/households", `{"owner_name":"Ayu","address":"Address"}`, "application/json", 500, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := householdRequest(h, tc.method, tc.path, tc.body, tc.media)
			var body struct {
				Error struct {
					Code   string            `json:"code"`
					Fields map[string]string `json:"fields"`
				} `json:"error"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if res.Code != tc.status || body.Error.Code != tc.code || res.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected response: %d %s", res.Code, res.Body.String())
			}
			if tc.name == "required fields" && (body.Error.Fields["owner_name"] == "" || body.Error.Fields["address"] == "") {
				t.Fatal("missing field errors")
			}
			if tc.name == "null in owner" && body.Error.Fields["owner_name"] == "" {
				t.Fatal("missing owner field error")
			}
			if tc.name == "null in address" && body.Error.Fields["address"] == "" {
				t.Fatal("missing address field error")
			}
			if strings.Contains(res.Body.String(), "private-pass") || strings.Contains(res.Body.String(), "SELECT") {
				t.Fatal("response leaked internal error")
			}
		})
	}
}

func TestListHouseholdsHTTP(t *testing.T) {
	for _, tc := range []struct {
		query       string
		page, limit int64
	}{{"", 1, 20}, {"?page=2&limit=1", 2, 1}} {
		h := householdRouter(householdRepositoryStub{list: func(ctx context.Context, page domain.Pagination) (domain.HouseholdPage, error) {
			if page.Page != tc.page || page.Limit != tc.limit {
				t.Fatalf("unexpected pagination: %+v", page)
			}
			return domain.HouseholdPage{Total: 7}, nil
		}})
		res := householdRequest(h, "GET", "/api/households"+tc.query, "", "")
		var body struct {
			Data json.RawMessage                    `json:"data"`
			Meta struct{ Page, Limit, Total int64 } `json:"meta"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if res.Code != 200 || string(body.Data) != "[]" || body.Meta.Page != tc.page || body.Meta.Limit != tc.limit || body.Meta.Total != 7 {
			t.Fatalf("unexpected list: %d %s", res.Code, res.Body.String())
		}
	}
}

func TestHouseholdMethods(t *testing.T) {
	h := householdRouter(householdRepositoryStub{})
	for _, tc := range []struct{ method, path, allow string }{
		{"PUT", "/api/households", "GET, HEAD, POST"},
		{"POST", "/api/households/" + householdID, "DELETE, GET, HEAD"},
	} {
		res := householdRequest(h, tc.method, tc.path, "", "")
		if res.Code != 405 || res.Header().Get("Allow") != tc.allow || !json.Valid(res.Body.Bytes()) {
			t.Fatalf("unexpected method response: %d %s", res.Code, res.Body.String())
		}
	}
}

func TestHouseholdHTTPWithPostgres(t *testing.T) {
	pool := testutil.NewDatabase(t)
	h := New(pool.Ping, slog.New(slog.NewTextHandler(io.Discard, nil)), service.NewHousehold(postgres.NewHousehold(pool)), nil, nil)
	res := householdRequest(h, "POST", "/api/households", `{"owner_name":"Ayu","address":"Jl. Merdeka 1"}`, "application/json")
	if res.Code != 201 {
		t.Fatalf("create: %d %s", res.Code, res.Body.String())
	}
	var created struct {
		Data domain.Household `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	path := "/api/households/" + created.Data.ID
	if res := householdRequest(h, "GET", path, "", ""); res.Code != 200 {
		t.Fatalf("get: %d %s", res.Code, res.Body.String())
	}
	res = householdRequest(h, "GET", "/api/households", "", "")
	var list struct {
		Data []domain.Household    `json:"data"`
		Meta struct{ Total int64 } `json:"meta"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if res.Code != 200 || list.Meta.Total != 1 || len(list.Data) != 1 || list.Data[0].ID != created.Data.ID {
		t.Fatalf("list: %d %s", res.Code, res.Body.String())
	}
	res = householdRequest(h, "DELETE", path, "", "")
	if res.Code != 204 || res.Body.Len() != 0 {
		t.Fatalf("delete: %d %s", res.Code, res.Body.String())
	}
	if res := householdRequest(h, "GET", path, "", ""); res.Code != 404 {
		t.Fatalf("get deleted: %d", res.Code)
	}
	if res := householdRequest(h, "DELETE", path, "", ""); res.Code != 404 {
		t.Fatalf("delete missing: %d", res.Code)
	}
	res = householdRequest(h, "POST", "/api/households", `{"owner_name":"Budi","address":"Address"}`, "application/json")
	if err := json.Unmarshal(res.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO pickups(id,household_id,type) VALUES (gen_random_uuid(),$1,'organic')", created.Data.ID); err != nil {
		t.Fatal(err)
	}
	res = householdRequest(h, "DELETE", "/api/households/"+created.Data.ID, "", "")
	if res.Code != 409 {
		t.Fatalf("referenced delete: %d %s", res.Code, res.Body.String())
	}
	if res := householdRequest(h, "GET", "/api/households/"+created.Data.ID, "", ""); res.Code != 200 {
		t.Fatalf("referenced household lost: %d", res.Code)
	}
}
