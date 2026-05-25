package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func TestInflightStore_Count_ZeroWhenAbsent(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewInflightStore(rdb)
	count, err := store.Count(context.Background(), "gpt-4o")
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestInflightStore_IncrementDecrement(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewInflightStore(rdb)
	ctx := context.Background()

	require.NoError(t, store.Increment(ctx, "gpt-4o"))
	require.NoError(t, store.Increment(ctx, "gpt-4o"))

	count, err := store.Count(ctx, "gpt-4o")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	require.NoError(t, store.Decrement(ctx, "gpt-4o"))
	count, err = store.Count(ctx, "gpt-4o")
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestInflightStore_Decrement_DoesNotGoBelowZero(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewInflightStore(rdb)
	ctx := context.Background()

	// Decrement without prior increment must not produce a negative count.
	require.NoError(t, store.Decrement(ctx, "gpt-4o"))
	count, err := store.Count(ctx, "gpt-4o")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(0))
}

func TestInflightStore_ModelsAreIsolated(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewInflightStore(rdb)
	ctx := context.Background()

	require.NoError(t, store.Increment(ctx, "gpt-4o"))
	require.NoError(t, store.Increment(ctx, "gpt-4o"))

	count, err := store.Count(ctx, "claude-sonnet-4-6")
	require.NoError(t, err)
	assert.Zero(t, count, "different model counter must remain independent")
}

func TestInflightStore_MultipleIncrements(t *testing.T) {
	rdb, _ := newLatencyMiniRedis(t)
	store := storage.NewInflightStore(rdb)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Increment(ctx, "claude-opus-4-7"))
	}
	count, err := store.Count(ctx, "claude-opus-4-7")
	require.NoError(t, err)
	assert.Equal(t, int64(5), count)

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Decrement(ctx, "claude-opus-4-7"))
	}
	count, err = store.Count(ctx, "claude-opus-4-7")
	require.NoError(t, err)
	assert.Zero(t, count)
}
