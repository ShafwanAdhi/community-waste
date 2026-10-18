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
)

func TestHealth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		checkErr   error
		wantStatus int
	}{
		{"healthy", nil, http.StatusOK},
		{"database unavailable", errors.New("password=private-pass"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(func(context.Context) error { return tc.checkErr }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status=%d, want %d", rec.Code, tc.wantStatus)
			}
			if rec.Header().Get("Content-Type") != "application/json" || !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("expected JSON response: %s", rec.Body.String())
			}
			var body struct {
				Data struct {
					Status string `json:"status"`
				} `json:"data"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.checkErr == nil && body.Data.Status != "ok" {
				t.Fatal("missing healthy status")
			}
			if tc.checkErr != nil && body.Error.Code != "service_unavailable" {
				t.Fatal("missing public error code")
			}
			if strings.Contains(rec.Body.String(), "private-pass") {
				t.Fatal("response leaked database details")
			}
		})
	}
}

func TestRoutingErrorsAreJSON(t *testing.T) {
	h := New(func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil)
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{http.MethodGet, "/missing", 404, "not_found"},
		{http.MethodPost, "/healthz", 405, "method_not_allowed"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, nil))
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if rec.Code != tc.status || body.Error.Code != tc.code {
				t.Fatalf("unexpected error: status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.status == 405 && rec.Header().Get("Allow") != "GET, HEAD" {
				t.Fatal("missing allowed methods")
			}
		})
	}
}

func TestHealthPropagatesRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var received error
	h := New(func(ctx context.Context) error { received = ctx.Err(); return received }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(ctx))
	if !errors.Is(received, context.Canceled) {
		t.Fatalf("check received cancellation %v", received)
	}
}
