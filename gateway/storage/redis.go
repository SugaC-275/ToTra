package storage

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type QuotaStore struct {
	rdb *redis.Client
}

func NewQuotaStore(rdb *redis.Client) *QuotaStore {
	return &QuotaStore{rdb: rdb}
}

func (q *QuotaStore) CheckAndIncrement(ctx context.Context, tenantID, userID, yearMonth string, quotaLimit, cost int) (bool, int, error) {
	key := fmt.Sprintf("quota:%s:%s:%s", tenantID, userID, yearMonth)

	script := redis.NewScript(`
		local current = tonumber(redis.call('GET', KEYS[1]) or 0)
		local limit = tonumber(ARGV[1])
		local cost = tonumber(ARGV[2])
		if current + cost > limit then
			return {0, limit - current}
		end
		local new = redis.call('INCRBY', KEYS[1], cost)
		redis.call('EXPIRE', KEYS[1], 2678400)
		return {1, limit - new}
	`)

	result, err := script.Run(ctx, q.rdb, []string{key}, quotaLimit, cost).Int64Slice()
	if err != nil {
		return false, 0, fmt.Errorf("quota check: %w", err)
	}

	allowed := result[0] == 1
	remaining := int(result[1])
	if remaining < 0 {
		remaining = 0
	}
	return allowed, remaining, nil
}

func (q *QuotaStore) GetUsage(ctx context.Context, tenantID, userID, yearMonth string) (int, error) {
	key := fmt.Sprintf("quota:%s:%s:%s", tenantID, userID, yearMonth)
	val, err := q.rdb.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}
