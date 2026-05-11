package services

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func RetentionCutoff(months int) time.Time {
	return time.Now().UTC().AddDate(0, -months, 0)
}

type DataRetentionServiceIface interface {
	GetRetentionMonths(ctx context.Context, tenantID string) (int, error)
	SetRetentionMonths(ctx context.Context, tenantID string, months int) error
	RunRetentionCleanup(ctx context.Context, tenantID string) (int64, error)
}

type DataRetentionService struct {
	pool *pgxpool.Pool
}

func NewDataRetentionService(pool *pgxpool.Pool) *DataRetentionService {
	return &DataRetentionService{pool: pool}
}

func (s *DataRetentionService) GetRetentionMonths(ctx context.Context, tenantID string) (int, error) {
	var months int
	err := s.pool.QueryRow(ctx,
		`SELECT data_retention_months FROM tenants WHERE id = $1`,
		tenantID,
	).Scan(&months)
	return months, err
}

func (s *DataRetentionService) SetRetentionMonths(ctx context.Context, tenantID string, months int) error {
	if months < 1 {
		months = 1
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE tenants SET data_retention_months = $1 WHERE id = $2`,
		months, tenantID,
	)
	return err
}

func (s *DataRetentionService) RunRetentionCleanup(ctx context.Context, tenantID string) (int64, error) {
	months, err := s.GetRetentionMonths(ctx, tenantID)
	if err != nil {
		return 0, err
	}
	cutoff := RetentionCutoff(months)

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var totalDeleted int64

	// usage_records uses request_at (confirmed from 001_init.sql)
	tag, err := tx.Exec(ctx,
		`DELETE FROM usage_records WHERE tenant_id = $1 AND request_at < $2`,
		tenantID, cutoff,
	)
	if err != nil {
		return 0, err
	}
	totalDeleted += tag.RowsAffected()

	// Also clean output_events if it exists
	var outputEventsExists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'output_events'
		)`,
	).Scan(&outputEventsExists)
	if err != nil {
		return 0, err
	}
	if outputEventsExists {
		tag, err = tx.Exec(ctx,
			`DELETE FROM output_events WHERE tenant_id = $1 AND occurred_at < $2`,
			tenantID, cutoff,
		)
		if err != nil {
			return 0, err
		}
		totalDeleted += tag.RowsAffected()
	}

	return totalDeleted, tx.Commit(ctx)
}
