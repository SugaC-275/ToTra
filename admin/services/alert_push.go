package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertEvent is the payload passed to Deliver for fan-out to all matching configs.
type AlertEvent struct {
	TenantID  string
	EventType string // "budget_exceeded", "budget_warning", "compliance_violation", "pii_spike"
	Title     string
	Message   string
	Severity  string // "info", "warning", "critical"
	Timestamp time.Time
	Metadata  map[string]any
}

// AlertDeliveryConfig is a single delivery destination for a tenant.
type AlertDeliveryConfig struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Channel     string    `json:"channel"`      // "slack", "email", "webhook"
	Destination string    `json:"destination"`  // URL or email address
	EventTypes  []string  `json:"event_types"`
	Enabled     bool      `json:"enabled"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AlertPushService fans out alert events to configured delivery channels.
type AlertPushService struct {
	pool       *pgxpool.Pool
	httpClient *http.Client
}

// NewAlertPushService creates a new AlertPushService.
func NewAlertPushService(pool *pgxpool.Pool) *AlertPushService {
	return &AlertPushService{
		pool:       pool,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// GetConfigs returns all alert delivery configs for a tenant.
func (s *AlertPushService) GetConfigs(ctx context.Context, tenantID string) ([]AlertDeliveryConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, channel::text, destination,
		       ARRAY(SELECT unnest(event_types)::text), enabled, created_at, updated_at
		FROM alert_delivery_configs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("alert_push: list configs: %w", err)
	}
	defer rows.Close()

	var configs []AlertDeliveryConfig
	for rows.Next() {
		var c AlertDeliveryConfig
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Channel, &c.Destination,
			&c.EventTypes, &c.Enabled, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("alert_push: scan config: %w", err)
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// CreateConfig persists a new alert delivery config.
func (s *AlertPushService) CreateConfig(ctx context.Context, tenantID string, cfg AlertDeliveryConfig) error {
	if tenantID == "" || cfg.Channel == "" || cfg.Destination == "" || len(cfg.EventTypes) == 0 {
		return fmt.Errorf("alert_push: tenant_id, channel, destination, event_types are required")
	}

	// Build typed arrays for Postgres ENUM cast.
	eventTypesSQL := make([]string, len(cfg.EventTypes))
	copy(eventTypesSQL, cfg.EventTypes)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO alert_delivery_configs (tenant_id, channel, destination, event_types, enabled)
		VALUES ($1, $2::alert_channel, $3,
		        ARRAY(SELECT unnest($4::text[])::alert_event_type),
		        $5)
	`, tenantID, cfg.Channel, cfg.Destination, eventTypesSQL, cfg.Enabled)
	if err != nil {
		return fmt.Errorf("alert_push: create config: %w", err)
	}
	return nil
}

// DeleteConfig removes a delivery config by ID, scoped to the tenant.
func (s *AlertPushService) DeleteConfig(ctx context.Context, tenantID, configID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM alert_delivery_configs WHERE id = $1 AND tenant_id = $2
	`, configID, tenantID)
	if err != nil {
		return fmt.Errorf("alert_push: delete config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("alert_push: config %s not found", configID)
	}
	return nil
}

// Deliver fans out the event concurrently to all enabled, matching configs.
func (s *AlertPushService) Deliver(ctx context.Context, event AlertEvent) error {
	configs, err := s.GetConfigs(ctx, event.TenantID)
	if err != nil {
		return fmt.Errorf("alert_push: deliver fetch configs: %w", err)
	}

	var wg sync.WaitGroup
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		if !matchesEventType(cfg.EventTypes, event.EventType) {
			continue
		}

		wg.Add(1)
		go func(c AlertDeliveryConfig) {
			defer wg.Done()
			dctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			var delivErr error
			switch c.Channel {
			case "slack":
				delivErr = s.deliverSlack(dctx, c.Destination, event)
			case "webhook":
				delivErr = s.deliverWebhook(dctx, c.Destination, event)
			case "email":
				delivErr = s.deliverEmail(dctx, c.Destination, event)
			default:
				log.Printf("alert_push: unknown channel %q for config %s", c.Channel, c.ID)
				return
			}
			if delivErr != nil {
				log.Printf("alert_push: delivery failed [%s/%s]: %v", c.Channel, c.ID, delivErr)
			}
		}(cfg)
	}
	wg.Wait()
	return nil
}

// deliverSlack posts a message to a Slack incoming webhook URL.
func (s *AlertPushService) deliverSlack(ctx context.Context, webhookURL string, event AlertEvent) error {
	color := slackColor(event.Severity)
	payload := map[string]any{
		"text": fmt.Sprintf("[%s] %s", event.Severity, event.Title),
		"attachments": []map[string]any{
			{
				"color": color,
				"title": event.Title,
				"text":  event.Message,
				"ts":    event.Timestamp.Unix(),
			},
		},
	}
	return s.postJSON(ctx, webhookURL, payload)
}

// deliverWebhook posts the full event payload to a generic HTTP endpoint.
func (s *AlertPushService) deliverWebhook(ctx context.Context, url string, event AlertEvent) error {
	payload := map[string]any{
		"tenant_id":  event.TenantID,
		"event_type": event.EventType,
		"title":      event.Title,
		"message":    event.Message,
		"severity":   event.Severity,
		"timestamp":  event.Timestamp.UTC().Format(time.RFC3339),
		"metadata":   event.Metadata,
	}
	return s.postJSON(ctx, url, payload)
}

// deliverEmail sends an alert via SMTP using environment-based configuration.
func (s *AlertPushService) deliverEmail(_ context.Context, toAddr string, event AlertEvent) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	if host == "" || port == "" || from == "" {
		return fmt.Errorf("alert_push: SMTP_HOST, SMTP_PORT, SMTP_FROM must be set")
	}

	addr := host + ":" + port
	subject := fmt.Sprintf("[%s] %s", event.Severity, event.Title)
	body := fmt.Sprintf("Subject: %s\r\nFrom: %s\r\nTo: %s\r\nContent-Type: text/plain\r\n\r\n%s\r\n\nTimestamp: %s\nTenant: %s",
		subject, from, toAddr, event.Message, event.Timestamp.UTC().Format(time.RFC3339), event.TenantID)

	var auth smtp.Auth
	if user != "" && pass != "" {
		auth = smtp.PlainAuth("", user, pass, host)
	}

	if err := smtp.SendMail(addr, auth, from, []string{toAddr}, []byte(body)); err != nil {
		return fmt.Errorf("alert_push: smtp send: %w", err)
	}
	return nil
}

// postJSON marshals payload and POSTs it to the given URL.
func (s *AlertPushService) postJSON(ctx context.Context, url string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

// matchesEventType returns true if eventType is in the configured list.
func matchesEventType(configured []string, eventType string) bool {
	for _, et := range configured {
		if et == eventType {
			return true
		}
	}
	return false
}

// slackColor maps severity to a Slack attachment color.
func slackColor(severity string) string {
	switch severity {
	case "critical":
		return "danger"
	case "warning":
		return "warning"
	default:
		return "good"
	}
}
