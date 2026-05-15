package storage

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SIEMConfigRow struct {
	ID              string
	EndpointURL     string
	APIKeyEncrypted string
}

type SIEMGatewayStore struct{ pool *pgxpool.Pool }

func NewSIEMGatewayStore(pool *pgxpool.Pool) *SIEMGatewayStore {
	return &SIEMGatewayStore{pool: pool}
}

func (s *SIEMGatewayStore) GetActiveConfigs(ctx context.Context, tenantID, eventType string) ([]*SIEMConfigRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, endpoint_url, api_key_encrypted
		 FROM siem_configs
		 WHERE tenant_id = $1 AND is_active = TRUE AND $2 = ANY(event_types)`,
		tenantID, eventType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var configs []*SIEMConfigRow
	for rows.Next() {
		var c SIEMConfigRow
		if err := rows.Scan(&c.ID, &c.EndpointURL, &c.APIKeyEncrypted); err != nil {
			return nil, err
		}
		configs = append(configs, &c)
	}
	return configs, rows.Err()
}

func (s *SIEMGatewayStore) EnqueueDelivery(ctx context.Context, tenantID, configID, eventType string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO siem_delivery_queue (tenant_id, siem_config_id, event_type, payload)
		 VALUES ($1, $2, $3, $4)`,
		tenantID, configID, eventType, b)
	return err
}
