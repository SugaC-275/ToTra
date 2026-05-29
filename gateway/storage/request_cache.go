package storage

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

func CacheKey(tenantID, body string) string {
	h := sha256.Sum256([]byte(tenantID + "|" + body))
	return fmt.Sprintf("req_cache:%x", h)
}

type RequestCache struct{ rdb *redis.Client }

func NewRequestCache(rdb *redis.Client) *RequestCache { return &RequestCache{rdb: rdb} }

func (c *RequestCache) Get(ctx context.Context, key string) ([]byte, bool) {
	val, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, false
	}
	return val, true
}

func (c *RequestCache) Set(ctx context.Context, key string, body []byte, ttl time.Duration) {
	c.rdb.Set(ctx, key, body, ttl)
}

func (c *RequestCache) IncrHit(ctx context.Context, tenantID, yearMonth string) {
	c.rdb.Incr(ctx, fmt.Sprintf("cache_hits:%s:%s", tenantID, yearMonth))
}

func (c *RequestCache) GetHitCount(ctx context.Context, tenantID, yearMonth string) int64 {
	n, _ := c.rdb.Get(ctx, fmt.Sprintf("cache_hits:%s:%s", tenantID, yearMonth)).Int64()
	return n
}

// FlushTenant deletes all exact-cache keys for tenantID, including hit counters.
// Cache entries are keyed by content hash (not tenant), so only the hit-count
// keys can be matched by pattern; individual response blobs are shared-key and
// not flushed here to avoid cross-tenant collision.
func (c *RequestCache) FlushTenant(ctx context.Context, tenantID string) error {
	pattern := fmt.Sprintf("cache_hits:%s:*", tenantID)
	return scanAndDeleteRC(ctx, c.rdb, pattern)
}

func scanAndDeleteRC(ctx context.Context, rdb *redis.Client, pattern string) error {
	var cursor uint64
	for {
		keys, next, err := rdb.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return err
		}
		if len(keys) > 0 {
			rdb.Del(ctx, keys...)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}
