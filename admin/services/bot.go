package services

import (
	"bytes"
	"context"
	"encoding/json"
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

type BotTopEntry struct {
	UserName string
	AIQScore float64
}

type botEntry struct {
	platform   string
	webhookURL string
}

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
			continue
		}
		result = append(result, botEntry{platform, url})
	}
	return result, rows.Err()
}

func (s *BotService) SendKPISummary(ctx context.Context, tenantID, month string) error {
	configs, err := s.loadEnabledConfigs(ctx, tenantID)
	if err != nil {
		return err
	}
	if len(configs) == 0 {
		return nil
	}

	rows, err := s.pool.Query(ctx,
		`SELECT u.name, es.aiq_score
		 FROM efficiency_snapshots es
		 JOIN users u ON u.id = es.user_id
		 WHERE es.tenant_id = $1 AND es.year_month = $2
		   AND es.anomaly_flagged = FALSE
		 ORDER BY es.aiq_score DESC
		 LIMIT 3`,
		tenantID, month,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var topEntries []BotTopEntry
	for rows.Next() {
		var e BotTopEntry
		if err := rows.Scan(&e.UserName, &e.AIQScore); err != nil {
			return err
		}
		topEntries = append(topEntries, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var tenantName string
	s.pool.QueryRow(ctx, `SELECT name FROM tenants WHERE id = $1`, tenantID).Scan(&tenantName)

	message := FormatKPISummaryMessage(tenantName, month, topEntries)

	var sendErr error
	for _, cfg := range configs {
		if err := SendBotMessage(cfg.platform, cfg.webhookURL, message); err != nil {
			sendErr = err
		}
	}
	return sendErr
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

// FormatKPISummaryMessage builds the notification text for KPI summary.
func FormatKPISummaryMessage(tenantName, month string, top []BotTopEntry) string {
	msg := fmt.Sprintf("ToTra KPI Summary — %s (%s)\n", tenantName, month)
	if len(top) == 0 {
		msg += "\nNo KPI snapshots found for this month."
		return msg
	}
	msg += "\nTop performers by AIQ score:\n"
	for i, e := range top {
		msg += fmt.Sprintf("%d. %s — AIQ: %.1f\n", i+1, e.UserName, e.AIQScore)
	}
	return msg
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

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("bot webhook returned status %d", resp.StatusCode)
	}
	return nil
}
