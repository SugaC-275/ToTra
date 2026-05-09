package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ModelConfig struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	BaseURL  string  `json:"base_url"`
	SCURate  float64 `json:"scu_rate"`
	IsActive bool    `json:"is_active"`
}

type CreateModelRequest struct {
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	BaseURL  string  `json:"base_url"`
	APIKey   string  `json:"api_key"`
	SCURate  float64 `json:"scu_rate"`
}

type ModelService struct {
	pool *pgxpool.Pool
}

func NewModelService(pool *pgxpool.Pool) *ModelService {
	return &ModelService{pool: pool}
}

func (s *ModelService) List(ctx context.Context, tenantID string) ([]*ModelConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, provider, base_url, scu_rate, is_active FROM model_configs WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []*ModelConfig
	for rows.Next() {
		m := &ModelConfig{}
		rows.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive)
		models = append(models, m)
	}
	return models, nil
}

func (s *ModelService) Create(ctx context.Context, tenantID string, req CreateModelRequest) (*ModelConfig, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		uuid.New().String(), tenantID, req.Name, req.Provider, req.APIKey, req.BaseURL, req.SCURate,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create model config: %w", err)
	}
	return &ModelConfig{ID: id, Name: req.Name, Provider: req.Provider, BaseURL: req.BaseURL, SCURate: req.SCURate, IsActive: true}, nil
}
