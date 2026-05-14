package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yourorg/totra/gateway/middleware"
)

const policyRuleCacheTTL = 5 * time.Minute

type PolicyRuleStore struct {
	pool *pgxpool.Pool
	rdb  *redis.Client
}

func NewPolicyRuleStore(pool *pgxpool.Pool, rdb *redis.Client) *PolicyRuleStore {
	return &PolicyRuleStore{pool: pool, rdb: rdb}
}

func (s *PolicyRuleStore) GetRules(ctx context.Context, tenantID string) ([]*middleware.PolicyRule, error) {
	cacheKey := fmt.Sprintf("policy:%s", tenantID)
	if cached, err := s.rdb.Get(ctx, cacheKey).Bytes(); err == nil {
		var rules []*middleware.PolicyRule
		if json.Unmarshal(cached, &rules) == nil {
			return rules, nil
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT name, pattern, action
		FROM tenant_policy_rules
		WHERE tenant_id = $1 AND is_active = TRUE
		ORDER BY id ASC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("policy rules query: %w", err)
	}
	defer rows.Close()
	var rules []*middleware.PolicyRule
	for rows.Next() {
		r := &middleware.PolicyRule{}
		if err := rows.Scan(&r.Name, &r.Pattern, &r.Action); err != nil {
			return nil, fmt.Errorf("policy rules scan: %w", err)
		}
		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("policy rules rows: %w", err)
	}
	if data, err := json.Marshal(rules); err == nil {
		s.rdb.Set(ctx, cacheKey, data, policyRuleCacheTTL)
	}
	return rules, nil
}

func (s *PolicyRuleStore) InvalidateCache(ctx context.Context, tenantID string) {
	s.rdb.Del(ctx, fmt.Sprintf("policy:%s", tenantID))
}
