package storage_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func newLatencyMiniRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

func TestLatencyStore_P95_NoData_ReturnsZero(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewLatencyStore(rdb)
	p95, err := store.P95Latency(context.Background(), "gpt-4o")
	require.NoError(t, err)
	assert.Zero(t, p95)
}

func TestLatencyStore_P95_SingleSample(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewLatencyStore(rdb)
	ctx := context.Background()

	require.NoError(t, store.RecordLatency(ctx, "gpt-4o", 200))

	p95, err := store.P95Latency(ctx, "gpt-4o")
	require.NoError(t, err)
	assert.Equal(t, 200.0, p95)
}

func TestLatencyStore_P95_MultipleSamples(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewLatencyStore(rdb)
	ctx := context.Background()

	// Record 20 latencies: 10, 20, ..., 200 ms
	for i := 1; i <= 20; i++ {
		require.NoError(t, store.RecordLatency(ctx, "gpt-4o", int64(i*10)))
	}

	p95, err := store.P95Latency(ctx, "gpt-4o")
	require.NoError(t, err)
	// P95 of 20 samples: index 18 (0-based) → value 190
	assert.Equal(t, 190.0, p95)
}

func TestLatencyStore_ModelsAreIsolated(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewLatencyStore(rdb)
	ctx := context.Background()

	require.NoError(t, store.RecordLatency(ctx, "gpt-4o", 500))

	p95, err := store.P95Latency(ctx, "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Zero(t, p95, "different model should have no data")
}

func TestLatencyStore_RecordLatency_PrunesOldEntries(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewLatencyStore(rdb)
	ctx := context.Background()

	// Inject a stale entry directly: score = unix_ms from 10 minutes ago.
	staleMs := time.Now().Add(-10 * time.Minute).UnixMilli()
	member := strconv.FormatInt(staleMs, 10) + ":100"
	err := rdb.ZAdd(ctx, "gw:latency:gpt-4o", redis.Z{Score: float64(staleMs), Member: member}).Err()
	require.NoError(t, err)

	// RecordLatency should prune the stale entry atomically.
	require.NoError(t, store.RecordLatency(ctx, "gpt-4o", 300))

	// Only the fresh 300ms sample survives; P95 must equal 300.
	p95, err := store.P95Latency(ctx, "gpt-4o")
	require.NoError(t, err)
	assert.Equal(t, 300.0, p95)
}
