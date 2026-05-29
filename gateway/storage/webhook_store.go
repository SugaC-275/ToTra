package storage

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WebhookConfig holds a registered outbound webhook endpoint.
type WebhookConfig struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id,omitempty"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Secret      string    `json:"secret,omitempty"`
	Events      []string  `json:"events"`
	WebhookType string    `json:"webhook_type"` // 'webhook' | 'slack' | 'pagerduty'
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// WebhookStore persists webhook configurations in Postgres.
type WebhookStore struct {
	pool *pgxpool.Pool
}

// NewWebhookStore creates a WebhookStore backed by the given connection pool.
func NewWebhookStore(pool *pgxpool.Pool) *WebhookStore {
	return &WebhookStore{pool: pool}
}

// Create inserts a new webhook configuration and returns the persisted record.
// webhookType may be 'webhook' (default), 'slack', or 'pagerduty'.
func (s *WebhookStore) Create(ctx context.Context, tenantID, name, url, secret string, events []string, webhookType ...string) (*WebhookConfig, error) {
	wt := "webhook"
	if len(webhookType) > 0 && webhookType[0] != "" {
		wt = webhookType[0]
	}
	var cfg WebhookConfig
	err := s.pool.QueryRow(ctx, `
		INSERT INTO webhook_configs (tenant_id, name, url, secret, events, webhook_type)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, name, url, secret, events, webhook_type, is_active, created_at
	`, tenantID, name, url, secret, events, wt).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.URL,
		&cfg.Secret, &cfg.Events, &cfg.WebhookType, &cfg.IsActive, &cfg.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// List returns all webhook configurations for the given tenant.
func (s *WebhookStore) List(ctx context.Context, tenantID string) ([]*WebhookConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, url, events, webhook_type, is_active, created_at
		FROM webhook_configs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cfgs []*WebhookConfig
	for rows.Next() {
		var cfg WebhookConfig
		if err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.URL,
			&cfg.Events, &cfg.WebhookType, &cfg.IsActive, &cfg.CreatedAt); err != nil {
			return nil, err
		}
		cfgs = append(cfgs, &cfg)
	}
	return cfgs, rows.Err()
}

// Delete removes a webhook configuration owned by the given tenant.
func (s *WebhookStore) Delete(ctx context.Context, tenantID, id string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM webhook_configs WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	return err
}

// GetForEvent returns active webhooks for a tenant that are subscribed to eventType.
func (s *WebhookStore) GetForEvent(ctx context.Context, tenantID, eventType string) ([]*WebhookConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, url, secret, events, webhook_type, is_active, created_at
		FROM webhook_configs
		WHERE tenant_id = $1 AND $2 = ANY(events) AND is_active = true
	`, tenantID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cfgs []*WebhookConfig
	for rows.Next() {
		var cfg WebhookConfig
		if err := rows.Scan(&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.URL,
			&cfg.Secret, &cfg.Events, &cfg.WebhookType, &cfg.IsActive, &cfg.CreatedAt); err != nil {
			return nil, err
		}
		cfgs = append(cfgs, &cfg)
	}
	return cfgs, rows.Err()
}

// GetByID fetches a single webhook config by tenant and ID (includes secret for signing).
func (s *WebhookStore) GetByID(ctx context.Context, tenantID, id string) (*WebhookConfig, error) {
	var cfg WebhookConfig
	err := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, url, secret, events, webhook_type, is_active, created_at
		FROM webhook_configs
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).Scan(
		&cfg.ID, &cfg.TenantID, &cfg.Name, &cfg.URL,
		&cfg.Secret, &cfg.Events, &cfg.WebhookType, &cfg.IsActive, &cfg.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
