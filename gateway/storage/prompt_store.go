package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PromptTemplate struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id,omitempty"`
	Name      string    `json:"name"`
	Version   int       `json:"version"`
	Content   string    `json:"content"`
	Variables []string  `json:"variables"`
	Model     string    `json:"model,omitempty"`
	Tags      []string  `json:"tags"`
	IsActive  bool      `json:"is_active"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type PromptStore struct{ pool *pgxpool.Pool }

func NewPromptStore(pool *pgxpool.Pool) *PromptStore {
	return &PromptStore{pool: pool}
}

func (s *PromptStore) Create(ctx context.Context, p *PromptTemplate) error {
	var model *string
	if p.Model != "" {
		model = &p.Model
	}
	var createdBy *string
	if p.CreatedBy != "" {
		createdBy = &p.CreatedBy
	}
	vars := p.Variables
	if vars == nil {
		vars = []string{}
	}
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO prompt_templates (tenant_id, name, version, content, variables, model, tags, is_active, created_by)
		 VALUES ($1, $2,
		   COALESCE((SELECT MAX(version)+1 FROM prompt_templates WHERE tenant_id=$1 AND name=$2), 1),
		   $3, $4, $5, $6, true, $7)
		 RETURNING id, version, created_at`,
		p.TenantID, p.Name, p.Content, vars, model, tags, createdBy,
	).Scan(&p.ID, &p.Version, &p.CreatedAt)
	if err != nil {
		return fmt.Errorf("prompt_store create: %w", err)
	}
	return nil
}

func (s *PromptStore) GetLatest(ctx context.Context, tenantID, name string) (*PromptTemplate, error) {
	var p PromptTemplate
	var model *string
	var createdBy *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, version, content, variables, model, tags, is_active, created_by, created_at
		 FROM prompt_templates WHERE tenant_id=$1 AND name=$2 AND is_active=true
		 ORDER BY version DESC LIMIT 1`,
		tenantID, name,
	).Scan(&p.ID, &p.TenantID, &p.Name, &p.Version, &p.Content, &p.Variables,
		&model, &p.Tags, &p.IsActive, &createdBy, &p.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prompt_store get_latest: %w", err)
	}
	if model != nil {
		p.Model = *model
	}
	if createdBy != nil {
		p.CreatedBy = *createdBy
	}
	return &p, nil
}

func (s *PromptStore) GetVersion(ctx context.Context, tenantID, name string, version int) (*PromptTemplate, error) {
	var p PromptTemplate
	var model *string
	var createdBy *string
	err := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, version, content, variables, model, tags, is_active, created_by, created_at
		 FROM prompt_templates WHERE tenant_id=$1 AND name=$2 AND version=$3`,
		tenantID, name, version,
	).Scan(&p.ID, &p.TenantID, &p.Name, &p.Version, &p.Content, &p.Variables,
		&model, &p.Tags, &p.IsActive, &createdBy, &p.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("prompt_store get_version: %w", err)
	}
	if model != nil {
		p.Model = *model
	}
	if createdBy != nil {
		p.CreatedBy = *createdBy
	}
	return &p, nil
}

func (s *PromptStore) List(ctx context.Context, tenantID string, limit int) ([]*PromptTemplate, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (name) id, tenant_id, name, version, content, variables, COALESCE(model,''), tags, is_active, COALESCE(created_by::text,''), created_at
		 FROM prompt_templates WHERE tenant_id=$1
		 ORDER BY name, version DESC
		 LIMIT $2`,
		tenantID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("prompt_store list: %w", err)
	}
	defer rows.Close()
	var prompts []*PromptTemplate
	for rows.Next() {
		var p PromptTemplate
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Version, &p.Content, &p.Variables,
			&p.Model, &p.Tags, &p.IsActive, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		prompts = append(prompts, &p)
	}
	return prompts, rows.Err()
}

// Render replaces {{variable}} placeholders in content with provided values.
func (p *PromptTemplate) Render(vars map[string]string) string {
	result := p.Content
	for k, v := range vars {
		result = strings.ReplaceAll(result, "{{"+k+"}}", v)
	}
	return result
}
