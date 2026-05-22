package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

const (
	rateLimitConfigCacheTTL = 60 * time.Second
	rateLimitWindowSeconds  = 60
)

// rateLimitConfig is the cached representation of a row in rate_limit_configs.
type rateLimitConfig struct {
	MaxPerMin        int `json:"max_per_min"`
	MaxPerUserPerMin int `json:"max_per_user_per_min"`
}

// RateLimitStore combines Redis (sliding-window counters) with Postgres
// (per-tenant config) following the same pattern as PolicyRuleStore.
type RateLimitStore struct {
	rdb  *redis.Client
	pool *pgxpool.Pool
}

// NewRateLimitStore constructs a RateLimitStore. pool may be nil in unit tests
// that never call GetConfig.
func NewRateLimitStore(rdb *redis.Client, pool *pgxpool.Pool) *RateLimitStore {
	return &RateLimitStore{rdb: rdb, pool: pool}
}

// GetConfig returns the rate-limit settings for tenantID, using a 60-second
// Redis cache in front of Postgres. If no row exists, safe defaults are returned.
func (s *RateLimitStore) GetConfig(ctx context.Context, tenantID string) (maxPerMin, maxPerUserPerMin int, err error) {
	cacheKey := fmt.Sprintf("rlcfg:%s", tenantID)

	if cached, e := s.rdb.Get(ctx, cacheKey).Bytes(); e == nil {
		var cfg rateLimitConfig
		if json.Unmarshal(cached, &cfg) == nil {
			return cfg.MaxPerMin, cfg.MaxPerUserPerMin, nil
		}
	}

	var cfg rateLimitConfig
	cfg.MaxPerMin = 60
	cfg.MaxPerUserPerMin = 20

	if s.pool != nil {
		e := s.pool.QueryRow(ctx,
			`SELECT max_requests_per_minute, max_requests_per_minute_per_user
			 FROM rate_limit_configs WHERE tenant_id = $1`,
			tenantID,
		).Scan(&cfg.MaxPerMin, &cfg.MaxPerUserPerMin)
		if e != nil && e != pgx.ErrNoRows {
			return 0, 0, fmt.Errorf("rate_limit_store: get config: %w", e)
		}
	}

	if data, e := json.Marshal(cfg); e == nil {
		s.rdb.Set(ctx, cacheKey, data, rateLimitConfigCacheTTL)
	}

	return cfg.MaxPerMin, cfg.MaxPerUserPerMin, nil
}

// CheckAndIncrement implements a Redis sliding-window rate limiter.
//
// Key: "rl:{tenantID}:{userID}"
// Each request is recorded as a sorted-set member with score = Unix timestamp.
// Entries older than 60 s are pruned before counting.
//
// Returns:
//
//	allowed           – false when the window count already equals limit
//	remaining         – slots left after this request (0 when denied)
//	retryAfterSeconds – seconds until the oldest entry expires (0 when allowed)
func (s *RateLimitStore) CheckAndIncrement(
	ctx context.Context,
	tenantID, userID string,
	limit int,
) (allowed bool, remaining, retryAfterSeconds int, err error) {
	key := fmt.Sprintf("rl:%s:%s", tenantID, userID)
	now := time.Now()
	nowUnix := now.UnixNano()
	windowStart := now.Add(-time.Duration(rateLimitWindowSeconds) * time.Second).UnixNano()

	// Unique member: nanosecond timestamp + random suffix to avoid collisions
	// on high-throughput paths.
	member := fmt.Sprintf("%d-%d", nowUnix, rand.Int63())

	pipe := s.rdb.Pipeline()
	// Remove expired entries from the sorted set.
	pipe.ZRemRangeByScore(ctx, key, "0", fmt.Sprintf("%d", windowStart))
	// Count current-window entries before adding this request.
	countCmd := pipe.ZCard(ctx, key)
	// Add the new request.
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(nowUnix), Member: member})
	// Ensure the key expires automatically.
	pipe.Expire(ctx, key, rateLimitWindowSeconds*time.Second)

	if _, e := pipe.Exec(ctx); e != nil {
		return false, 0, 0, fmt.Errorf("rate_limit_store: pipeline: %w", e)
	}

	count := int(countCmd.Val())

	if count >= limit {
		// Remove the member we just added — request is denied.
		s.rdb.ZRem(ctx, key, member)

		// Compute retry-after: time until the oldest entry in the window expires.
		oldest, e := s.rdb.ZRangeWithScores(ctx, key, 0, 0).Result()
		retryAfter := rateLimitWindowSeconds
		if e == nil && len(oldest) > 0 {
			oldestNs := int64(oldest[0].Score)
			expiresAt := time.Unix(0, oldestNs).Add(rateLimitWindowSeconds * time.Second)
			if secs := int(time.Until(expiresAt).Seconds()) + 1; secs > 0 {
				retryAfter = secs
			}
		}
		return false, 0, retryAfter, nil
	}

	return true, limit - count - 1, 0, nil
}
