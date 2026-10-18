package postgres

import (
	"errors"
	"testing"

	"community-waste/internal/domain"
	"community-waste/internal/testutil"
)

func TestHouseholdRepositoryCRUD(t *testing.T) {
	repository := NewHousehold(testutil.NewDatabase(t))
	const owner = "Ayu'; DROP TABLE households; --"
	created, err := repository.Create(t.Context(), owner, "Jl. Merdeka 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(created.ID) != 36 || created.OwnerName != owner || created.Address != "Jl. Merdeka 1" || created.CreatedAt.IsZero() || !created.CreatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("unexpected created household: %+v", created)
	}
	got, err := repository.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != created {
		t.Fatalf("household did not round-trip: got %+v, want %+v", got, created)
	}
	if err := repository.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Get(t.Context(), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get after delete: %v", err)
	}
	if err := repository.Delete(t.Context(), created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("repeat Delete: %v", err)
	}
}

func TestHouseholdRepositoryEmptyPage(t *testing.T) {
	repository := NewHousehold(testutil.NewDatabase(t))
	page, err := repository.List(t.Context(), domain.Pagination{Page: 1, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Items == nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("unexpected empty page: %+v", page)
	}
}

func TestHouseholdRepositoryPagination(t *testing.T) {
	pool := testutil.NewDatabase(t)
	_, err := pool.Exec(t.Context(), `INSERT INTO households(id,owner_name,address,created_at) VALUES
		('10000000-0000-4000-8000-000000000001','One','Address','2026-01-01T00:00:00Z'),
		('10000000-0000-4000-8000-000000000002','Two','Address','2026-01-01T00:00:00Z'),
		('10000000-0000-4000-8000-000000000003','Three','Address','2026-01-01T00:00:00Z'),
		('10000000-0000-4000-8000-000000000004','Four','Address','2026-01-02T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	repository := NewHousehold(pool)
	for _, tc := range []struct {
		page int64
		ids  []string
	}{
		{1, []string{"10000000-0000-4000-8000-000000000004", "10000000-0000-4000-8000-000000000003"}},
		{2, []string{"10000000-0000-4000-8000-000000000002", "10000000-0000-4000-8000-000000000001"}},
		{3, []string{}},
	} {
		page, err := repository.List(t.Context(), domain.Pagination{Page: tc.page, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 || page.Items == nil || len(page.Items) != len(tc.ids) {
			t.Fatalf("unexpected page %d: %+v", tc.page, page)
		}
		for i, id := range tc.ids {
			if page.Items[i].ID != id {
				t.Fatalf("page %d position %d: %s", tc.page, i, page.Items[i].ID)
			}
		}
	}
}

func TestHouseholdRepositoryRejectsReferencedDelete(t *testing.T) {
	pool := testutil.NewDatabase(t)
	repository := NewHousehold(pool)
	household, err := repository.Create(t.Context(), "Ayu", "Jl. Merdeka 1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(t.Context(), "INSERT INTO pickups(id,household_id,type) VALUES (gen_random_uuid(),$1,'organic')", household.ID); err != nil {
		t.Fatal(err)
	}
	if err := repository.Delete(t.Context(), household.ID); !errors.Is(err, domain.ErrHouseholdInUse) {
		t.Fatalf("expected restricted delete, got %v", err)
	}
	if _, err := repository.Get(t.Context(), household.ID); err != nil {
		t.Fatalf("referenced household was lost: %v", err)
	}
}
