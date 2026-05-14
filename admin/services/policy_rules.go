package services

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func ValidatePattern(pattern string) error {
	_, err := regexp.Compile(pattern)
	return err
}

func ValidateAction(action string) error {
	if action == "block" || action == "log" {
		return nil
	}
	return fmt.Errorf("action must be 'block' or 'log', got %q", action)
}

type PolicyRule struct {
	ID        int64  `json:"id"`
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Pattern   string `json:"pattern"`
	Action    string `json:"action"`
	IsActive  bool   `json:"is_active"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type PolicyRulesService struct{ pool *pgxpool.Pool }

func NewPolicyRulesService(pool *pgxpool.Pool) *PolicyRulesService {
	return &PolicyRulesService{pool: pool}
}

func (s *PolicyRulesService) List(ctx context.Context, tenantID string) ([]*PolicyRule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, pattern, action, is_active, created_at, updated_at
		FROM tenant_policy_rules WHERE tenant_id = $1 ORDER BY id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("policy rules list: %w", err)
	}
	defer rows.Close()
	var rules []*PolicyRule
	for rows.Next() {
		r := &PolicyRule{}
		var ca, ua time.Time
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.Pattern, &r.Action, &r.IsActive, &ca, &ua); err != nil {
			return nil, err
		}
		r.CreatedAt = ca.UTC().Format(time.RFC3339)
		r.UpdatedAt = ua.UTC().Format(time.RFC3339)
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if rules == nil {
		rules = []*PolicyRule{}
	}
	return rules, nil
}

func (s *PolicyRulesService) Create(ctx context.Context, tenantID, name, pattern, action string) (*PolicyRule, error) {
	if err := ValidatePattern(pattern); err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	if err := ValidateAction(action); err != nil {
		return nil, err
	}
	var r PolicyRule
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_policy_rules(tenant_id, name, pattern, action)
		VALUES ($1, $2, $3, $4)
		RETURNING id, tenant_id, name, pattern, action, is_active, created_at, updated_at
	`, tenantID, name, pattern, action).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Pattern, &r.Action, &r.IsActive, &ca, &ua,
	)
	if err != nil {
		return nil, fmt.Errorf("policy rule create: %w", err)
	}
	r.CreatedAt = ca.UTC().Format(time.RFC3339)
	r.UpdatedAt = ua.UTC().Format(time.RFC3339)
	return &r, nil
}

func (s *PolicyRulesService) Update(ctx context.Context, tenantID string, id int64, name, pattern, action string, isActive bool) (*PolicyRule, error) {
	if err := ValidatePattern(pattern); err != nil {
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}
	if err := ValidateAction(action); err != nil {
		return nil, err
	}
	var r PolicyRule
	var ca, ua time.Time
	err := s.pool.QueryRow(ctx, `
		UPDATE tenant_policy_rules
		SET name=$3, pattern=$4, action=$5, is_active=$6, updated_at=NOW()
		WHERE id=$1 AND tenant_id=$2
		RETURNING id, tenant_id, name, pattern, action, is_active, created_at, updated_at
	`, id, tenantID, name, pattern, action, isActive).Scan(
		&r.ID, &r.TenantID, &r.Name, &r.Pattern, &r.Action, &r.IsActive, &ca, &ua,
	)
	if err != nil {
		return nil, fmt.Errorf("policy rule update: %w", err)
	}
	r.CreatedAt = ca.UTC().Format(time.RFC3339)
	r.UpdatedAt = ua.UTC().Format(time.RFC3339)
	return &r, nil
}

func (s *PolicyRulesService) Delete(ctx context.Context, tenantID string, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tenant_policy_rules WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	if err != nil {
		return fmt.Errorf("policy rule delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found")
	}
	return nil
}
