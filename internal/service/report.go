package service

import (
	"context"
	"fmt"

	"community-waste/internal/domain"
)

type ReportRepository interface {
	WasteSummary(context.Context) ([]domain.WasteSummary, error)
	PaymentSummary(context.Context) ([]domain.PaymentStatusSummary, error)
}

type Report struct{ repository ReportRepository }

func NewReport(repository ReportRepository) *Report { return &Report{repository: repository} }

func (s *Report) WasteSummary(ctx context.Context) ([]domain.WasteSummary, error) {
	rows, err := s.repository.WasteSummary(ctx)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []domain.WasteSummary{}
	}
	return rows, nil
}

func (s *Report) PaymentSummary(ctx context.Context) (domain.PaymentSummary, error) {
	rows, err := s.repository.PaymentSummary(ctx)
	if err != nil {
		return domain.PaymentSummary{}, err
	}
	result := domain.PaymentSummary{Statuses: []domain.PaymentStatusSummary{{Status: domain.PaymentPending}, {Status: domain.PaymentPaid}, {Status: domain.PaymentFailed}}}
	for _, row := range rows {
		var index int
		switch row.Status {
		case domain.PaymentPending:
			index = 0
		case domain.PaymentPaid:
			index = 1
			result.TotalRevenue = row.Amount
		case domain.PaymentFailed:
			index = 2
		default:
			return domain.PaymentSummary{}, fmt.Errorf("unknown payment summary status %q", row.Status)
		}
		result.Statuses[index] = row
	}
	return result, nil
}
