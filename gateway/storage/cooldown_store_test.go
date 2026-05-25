package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

// newCooldownMiniRedis starts an in-process Redis server and returns a client
// connected to it. The server is automatically stopped when t completes.
func newCooldownMiniRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

// TestCooldownStore_MarkFailure_TriggersAfterThreshold verifies that three
// consecutive MarkFailure calls activate the cooling period.
func TestCooldownStore_MarkFailure_TriggersAfterThreshold(t *testing.T) {
	rdb, _ := newCooldownMiniRedis(t)
	store := storage.NewCooldownStore(rdb)
	ctx := context.Background()

	// First two failures must not trigger cooling.
	require.NoError(t, store.MarkFailure(ctx, "openai"))
	cooling, err := store.IsCooling(ctx, "openai")
	require.NoError(t, err)
	assert.False(t, cooling, "should not be cooling after 1 failure")

	require.NoError(t, store.MarkFailure(ctx, "openai"))
	cooling, err = store.IsCooling(ctx, "openai")
	require.NoError(t, err)
	assert.False(t, cooling, "should not be cooling after 2 failures")

	// Third failure crosses the threshold.
	require.NoError(t, store.MarkFailure(ctx, "openai"))
	cooling, err = store.IsCooling(ctx, "openai")
	require.NoError(t, err)
	assert.True(t, cooling, "should be cooling after 3 failures")
}

// TestCooldownStore_MarkSuccess_ResetsCount verifies that a successful request
// clears the failure counter so subsequent failures start from zero.
func TestCooldownStore_MarkSuccess_ResetsCount(t *testing.T) {
	rdb, _ := newCooldownMiniRedis(t)
	store := storage.NewCooldownStore(rdb)
	ctx := context.Background()

	// Build up two failures then succeed.
	require.NoError(t, store.MarkFailure(ctx, "anthropic"))
	require.NoError(t, store.MarkFailure(ctx, "anthropic"))
	require.NoError(t, store.MarkSuccess(ctx, "anthropic"))

	// Two more failures after reset must not trigger cooling (counter restarted).
	require.NoError(t, store.MarkFailure(ctx, "anthropic"))
	require.NoError(t, store.MarkFailure(ctx, "anthropic"))
	cooling, err := store.IsCooling(ctx, "anthropic")
	require.NoError(t, err)
	assert.False(t, cooling, "counter was reset; 2 failures should not trigger cooling")
}

// TestCooldownStore_IsCooling_FalseWhenNotCooling verifies that IsCooling
// returns false for a provider that has never failed.
func TestCooldownStore_IsCooling_FalseWhenNotCooling(t *testing.T) {
	rdb, _ := newCooldownMiniRedis(t)
	store := storage.NewCooldownStore(rdb)
	ctx := context.Background()

	cooling, err := store.IsCooling(ctx, "bedrock")
	require.NoError(t, err)
	assert.False(t, cooling)
}

// TestCooldownStore_CooldownRemaining_PositiveWhileCooling verifies that
// CooldownRemaining returns a positive duration during the cooldown window.
func TestCooldownStore_CooldownRemaining_PositiveWhileCooling(t *testing.T) {
	rdb, _ := newCooldownMiniRedis(t)
	store := storage.NewCooldownStore(rdb)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, store.MarkFailure(ctx, "openai"))
	}

	remaining, err := store.CooldownRemaining(ctx, "openai")
	require.NoError(t, err)
	assert.Positive(t, remaining, "cooldown remaining should be > 0 while cooling")
	assert.LessOrEqual(t, remaining, 60*time.Second, "remaining should not exceed the cooldown TTL")
}

// TestCooldownStore_CooldownRemaining_ZeroWhenNotCooling verifies that
// CooldownRemaining returns zero for a provider that is not in cooldown.
func TestCooldownStore_CooldownRemaining_ZeroWhenNotCooling(t *testing.T) {
	rdb, _ := newCooldownMiniRedis(t)
	store := storage.NewCooldownStore(rdb)
	ctx := context.Background()

	remaining, err := store.CooldownRemaining(ctx, "anthropic")
	require.NoError(t, err)
	assert.Zero(t, remaining)
}

// TestCooldownStore_ProvidersAreIsolated verifies that failures for one
// provider do not affect another provider's state.
func TestCooldownStore_ProvidersAreIsolated(t *testing.T) {
	rdb, _ := newCooldownMiniRedis(t)
	store := storage.NewCooldownStore(rdb)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, store.MarkFailure(ctx, "openai"))
	}

	// "anthropic" should be completely unaffected.
	cooling, err := store.IsCooling(ctx, "anthropic")
	require.NoError(t, err)
	assert.False(t, cooling, "anthropic should not be cooling due to openai failures")
}
