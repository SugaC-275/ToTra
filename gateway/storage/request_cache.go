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
