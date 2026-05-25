package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/crypto"
)

type ModelConfig struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Provider        string   `json:"provider"`
	BaseURL         string   `json:"base_url"`
	SCURate         float64  `json:"scu_rate"`
	IsActive        bool     `json:"is_active"`
	CacheDisabled   bool     `json:"cache_disabled"`
	PricePerMInput  *float64 `json:"price_per_m_input"`
	PricePerMOutput *float64 `json:"price_per_m_output"`
}

type CreateModelRequest struct {
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	BaseURL  string  `json:"base_url"`
	APIKey   string  `json:"api_key"`
	SCURate  float64 `json:"scu_rate"`
}

type ModelService struct {
	pool          *pgxpool.Pool
	encryptionKey string
}

func NewModelService(pool *pgxpool.Pool, encryptionKey string) *ModelService {
	return &ModelService{pool: pool, encryptionKey: encryptionKey}
}

func (s *ModelService) List(ctx context.Context, tenantID string) ([]*ModelConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, provider, base_url, scu_rate, is_active, cache_disabled, price_per_m_input, price_per_m_output FROM model_configs WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []*ModelConfig
	for rows.Next() {
		m := &ModelConfig{}
		rows.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive, &m.CacheDisabled, &m.PricePerMInput, &m.PricePerMOutput)
		models = append(models, m)
	}
	return models, nil
}

// UpdatePricing sets per-million-token pricing for a model config.
func (s *ModelService) UpdatePricing(ctx context.Context, tenantID, modelID string, inputPrice, outputPrice float64) (*ModelConfig, error) {
	var m ModelConfig
	err := s.pool.QueryRow(ctx,
		`UPDATE model_configs
		 SET price_per_m_input=$1, price_per_m_output=$2
		 WHERE id=$3 AND tenant_id=$4
		 RETURNING id, name, provider, base_url, scu_rate, is_active, cache_disabled, price_per_m_input, price_per_m_output`,
		inputPrice, outputPrice, modelID, tenantID,
	).Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive, &m.CacheDisabled, &m.PricePerMInput, &m.PricePerMOutput)
	if err != nil {
		return nil, fmt.Errorf("update model pricing: %w", err)
	}
	return &m, nil
}

// UpdateCacheSettings sets the cache_disabled flag for a model config.
func (s *ModelService) UpdateCacheSettings(ctx context.Context, tenantID, modelID string, cacheDisabled bool) (*ModelConfig, error) {
	var m ModelConfig
	err := s.pool.QueryRow(ctx,
		`UPDATE model_configs
		 SET cache_disabled=$1
		 WHERE id=$2 AND tenant_id=$3
		 RETURNING id, name, provider, base_url, scu_rate, is_active, cache_disabled, price_per_m_input, price_per_m_output`,
		cacheDisabled, modelID, tenantID,
	).Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive, &m.CacheDisabled, &m.PricePerMInput, &m.PricePerMOutput)
	if err != nil {
		return nil, fmt.Errorf("update model cache settings: %w", err)
	}
	return &m, nil
}

func (s *ModelService) Create(ctx context.Context, tenantID string, req CreateModelRequest) (*ModelConfig, error) {
	encryptedKey, err := crypto.Encrypt(req.APIKey, s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt api key: %w", err)
	}
	var id string
	err = s.pool.QueryRow(ctx,
		`INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		uuid.New().String(), tenantID, req.Name, req.Provider, encryptedKey, req.BaseURL, req.SCURate,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create model config: %w", err)
	}
	return &ModelConfig{ID: id, Name: req.Name, Provider: req.Provider, BaseURL: req.BaseURL, SCURate: req.SCURate, IsActive: true}, nil
}
