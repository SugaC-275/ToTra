package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UsageRecord struct {
	TenantID               string
	UserID                 string
	ModelConfigID          string
	ConversationID         string // empty string → stored as NULL
	PromptTokens           int
	CompletionTokens       int
	SCUCost                float64
	USDCost                float64
	ResponseMS             int
	PromptBytesOriginal    int // 0 when compression was not applied
	PromptBytesCompressed  int // 0 when compression was not applied
	Tags                   []string // spend tags, empty → stored as '{}'
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
	var convID *string
	if r.ConversationID != "" {
		if _, err := uuid.Parse(r.ConversationID); err == nil {
			convID = &r.ConversationID
		}
		// invalid UUID → store as NULL, no error
	}
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	_, err := u.pool.Exec(ctx, `
		INSERT INTO usage_records
			(tenant_id, user_id, model_config_id, conversation_id,
			 prompt_tokens, completion_tokens, scu_cost, usd_cost, response_ms,
			 prompt_bytes_original, prompt_bytes_compressed, tags)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		r.TenantID, r.UserID, r.ModelConfigID, convID,
		r.PromptTokens, r.CompletionTokens,
		r.SCUCost, r.USDCost, r.ResponseMS,
		r.PromptBytesOriginal, r.PromptBytesCompressed, tags,
	)
	if err != nil {
		return fmt.Errorf("insert usage record: %w", err)
	}
	return nil
}
