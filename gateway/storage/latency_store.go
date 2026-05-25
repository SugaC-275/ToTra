package storage

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const latencyKeyPrefix = "gw:latency:"

// LatencyStore tracks per-model request latencies in a Redis Sorted Set so that
// P95 can be computed over a rolling 5-minute window without a full time-series
// database.
//
// Redis key layout:
//
//	gw:latency:{model} – Sorted Set; score = unix_ms; member = "{unix_ms}:{latency_ms}"
type LatencyStore struct {
	rdb    *redis.Client
	window time.Duration
}

func NewLatencyStore(rdb *redis.Client) *LatencyStore {
	return &LatencyStore{
		rdb:    rdb,
		window: 5 * time.Minute,
	}
}

var latencyRecordScript = redis.NewScript(`
	local key      = KEYS[1]
	local score    = tonumber(ARGV[1])
	local member   = ARGV[2]
	local cutoff   = tonumber(ARGV[3])
	redis.call('ZADD', key, score, member)
	redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)
	return 1
`)

// RecordLatency appends a latency sample for model and prunes entries older
// than the 5-minute window atomically.
func (s *LatencyStore) RecordLatency(ctx context.Context, model string, latencyMs int64) error {
	key := latencyKeyPrefix + model
	now := time.Now().UnixMilli()
	member := strconv.FormatInt(now, 10) + ":" + strconv.FormatInt(latencyMs, 10)
	cutoff := now - s.window.Milliseconds()

	if err := latencyRecordScript.Run(ctx, s.rdb, []string{key},
		now, member, cutoff,
	).Err(); err != nil {
		return fmt.Errorf("latency_store: record: %w", err)
	}
	return nil
}

// P95Latency computes the P95 latency in milliseconds from all samples recorded
// in the last 5 minutes. Returns 0 when no data exists.
func (s *LatencyStore) P95Latency(ctx context.Context, model string) (float64, error) {
	key := latencyKeyPrefix + model
	now := time.Now().UnixMilli()
	cutoff := now - s.window.Milliseconds()

	members, err := s.rdb.ZRangeByScore(ctx, key, &redis.ZRangeBy{
		Min: strconv.FormatInt(cutoff, 10),
		Max: strconv.FormatInt(now, 10),
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("latency_store: p95 range: %w", err)
	}
	if len(members) == 0 {
		return 0, nil
	}

	latencies := make([]float64, 0, len(members))
	for _, m := range members {
		parts := strings.SplitN(m, ":", 2)
		if len(parts) != 2 {
			continue
		}
		v, err := strconv.ParseFloat(parts[1], 64)
		if err != nil {
			continue
		}
		latencies = append(latencies, v)
	}
	if len(latencies) == 0 {
		return 0, nil
	}

	sort.Float64s(latencies)
	// P95: index at 95th percentile, ceiling to avoid under-reporting.
	idx := int(float64(len(latencies)-1) * 0.95)
	return latencies[idx], nil
}
