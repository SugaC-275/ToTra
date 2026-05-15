package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/crypto"
)

// SIEMConfig represents a SIEM integration configuration for a tenant.
type SIEMConfig struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	EndpointURL string    `json:"endpoint_url"`
	EventTypes  []string  `json:"event_types"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// SIEMConfigService manages SIEM configurations.
type SIEMConfigService struct {
	pool   *pgxpool.Pool
	encKey string
}

// NewSIEMConfigService creates a new SIEMConfigService.
func NewSIEMConfigService(pool *pgxpool.Pool, encKey string) *SIEMConfigService {
	return &SIEMConfigService{pool: pool, encKey: encKey}
}

// Create stores a new SIEM config, encrypting the API key at rest.
func (s *SIEMConfigService) Create(ctx context.Context, tenantID, name, endpointURL, apiKey string, eventTypes []string) (*SIEMConfig, error) {
	encAPIKey, err := crypto.Encrypt(apiKey, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}
	eventTypesJSON, err := json.Marshal(eventTypes)
	if err != nil {
		return nil, fmt.Errorf("marshal event_types: %w", err)
	}
	id := uuid.New().String()
	var createdAt time.Time
	err = s.pool.QueryRow(ctx,
		`INSERT INTO siem_configs
		 (id, tenant_id, name, endpoint_url, api_key_encrypted, event_types, is_active, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, true, NOW())
		 RETURNING id, created_at`,
		id, tenantID, name, endpointURL, encAPIKey, eventTypesJSON,
	).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert siem_config: %w", err)
	}
	return &SIEMConfig{
		ID:          id,
		TenantID:    tenantID,
		Name:        name,
		EndpointURL: endpointURL,
		EventTypes:  eventTypes,
		IsActive:    true,
		CreatedAt:   createdAt,
	}, nil
}

// List returns all SIEM configs for a tenant (never nil).
func (s *SIEMConfigService) List(ctx context.Context, tenantID string) ([]*SIEMConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, endpoint_url, event_types, is_active, created_at
		 FROM siem_configs WHERE tenant_id = $1 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	configs := []*SIEMConfig{}
	for rows.Next() {
		c := &SIEMConfig{}
		var eventTypesJSON []byte
		if err := rows.Scan(&c.ID, &c.TenantID, &c.Name, &c.EndpointURL, &eventTypesJSON, &c.IsActive, &c.CreatedAt); err != nil {
			return nil, err
		}
		if len(eventTypesJSON) > 0 {
			_ = json.Unmarshal(eventTypesJSON, &c.EventTypes)
		}
		configs = append(configs, c)
	}
	return configs, rows.Err()
}

// Delete removes a SIEM config; returns an error if no row was found.
func (s *SIEMConfigService) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM siem_configs WHERE tenant_id = $1 AND id = $2`,
		tenantID, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("siem config not found: %s", id)
	}
	return nil
}

// ---------------------------------------------------------------------------
// DeliveryLogRow
// ---------------------------------------------------------------------------

// DeliveryLogRow is a single row from the SIEM delivery log.
type DeliveryLogRow struct {
	ID        int64     `json:"id"`
	EventType string    `json:"event_type"`
	Status    string    `json:"status"`
	Attempts  int       `json:"attempts"`
	CreatedAt time.Time `json:"created_at"`
}

// ---------------------------------------------------------------------------
// SIEMDeliveryService
// ---------------------------------------------------------------------------

// SIEMDeliveryService handles polling the delivery queue and sending events.
type SIEMDeliveryService struct {
	pool   *pgxpool.Pool
	encKey string
}

// NewSIEMDeliveryService creates a new SIEMDeliveryService.
func NewSIEMDeliveryService(pool *pgxpool.Pool, encKey string) *SIEMDeliveryService {
	return &SIEMDeliveryService{pool: pool, encKey: encKey}
}

// RunWorker polls the delivery queue every 30 seconds until ctx is cancelled.
func (s *SIEMDeliveryService) RunWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processQueue(ctx)
		}
	}
}

