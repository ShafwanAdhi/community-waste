package postgres

import (
	"context"
	"fmt"

	"community-waste/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Report struct{ pool *pgxpool.Pool }

func NewReport(pool *pgxpool.Pool) *Report { return &Report{pool: pool} }

func (r *Report) WasteSummary(ctx context.Context) ([]domain.WasteSummary, error) {
	rows, err := r.pool.Query(ctx, "SELECT type,status,count(*) FROM pickups GROUP BY type,status ORDER BY type,status")
	if err != nil {
		return nil, fmt.Errorf("query waste summary: %w", err)
	}
	defer rows.Close()
	result := make([]domain.WasteSummary, 0)
	for rows.Next() {
		var row domain.WasteSummary
		if err := rows.Scan(&row.Type, &row.Status, &row.Count); err != nil {
			return nil, fmt.Errorf("read waste summary: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read waste summary rows: %w", err)
	}
	return result, nil
}

func (r *Report) PaymentSummary(ctx context.Context) ([]domain.PaymentStatusSummary, error) {
	// Sum NUMERIC values before converting exact hundredths to the domain's int64.
	rows, err := r.pool.Query(ctx, "SELECT status,count(*),(sum(amount)*100)::bigint FROM payments GROUP BY status")
	if err != nil {
		return nil, fmt.Errorf("query payment summary: %w", err)
	}
	defer rows.Close()
	result := make([]domain.PaymentStatusSummary, 0, 3)
	for rows.Next() {
		var row domain.PaymentStatusSummary
		var cents int64
		if err := rows.Scan(&row.Status, &row.Count, &cents); err != nil {
			return nil, fmt.Errorf("read payment summary: %w", err)
		}
		row.Amount = domain.Money(cents)
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read payment summary rows: %w", err)
	}
	return result, nil
}
