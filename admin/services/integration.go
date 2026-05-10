package services

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WebhookConfig struct {
	ID           string             `json:"id"`
	TenantID     string             `json:"tenant_id"`
	Platform     string             `json:"platform"`
	EventWeights map[string]float64 `json:"event_weights"`
	IsActive     bool               `json:"is_active"`
	CreatedAt    time.Time          `json:"created_at"`
}

type CreateWebhookConfigRequest struct {
	Platform      string             `json:"platform"`
	WebhookSecret string             `json:"webhook_secret"`
	EventWeights  map[string]float64 `json:"event_weights"`
}

type UserIntegration struct {
	ID         string    `json:"id"`
	Platform   string    `json:"platform"`
	ExternalID string    `json:"external_id"`
	CreatedBy  string    `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
}

type IntegrationService struct {
	pool *pgxpool.Pool
}

func NewIntegrationService(pool *pgxpool.Pool) *IntegrationService {
	return &IntegrationService{pool: pool}
}

// ListWebhookConfigs lists all webhook configs for a tenant (secrets excluded).
func (s *IntegrationService) ListWebhookConfigs(ctx context.Context, tenantID string) ([]*WebhookConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, platform, event_weights, is_active, created_at
		 FROM webhook_configs WHERE tenant_id=$1 ORDER BY platform`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var configs []*WebhookConfig
	for rows.Next() {
		c := &WebhookConfig{}
		var weightsJSON []byte
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Platform, &weightsJSON, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(weightsJSON, &c.EventWeights)
		configs = append(configs, c)
	}
	return configs, nil
}

// CreateWebhookConfig stores a new webhook config with encrypted secret (upsert by tenant+platform).
func (s *IntegrationService) CreateWebhookConfig(ctx context.Context, tenantID, encryptedSecret string, req CreateWebhookConfigRequest) (*WebhookConfig, error) {
	weightsJSON, _ := json.Marshal(req.EventWeights)
	id := uuid.New().String()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO webhook_configs (id, tenant_id, platform, webhook_secret_encrypted, event_weights)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (tenant_id, platform) DO UPDATE
		 SET webhook_secret_encrypted=$4, event_weights=$5, is_active=true`,
		id, tenantID, req.Platform, encryptedSecret, weightsJSON,
	)
	if err != nil {
		return nil, err
	}
	return &WebhookConfig{ID: id, TenantID: tenantID, Platform: req.Platform, IsActive: true}, nil
}

// ListUserIntegrations returns the third-party bindings for a user.
func (s *IntegrationService) ListUserIntegrations(ctx context.Context, userID string) ([]*UserIntegration, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, platform, external_id, created_by, created_at
		 FROM user_integrations WHERE user_id=$1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []*UserIntegration
	for rows.Next() {
		ui := &UserIntegration{}
		if err := rows.Scan(&ui.ID, &ui.Platform, &ui.ExternalID, &ui.CreatedBy, &ui.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, ui)
	}
	return items, nil
}

// BindUserIntegration creates or updates a user's third-party account binding.
func (s *IntegrationService) BindUserIntegration(ctx context.Context, tenantID, userID, platform, externalID, createdBy string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO user_integrations (id, tenant_id, user_id, platform, external_id, created_by)
		 VALUES ($1,$2,$3,$4,$5,$6)
		 ON CONFLICT (tenant_id, platform, external_id) DO UPDATE
		 SET user_id=$3, created_by=$6`,
		uuid.New().String(), tenantID, userID, platform, externalID, createdBy,
	)
	return err
}
