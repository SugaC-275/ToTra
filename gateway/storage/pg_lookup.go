package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

type PGUserLookup struct{ pool *pgxpool.Pool }

func NewPGUserLookup(pool *pgxpool.Pool) *PGUserLookup {
	return &PGUserLookup{pool: pool}
}

func (p *PGUserLookup) LookupByKeyHash(hash string) (*middleware.UserInfo, error) {
	var u middleware.UserInfo
	err := p.pool.QueryRow(context.Background(),
		`SELECT id, tenant_id, role FROM users WHERE api_key_hash = $1 AND is_active = true`,
		hash,
	).Scan(&u.UserID, &u.TenantID, &u.Role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	return &u, nil
}

type PGUserQuota struct{ pool *pgxpool.Pool }

func NewPGUserQuota(pool *pgxpool.Pool) *PGUserQuota { return &PGUserQuota{pool: pool} }

func (p *PGUserQuota) GetUserQuota(ctx context.Context, tenantID, userID string) (int, error) {
	var quota int
	err := p.pool.QueryRow(ctx,
		`SELECT quota_scu FROM users WHERE id = $1 AND tenant_id = $2`,
		userID, tenantID,
	).Scan(&quota)
	if err != nil {
		return 0, fmt.Errorf("get user quota: %w", err)
	}
	return quota, nil
}

type ModelConfig struct {
	ID              string
	Provider        string
	APIKey          string
	BaseURL         string
	SCURate         float64
	PricePerMInput  *float64 // nil when not configured
	PricePerMOutput *float64 // nil when not configured
}

type PGModelLookup struct{ pool *pgxpool.Pool }

func NewPGModelLookup(pool *pgxpool.Pool) *PGModelLookup { return &PGModelLookup{pool: pool} }

func (p *PGModelLookup) GetByName(ctx context.Context, tenantID, modelName string) (*ModelConfig, error) {
	var m ModelConfig
	err := p.pool.QueryRow(ctx,
		`SELECT id, provider, COALESCE(api_key_encrypted,''), base_url, scu_rate, price_per_m_input, price_per_m_output
		 FROM model_configs WHERE tenant_id = $1 AND name = $2 AND is_active = true`,
		tenantID, modelName,
	).Scan(&m.ID, &m.Provider, &m.APIKey, &m.BaseURL, &m.SCURate, &m.PricePerMInput, &m.PricePerMOutput)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get model config: %w", err)
	}
	return &m, nil
}
