package service

import (
	"context"
	"errors"
	"math"
	"testing"

	"community-waste/internal/domain"
)

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

func TestCreateHouseholdTrimsFields(t *testing.T) {
	svc := NewHousehold(householdRepositoryStub{create: func(ctx context.Context, owner, address string) (domain.Household, error) {
		if owner != "Ayu" || address != "Jl. Merdeka 1" {
			t.Fatalf("untrimmed fields: owner=%q address=%q", owner, address)
		}
		return domain.Household{ID: "10000000-0000-4000-8000-000000000001", OwnerName: owner, Address: address}, nil
	}})
	result, err := svc.Create(t.Context(), " \tAyu\u2003", "\nJl. Merdeka 1 \t")
	if err != nil {
		t.Fatal(err)
	}
	if result.ID != "10000000-0000-4000-8000-000000000001" || result.OwnerName != "Ayu" {
		t.Fatalf("unexpected household: %+v", result)
	}
}

func TestCreateHouseholdRejectsInvalidFields(t *testing.T) {
	for _, tc := range []struct{ name, owner, address, field string }{
		{"missing owner", "", "Jl. Merdeka 1", "owner_name"},
		{"whitespace owner", "\t \u2003", "Jl. Merdeka 1", "owner_name"},
		{"missing address", "Ayu", "", "address"},
		{"whitespace address", "Ayu", " \n\t", "address"},
		{"null in owner", "Ayu\x00", "Address", "owner_name"},
		{"null in address", "Ayu", "Address\x00", "address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHousehold(householdRepositoryStub{}).Create(t.Context(), tc.owner, tc.address)
			var validation *domain.ValidationError
			if !errors.As(err, &validation) || validation.Fields[tc.field] == "" {
				t.Fatalf("expected %s field error, got %v", tc.field, err)
			}
		})
	}
}

func TestHouseholdRejectsInvalidIDBeforeRepository(t *testing.T) {
	svc := NewHousehold(householdRepositoryStub{})
	for _, id := range []string{"", "not-a-uuid", "10000000000040008000000000000001", "10000000_0000-4000-8000-000000000001", "10000000-0000-4000-8000-00000000000z", "10000000-0000-4000-8000-0000000000--"} {
		t.Run(id, func(t *testing.T) {
			if _, err := svc.Get(t.Context(), id); !errors.Is(err, domain.ErrInvalidID) {
				t.Fatalf("Get: %v", err)
			}
			if err := svc.Delete(t.Context(), id); !errors.Is(err, domain.ErrInvalidID) {
				t.Fatalf("Delete: %v", err)
			}
		})
	}
}

func TestHouseholdPropagatesRepositoryErrors(t *testing.T) {
	const id = "10000000-0000-4000-8000-000000000001"
	unavailable := errors.New("database unavailable")
	svc := NewHousehold(householdRepositoryStub{
		create: func(context.Context, string, string) (domain.Household, error) {
			return domain.Household{}, unavailable
		},
		list: func(context.Context, domain.Pagination) (domain.HouseholdPage, error) {
			return domain.HouseholdPage{}, unavailable
		},
		get:    func(context.Context, string) (domain.Household, error) { return domain.Household{}, domain.ErrNotFound },
		delete: func(context.Context, string) error { return domain.ErrHouseholdInUse },
	})
	if _, err := svc.Create(t.Context(), "Ayu", "Jl. Merdeka 1"); !errors.Is(err, unavailable) {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.List(t.Context(), domain.Pagination{Page: 1, Limit: 20}); !errors.Is(err, unavailable) {
		t.Fatalf("List: %v", err)
	}
	if _, err := svc.Get(t.Context(), id); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
	if err := svc.Delete(t.Context(), id); !errors.Is(err, domain.ErrHouseholdInUse) {
		t.Fatalf("Delete: %v", err)
	}
}

func TestListHouseholdsRejectsInvalidPagination(t *testing.T) {
	svc := NewHousehold(householdRepositoryStub{})
	for _, page := range []domain.Pagination{
		{Page: 0, Limit: 20}, {Page: -1, Limit: 20}, {Page: 1, Limit: 0},
		{Page: 1, Limit: -1}, {Page: 1, Limit: 101}, {Page: math.MaxInt64, Limit: 100},
	} {
		if _, err := svc.List(t.Context(), page); !errors.Is(err, domain.ErrInvalidPagination) {
			t.Fatalf("pagination %+v: %v", page, err)
		}
	}
}

func TestListHouseholdsAcceptsPageAndMaximumLimit(t *testing.T) {
	svc := NewHousehold(householdRepositoryStub{list: func(ctx context.Context, page domain.Pagination) (domain.HouseholdPage, error) {
		if page.Page != 2 || page.Limit != 100 {
			t.Fatalf("unexpected pagination: %+v", page)
		}
		return domain.HouseholdPage{Items: []domain.Household{}, Total: 3}, nil
	}})
	page, err := svc.List(t.Context(), domain.Pagination{Page: 2, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 0 {
		t.Fatalf("unexpected page: %+v", page)
	}
}
