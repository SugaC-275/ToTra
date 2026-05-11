package storage_test

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func newMiniRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}

func TestAgentStore_IncrLoop_BelowLimit(t *testing.T) {
	rdb := newMiniRedis(t)
	store := storage.NewAgentStore(rdb, nil, 20)

	count, exceeded, err := store.IncrLoop(context.Background(), "conv-123")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
	assert.False(t, exceeded)
}

func TestAgentStore_IncrLoop_ExceedsLimit(t *testing.T) {
	rdb := newMiniRedis(t)
	store := storage.NewAgentStore(rdb, nil, 3)

	for i := 0; i < 3; i++ {
		_, _, _ = store.IncrLoop(context.Background(), "conv-abc")
	}
	count, exceeded, err := store.IncrLoop(context.Background(), "conv-abc")
	require.NoError(t, err)
	assert.Equal(t, int64(4), count)
	assert.True(t, exceeded)
}