// processQueue fetches pending delivery rows and attempts delivery with retry logic.
func (s *SIEMDeliveryService) processQueue(ctx context.Context) {
	rows, err := s.pool.Query(ctx,
		`SELECT dq.id, dq.event_type, dq.payload, dq.attempts,
		        sc.endpoint_url, sc.api_key_encrypted
		 FROM siem_delivery_queue dq
		 JOIN siem_configs sc ON sc.id = dq.siem_config_id AND sc.is_active = true
		 WHERE dq.status = 'pending'
		   AND (dq.next_retry_at IS NULL OR dq.next_retry_at <= NOW())
		 ORDER BY dq.created_at
		 LIMIT 100`,
	)
	if err != nil {
		return
	}
	defer rows.Close()

	type queueRow struct {
		id               int64
		eventType        string
		payload          []byte
		attempts         int
		endpointURL      string
		apiKeyEncrypted  string
	}

	var pending []queueRow
	for rows.Next() {
		var r queueRow
		if err := rows.Scan(&r.id, &r.eventType, &r.payload, &r.attempts, &r.endpointURL, &r.apiKeyEncrypted); err != nil {
			continue
		}
		pending = append(pending, r)
	}
	rows.Close()

	for _, r := range pending {
		apiKey, err := crypto.Decrypt(r.apiKeyEncrypted, s.encKey)
		if err != nil {
			continue
		}

		var payload map[string]any
		if err := json.Unmarshal(r.payload, &payload); err != nil {
			continue
		}

		deliverErr := DeliverToEndpoint(r.endpointURL, apiKey, payload)
		if deliverErr == nil {
			// Success
			s.pool.Exec(ctx,
				`UPDATE siem_delivery_queue
				 SET status='delivered', delivered_at=NOW()
				 WHERE id=$1`,
				r.id,
			)
		} else {
			// Failure — apply exponential backoff, max 3 attempts
			newAttempts := r.attempts + 1
			if newAttempts >= 3 {
				s.pool.Exec(ctx,
					`UPDATE siem_delivery_queue
					 SET status='failed', attempts=$1
					 WHERE id=$2`,
					newAttempts, r.id,
				)
			} else {
				s.pool.Exec(ctx,
					`UPDATE siem_delivery_queue
					 SET attempts=$1, next_retry_at=NOW() + ($2 * interval '1 minute')
					 WHERE id=$3`,
					newAttempts, 1<<newAttempts, r.id,
				)
			}
		}
	}
}

// SendTest fetches a SIEM config and sends a synthetic test event to its endpoint.
func (s *SIEMDeliveryService) SendTest(ctx context.Context, tenantID, configID string) error {
	var endpointURL, apiKeyEncrypted string
	err := s.pool.QueryRow(ctx,
		`SELECT endpoint_url, api_key_encrypted FROM siem_configs
		 WHERE tenant_id=$1 AND id=$2 AND is_active=true`,
		tenantID, configID,
	).Scan(&endpointURL, &apiKeyEncrypted)
	if err != nil {
		return fmt.Errorf("fetch siem config: %w", err)
	}
	apiKey, err := crypto.Decrypt(apiKeyEncrypted, s.encKey)
	if err != nil {
		return fmt.Errorf("decrypt api key: %w", err)
	}
	return DeliverToEndpoint(endpointURL, apiKey, map[string]any{
		"source":     "totra",
		"event_type": "test",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	})
}

// GetDeliveryLog returns recent delivery log rows for a tenant (never nil).
func (s *SIEMDeliveryService) GetDeliveryLog(ctx context.Context, tenantID string, limit int) ([]*DeliveryLogRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT dq.id, dq.event_type, dq.status, dq.attempts, dq.created_at
		 FROM siem_delivery_queue dq
		 JOIN siem_configs sc ON sc.id = dq.siem_config_id
		 WHERE sc.tenant_id = $1
		 ORDER BY dq.created_at DESC
		 LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := []*DeliveryLogRow{}
	for rows.Next() {
		r := &DeliveryLogRow{}
		if err := rows.Scan(&r.ID, &r.EventType, &r.Status, &r.Attempts, &r.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, r)
	}
	return logs, rows.Err()
}

// ---------------------------------------------------------------------------
// DeliverToEndpoint
// ---------------------------------------------------------------------------

var siemHTTPClient = &http.Client{Timeout: 10 * time.Second}

// DeliverToEndpoint POSTs a JSON payload to a SIEM endpoint with Bearer auth.
// Returns an error for non-2xx responses or transport failures.
func DeliverToEndpoint(endpointURL, apiKey string, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, endpointURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := siemHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("siem endpoint returned %d", resp.StatusCode)
	}
	return nil
}
