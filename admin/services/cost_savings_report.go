package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type MonthlySavingsReport struct {
	YearMonth          string            `json:"year_month"`
	RoutingEventCount  int64             `json:"routing_event_count"`
	RoutingEventModels []RoutedModelStat `json:"routed_models"`
	GeneratedAt        string            `json:"generated_at"`
}

type RoutedModelStat struct {
	OriginalModel string `json:"original_model"`
	RoutedModel   string `json:"routed_model"`
	Count         int64  `json:"count"`
}

type CostSavingsReportService struct{ pool *pgxpool.Pool }

func NewCostSavingsReportService(pool *pgxpool.Pool) *CostSavingsReportService {
	return &CostSavingsReportService{pool: pool}
}

func (s *CostSavingsReportService) GetMonthlySavings(ctx context.Context, tenantID, yearMonth string) (*MonthlySavingsReport, error) {
	report := &MonthlySavingsReport{YearMonth: yearMonth, GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gateway_routing_events WHERE tenant_id=$1 AND to_char(routed_at AT TIME ZONE 'UTC','YYYY-MM')=$2`,
		tenantID, yearMonth).Scan(&report.RoutingEventCount)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT original_model, routed_model, COUNT(*) AS cnt FROM gateway_routing_events
		 WHERE tenant_id=$1 AND to_char(routed_at AT TIME ZONE 'UTC','YYYY-MM')=$2
		 GROUP BY original_model, routed_model ORDER BY cnt DESC`,
		tenantID, yearMonth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stat RoutedModelStat
		if err := rows.Scan(&stat.OriginalModel, &stat.RoutedModel, &stat.Count); err != nil {
			return nil, err
		}
		report.RoutingEventModels = append(report.RoutingEventModels, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if report.RoutingEventModels == nil {
		report.RoutingEventModels = []RoutedModelStat{}
	}
	return report, nil
}
