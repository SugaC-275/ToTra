package storage_test

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	require.NoError(t, rdb.Ping(context.Background()).Err(), "Redis must be running for this test")
	t.Cleanup(func() { rdb.FlushDB(context.Background()) })
	return rdb
}

func TestQuotaStore_CheckAndIncrement(t *testing.T) {
	rdb := newTestRedis(t)
	qs := storage.NewQuotaStore(rdb)
	ctx := context.Background()

	allowed, remaining, err := qs.CheckAndIncrement(ctx, "tenant1", "user1", "2026-05", 1000, 100)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 900, remaining)

	allowed, remaining, err = qs.CheckAndIncrement(ctx, "tenant1", "user1", "2026-05", 1000, 900)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 0, remaining)

	allowed, remaining, err = qs.CheckAndIncrement(ctx, "tenant1", "user1", "2026-05", 1000, 1)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
}
