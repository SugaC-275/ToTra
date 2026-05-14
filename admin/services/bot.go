package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/crypto"
)

type BotConfig struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Platform  string    `json:"platform"`
	Label     string    `json:"label"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
}

type botEntry struct {
	platform   string
	webhookURL string
}

var botHTTPClient = &http.Client{Timeout: 10 * time.Second}

type BotService struct {
	pool   *pgxpool.Pool
	encKey string
}

func NewBotService(pool *pgxpool.Pool, encKey string) *BotService {
	return &BotService{pool: pool, encKey: encKey}
}

func (s *BotService) List(ctx context.Context, tenantID string) ([]BotConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, platform, label, enabled, created_at
		 FROM tenant_bot_configs WHERE tenant_id = $1 ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []BotConfig
	for rows.Next() {
		var c BotConfig
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Platform, &c.Label, &c.Enabled, &c.CreatedAt); err != nil {
			return nil, err
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

func (s *BotService) Add(ctx context.Context, tenantID, platform, webhookURL, label string) (*BotConfig, error) {
	encURL, err := crypto.Encrypt(webhookURL, s.encKey)
	if err != nil {
		return nil, err
	}
	var c BotConfig
	err = s.pool.QueryRow(ctx,
		`INSERT INTO tenant_bot_configs (tenant_id, platform, encrypted_url, label)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, tenant_id, platform, label, enabled, created_at`,
		tenantID, platform, encURL, label,
	).Scan(&c.ID, &c.TenantID, &c.Platform, &c.Label, &c.Enabled, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *BotService) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM tenant_bot_configs WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("bot config not found")
	}
	return nil
}

func (s *BotService) loadEnabledConfigs(ctx context.Context, tenantID string) ([]botEntry, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT platform, encrypted_url FROM tenant_bot_configs
		 WHERE tenant_id = $1 AND enabled = TRUE`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []botEntry
	for rows.Next() {
		var platform, encURL string
		if err := rows.Scan(&platform, &encURL); err != nil {
			return nil, err
		}
		url, err := crypto.Decrypt(encURL, s.encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypt bot config %s: %w", platform, err)
		}
		result = append(result, botEntry{platform, url})
	}
	return result, rows.Err()
}

func (s *BotService) SendTestMessage(ctx context.Context, tenantID, id string) error {
	var platform, encURL string
	err := s.pool.QueryRow(ctx,
		`SELECT platform, encrypted_url FROM tenant_bot_configs WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	).Scan(&platform, &encURL)
	if err != nil {
		return fmt.Errorf("bot config not found")
	}
	url, err := crypto.Decrypt(encURL, s.encKey)
	if err != nil {
		return err
	}
	return SendBotMessage(platform, url, "ToTra bot notification test — connection successful!")
}

// BroadcastAlert sends a message to all enabled bot configs for the given tenant.
func (s *BotService) BroadcastAlert(ctx context.Context, tenantID, message string) error {
	configs, err := s.loadEnabledConfigs(ctx, tenantID)
	if err != nil {
		return err
	}
	var errs []error
	for _, cfg := range configs {
		if err := SendBotMessage(cfg.platform, cfg.webhookURL, message); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SendBotMessage posts a text message to a Feishu or Slack webhook URL.
func SendBotMessage(platform, webhookURL, message string) error {
	var body interface{}
	switch platform {
	case "feishu":
		body = map[string]interface{}{
			"msg_type": "text",
			"content":  map[string]string{"text": message},
		}
	case "slack":
		body = map[string]string{"text": message}
	default:
		return fmt.Errorf("unsupported bot platform: %s", platform)
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	resp, err := botHTTPClient.Post(webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("bot webhook returned status %d", resp.StatusCode)
	}
	return nil
}
