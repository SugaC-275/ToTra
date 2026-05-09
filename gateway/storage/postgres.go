package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UsageRecord struct {
	TenantID         string
	UserID           string
	ModelConfigID    string
	PromptTokens     int
	CompletionTokens int
	SCUCost          float64
	USDCost          float64
	ResponseMS       int
}

type UsageStore struct {
	pool *pgxpool.Pool
	ch   chan *UsageRecord
}

func NewUsageStore(pool *pgxpool.Pool) *UsageStore {
	us := &UsageStore{
		pool: pool,
		ch:   make(chan *UsageRecord, 1000),
	}
	go us.runWorker()
	return us
}

// Record enqueues a usage record for async write. Never blocks the request path.
func (u *UsageStore) Record(r *UsageRecord) {
	select {
	case u.ch <- r:
	default:
		log.Println("usage record channel full, dropping record")
	}
}

func (u *UsageStore) runWorker() {
	for r := range u.ch {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := u.write(ctx, r); err != nil {
			log.Printf("failed to write usage record: %v", err)
		}
		cancel()
	}
}

func (u *UsageStore) write(ctx context.Context, r *UsageRecord) error {
	_, err := u.pool.Exec(ctx, `
		INSERT INTO usage_records
			(tenant_id, user_id, model_config_id, prompt_tokens, completion_tokens, scu_cost, usd_cost, response_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		r.TenantID, r.UserID, r.ModelConfigID,
		r.PromptTokens, r.CompletionTokens,
		r.SCUCost, r.USDCost, r.ResponseMS,
	)
	if err != nil {
		return fmt.Errorf("insert usage record: %w", err)
	}
	return nil
}
