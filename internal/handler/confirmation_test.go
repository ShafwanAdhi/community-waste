package handler

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"community-waste/internal/domain"
	"community-waste/internal/repository/postgres"
	"community-waste/internal/service"
	"community-waste/internal/storage"
	"community-waste/internal/testutil"
)

type confirmationServiceStub struct {
	confirm func(context.Context, string, []byte) (domain.Payment, error)
}

func (s confirmationServiceStub) Confirm(c context.Context, id string, b []byte) (domain.Payment, error) {
	return s.confirm(c, id, b)
}
func confirmationRouter(s ConfirmationService) http.Handler {
	return New(func(context.Context) error { return nil }, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, nil, nil, s)
}
func multipartRequest(t *testing.T, h http.Handler, id string, files [][]byte, names []string, truncate bool) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for i, data := range files {
		part, err := writer.CreateFormFile(names[i], "../../untrusted.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if !truncate {
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest("PUT", "/api/payments/"+id+"/confirm", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}

func TestConfirmationHTTPMultipart(t *testing.T) {
	h := confirmationRouter(confirmationServiceStub{confirm: func(_ context.Context, id string, b []byte) (domain.Payment, error) {
		if id != householdID || string(b) != "%PDF-1.4\n%%EOF" {
			t.Fatalf("input: %s %q", id, b)
		}
		path := "uploads/random.pdf"
		return domain.Payment{ID: id, Status: domain.PaymentPaid, ProofFileURL: &path}, nil
	}})
	res := multipartRequest(t, h, householdID, [][]byte{[]byte("%PDF-1.4\n%%EOF")}, []string{"proof_file"}, false)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"status":"paid"`) {
		t.Fatalf("success: %d %s", res.Code, res.Body.String())
	}
	for _, tc := range []struct {
		name     string
		files    [][]byte
		names    []string
		truncate bool
		status   int
	}{
		{"missing", nil, nil, false, 422}, {"empty", [][]byte{{}}, []string{"proof_file"}, false, 422},
		{"unknown", [][]byte{[]byte("proof")}, []string{"other"}, false, 400},
		{"duplicate", [][]byte{[]byte("a"), []byte("b")}, []string{"proof_file", "proof_file"}, false, 400},
		{"truncated", [][]byte{[]byte("%PDF-1.4\n%%EOF")}, []string{"proof_file"}, true, 400},
		{"too large", [][]byte{bytes.Repeat([]byte("x"), domain.MaxProofSize+1)}, []string{"proof_file"}, false, 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := multipartRequest(t, confirmationRouter(confirmationServiceStub{}), householdID, tc.files, tc.names, tc.truncate)
			if res.Code != tc.status {
				t.Fatalf("%d %s", res.Code, res.Body.String())
			}
		})
	}
	for _, media := range []string{"application/json", "multipart/form-data"} {
		res := householdRequest(h, "PUT", "/api/payments/"+householdID+"/confirm", "{}", media)
		want := 415
		if media == "multipart/form-data" {
			want = 400
		}
		if res.Code != want {
			t.Fatalf("%s: %d %s", media, res.Code, res.Body.String())
		}
	}
}

func TestPaymentDateFilterHTTP(t *testing.T) {
	h := billingRouter(billingServiceStub{list: func(_ context.Context, f domain.PaymentFilter) (domain.PaymentPage, error) {
		if f.DateFrom == nil || f.DateTo == nil || f.DateFrom.Format(time.RFC3339) != "2026-10-18T01:00:00Z" || !f.DateTo.After(*f.DateFrom) {
			t.Fatalf("bounds: %+v", f)
		}
		return domain.PaymentPage{}, nil
	}})
	res := householdRequest(h, "GET", "/api/payments?date_from=2026-10-18T08:00:00%2B07:00&date_to=2026-10-19T00:00:00Z", "", "")
	if res.Code != 200 {
		t.Fatalf("bounds: %d %s", res.Code, res.Body.String())
	}
	for _, query := range []string{"date_from=", "date_to=tomorrow", "date_from=a&date_from=b", "date_to=2026-10-18", "date_from=2026-10-18T08:00:00+07:00"} {
		res := householdRequest(h, "GET", "/api/payments?"+query, "", "")
		if res.Code != 400 {
			t.Fatalf("%s: %d %s", query, res.Code, res.Body.String())
		}
	}
}

func TestConfirmationHTTPIntegration(t *testing.T) {
	pool := testutil.NewDatabase(t)
	hh, err := postgres.NewHousehold(pool).Create(t.Context(), "Confirmation HTTP", "Address")
	if err != nil {
		t.Fatal(err)
	}
	pickups := service.NewPickup(postgres.NewPickup(pool))
	p, err := pickups.Create(t.Context(), hh.ID, domain.WastePaper, nil)
	if err != nil {
		t.Fatal(err)
	}
	invoice, err := service.NewBilling(postgres.NewBilling(pool)).Create(t.Context(), hh.ID, p.ID, "50000")
	if err != nil {
		t.Fatal(err)
	}
	h := confirmationRouter(service.NewConfirmation(postgres.NewConfirmation(pool), storage.NewLocalProof(t.TempDir())))
	for _, tc := range []struct {
		content []byte
		status  int
	}{
		{[]byte("plain text named as file"), 415}, {[]byte("%PDF-1.4\n%%EOF"), 200}, {[]byte("%PDF-1.4\nreplacement\n%%EOF"), 409},
	} {
		res := multipartRequest(t, h, invoice.ID, [][]byte{tc.content}, []string{"proof_file"}, false)
		if res.Code != tc.status {
			t.Fatalf("%d %s", res.Code, res.Body.String())
		}
	}
	res := multipartRequest(t, h, "bad", [][]byte{[]byte("%PDF-1.4\n%%EOF")}, []string{"proof_file"}, false)
	if res.Code != 400 {
		t.Fatalf("id: %d %s", res.Code, res.Body.String())
	}
}

func TestConfirmationHTTPImagesExactLimitAndStorageFailure(t *testing.T) {
	var pngProof, jpegProof bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	if err := png.Encode(&pngProof, picture); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegProof, picture, nil); err != nil {
		t.Fatal(err)
	}
	exact := make([]byte, domain.MaxProofSize)
	copy(exact, []byte("%PDF-1.4\n%%EOF"))
	for _, tc := range []struct {
		name    string
		content []byte
		broken  bool
		status  int
	}{{"png", pngProof.Bytes(), false, 200}, {"jpeg", jpegProof.Bytes(), false, 200}, {"exact limit", exact, false, 200}, {"storage error", []byte("%PDF-1.4\n%%EOF"), true, 500}} {
		t.Run(tc.name, func(t *testing.T) {
			pool := testutil.NewDatabase(t)
			hh, err := postgres.NewHousehold(pool).Create(t.Context(), "Upload HTTP", "Address")
			if err != nil {
				t.Fatal(err)
			}
			p, err := service.NewPickup(postgres.NewPickup(pool)).Create(t.Context(), hh.ID, domain.WastePaper, nil)
			if err != nil {
				t.Fatal(err)
			}
			invoice, err := service.NewBilling(postgres.NewBilling(pool)).Create(t.Context(), hh.ID, p.ID, "50000")
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if tc.broken {
				root = filepath.Join(root, "file")
				if err := os.WriteFile(root, []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h := confirmationRouter(service.NewConfirmation(postgres.NewConfirmation(pool), storage.NewLocalProof(root)))
			res := multipartRequest(t, h, invoice.ID, [][]byte{tc.content}, []string{"proof_file"}, false)
			if res.Code != tc.status {
				t.Fatalf("%d %s", res.Code, res.Body.String())
			}
			if tc.broken {
				var status string
				var path *string
				if err := pool.QueryRow(t.Context(), "SELECT status,proof_file_url FROM payments WHERE id=$1", invoice.ID).Scan(&status, &path); err != nil || status != "pending" || path != nil {
					t.Fatalf("changed on IO failure: %s %v %v", status, path, err)
				}
			}
		})
	}
}

func TestPaymentDateFiltersRejectNonRFC3339(t *testing.T) {
	h := billingRouter(billingServiceStub{list: func(context.Context, domain.PaymentFilter) (domain.PaymentPage, error) {
		return domain.PaymentPage{}, nil
	}})
	for _, query := range []string{
		"2026-10-18T1:00:00Z",
		"2026-10-18T01:00:00,5Z",
		"2026-10-18T01:00:00%2B24:00",
		"2026-10-18T01:00:00%2B00:60",
		"2026-10-18T01:00:00.1234567891Z",
	} {
		res := householdRequest(h, "GET", "/api/payments?date_from="+query, "", "")
		if res.Code != 400 {
			t.Errorf("accepted %s: %d %s", query, res.Code, res.Body.String())
		}
	}
}

func TestConfirmationRejectsOversizedUnknownLengthEpilogue(t *testing.T) {
	called := false
	h := confirmationRouter(confirmationServiceStub{confirm: func(context.Context, string, []byte) (domain.Payment, error) {
		called = true
		return domain.Payment{}, nil
	}})
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("proof_file", "proof.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("%PDF-1.4\n%%EOF")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body.Write(bytes.Repeat([]byte("x"), domain.MaxProofSize+64*1024))
	req := httptest.NewRequest("PUT", "/api/payments/"+householdID+"/confirm", &body)
	req.ContentLength = -1
	req.Header.Set("Content-Type", writer.FormDataContentType())
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 413 || called {
		t.Fatalf("oversized epilogue: %d called=%v %s", res.Code, called, res.Body.String())
	}
}
