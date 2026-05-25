package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const inflightKeyPrefix = "gw:inflight:"

// InflightStore tracks the number of in-flight requests per model using a Redis
// string counter. A 5-minute TTL ensures leaked counters expire automatically
// after a crash.
//
// Redis key layout:
//
//	gw:inflight:{model} – String counter
type InflightStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func NewInflightStore(rdb *redis.Client) *InflightStore {
	return &InflightStore{
		rdb: rdb,
		ttl: 5 * time.Minute,
	}
}

var inflightIncrScript = redis.NewScript(`
	local key = KEYS[1]
	local ttl = tonumber(ARGV[1])
	local count = redis.call('INCR', key)
	if count == 1 then
		redis.call('PEXPIRE', key, ttl)
	end
	return count
`)

// Increment atomically increments the in-flight counter for model, setting a
// TTL on first increment to guard against leaked counts after a crash.
func (s *InflightStore) Increment(ctx context.Context, model string) error {
	key := inflightKeyPrefix + model
	ttlMs := s.ttl.Milliseconds()
	if err := inflightIncrScript.Run(ctx, s.rdb, []string{key}, ttlMs).Err(); err != nil {
		return fmt.Errorf("inflight_store: increment: %w", err)
	}
	return nil
}

// decrementScript atomically decrements the counter but clamps it at zero so
// mismatched Increment/Decrement calls can't produce negative values.
var decrementScript = redis.NewScript(`
	local key = KEYS[1]
	local cur = tonumber(redis.call('GET', key))
	if cur == nil or cur <= 0 then
		return 0
	end
	return redis.call('DECR', key)
`)

// Decrement atomically decrements the in-flight counter for model, never below
// zero. It is a no-op when the key is absent.
func (s *InflightStore) Decrement(ctx context.Context, model string) error {
	key := inflightKeyPrefix + model
	if err := decrementScript.Run(ctx, s.rdb, []string{key}).Err(); err != nil {
		return fmt.Errorf("inflight_store: decrement: %w", err)
	}
	return nil
}

// Count returns the current in-flight count for model. Returns 0 when the key
// does not exist.
func (s *InflightStore) Count(ctx context.Context, model string) (int64, error) {
	key := inflightKeyPrefix + model
	val, err := s.rdb.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inflight_store: count: %w", err)
	}
	return val, nil
}
