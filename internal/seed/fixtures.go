package seed

import (
	"fmt"
	"time"

	"community-waste/internal/domain"
)

type householdFixture struct{ id, name, address string }
type pickupFixture struct {
	id, household string
	kind          domain.WasteType
	status        domain.PickupStatus
	date          *time.Time
	safety        *bool
}
type paymentFixture struct {
	id, household, pickup string
	amount                domain.Money
	status                domain.PaymentStatus
}

var createdAt = time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
var snapshotAt = time.Date(2026, 10, 16, 1, 0, 0, 0, time.UTC)

func households() []householdFixture {
	return []householdFixture{
		{householdID(0), "Sari", "Jl. Melati 10"}, {householdID(1), "Bima", "Jl. Kenanga 12"},
		{householdID(2), "Citra", "Jl. Anggrek 7"}, {householdID(3), "Damar", "Jl. Mawar 15"},
	}
}

func householdID(index int) string { return fmt.Sprintf("10000000-0000-4000-8000-%012d", 101+index) }
func pickupID(index int) string    { return fmt.Sprintf("20000000-0000-4000-8000-%012d", 101+index) }

func pickups() []pickupFixture {
	result := make([]pickupFixture, 0, 16)
	for i, kind := range []domain.WasteType{domain.WasteOrganic, domain.WastePlastic, domain.WastePaper, domain.WasteElectronic} {
		for j, status := range []domain.PickupStatus{domain.PickupPending, domain.PickupScheduled, domain.PickupCompleted, domain.PickupCanceled} {
			row := pickupFixture{id: pickupID(i*4 + j), household: householdID(i), kind: kind, status: status}
			if status == domain.PickupScheduled {
				date := time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)
				row.date = &date
			}
			if status == domain.PickupCompleted {
				date := snapshotAt
				row.date = &date
			}
			if kind == domain.WasteElectronic {
				safe := status == domain.PickupScheduled || status == domain.PickupCompleted
				row.safety = &safe
			}
			result = append(result, row)
		}
	}
	return result
}

func payments() []paymentFixture {
	rows := []paymentFixture{
		{household: householdID(0), pickup: pickupID(2), amount: 5000000, status: domain.PaymentPaid},
		{household: householdID(1), pickup: pickupID(6), amount: 5000000, status: domain.PaymentPending},
		{household: householdID(1), pickup: pickupID(7), amount: 5000000, status: domain.PaymentFailed},
		{household: householdID(2), pickup: pickupID(10), amount: 5000000, status: domain.PaymentPaid},
		{household: householdID(3), pickup: pickupID(14), amount: 10000000, status: domain.PaymentPending},
		{household: householdID(3), pickup: pickupID(15), amount: 10000000, status: domain.PaymentFailed},
	}
	for i := range rows {
		rows[i].id = fmt.Sprintf("30000000-0000-4000-8000-%012d", 101+i)
	}
	return rows
}
