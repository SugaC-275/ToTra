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

func newRateLimitMiniRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return rdb, mr
}

func TestRateLimitStore_GetConfig_Defaults(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil) // nil pool → defaults

	maxPerMin, maxPerUser, err := store.GetConfig(context.Background(), "tenant-x")
	require.NoError(t, err)
	assert.Equal(t, 60, maxPerMin)
	assert.Equal(t, 20, maxPerUser)
}

func TestRateLimitStore_GetConfig_Cached(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	// First call populates Redis cache with defaults.
	m1, u1, err := store.GetConfig(ctx, "tenant-cache")
	require.NoError(t, err)

	// Second call must hit cache (nil pool would panic on DB access if bypassed).
	m2, u2, err := store.GetConfig(ctx, "tenant-cache")
	require.NoError(t, err)
	assert.Equal(t, m1, m2)
	assert.Equal(t, u1, u2)
}

func TestRateLimitStore_CheckAndIncrement_AllowedUnderLimit(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	allowed, remaining, retryAfter, err := store.CheckAndIncrement(ctx, "t1", "u1", 5)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, 4, remaining) // 5 limit − 1 request already recorded
	assert.Equal(t, 0, retryAfter)
}

func TestRateLimitStore_CheckAndIncrement_ExactlyAtLimit(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	// Fill up to the limit (3 requests, limit=3).
	for i := 0; i < 3; i++ {
		allowed, _, _, err := store.CheckAndIncrement(ctx, "t1", "u2", 3)
		require.NoError(t, err)
		assert.True(t, allowed, "request %d should be allowed", i+1)
	}

	// The 4th request must be denied.
	allowed, remaining, retryAfter, err := store.CheckAndIncrement(ctx, "t1", "u2", 3)
	require.NoError(t, err)
	assert.False(t, allowed)
	assert.Equal(t, 0, remaining)
	assert.Greater(t, retryAfter, 0)
}

func TestRateLimitStore_CheckAndIncrement_TenantIsolation(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	// Fill tenant-A's budget.
	for i := 0; i < 2; i++ {
		_, _, _, err := store.CheckAndIncrement(ctx, "tenantA", "u1", 2)
		require.NoError(t, err)
	}

	// tenant-B must be unaffected.
	allowed, _, _, err := store.CheckAndIncrement(ctx, "tenantB", "u1", 2)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRateLimitStore_CheckAndIncrement_UserIsolation(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	// Fill user-A's budget.
	for i := 0; i < 2; i++ {
		_, _, _, err := store.CheckAndIncrement(ctx, "t1", "userA", 2)
		require.NoError(t, err)
	}
	denied, _, _, _ := store.CheckAndIncrement(ctx, "t1", "userA", 2)
	assert.False(t, denied)

	// user-B within the same tenant must be unaffected.
	allowed, _, _, err := store.CheckAndIncrement(ctx, "t1", "userB", 2)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRateLimitStore_CheckAndIncrement_WindowExpiry(t *testing.T) {
	rdb, mr := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	// Fill the window.
	for i := 0; i < 2; i++ {
		allowed, _, _, err := store.CheckAndIncrement(ctx, "t1", "u-exp", 2)
		require.NoError(t, err)
		assert.True(t, allowed)
	}
	denied, _, _, _ := store.CheckAndIncrement(ctx, "t1", "u-exp", 2)
	assert.False(t, denied)

	// Advance miniredis clock by 61 seconds so all entries fall outside the window.
	mr.FastForward(61 * time.Second)

	// Window should be clear; request must be allowed again.
	allowed, _, _, err := store.CheckAndIncrement(ctx, "t1", "u-exp", 2)
	require.NoError(t, err)
	assert.True(t, allowed)
}

func TestRateLimitStore_CheckAndIncrement_RemainingDecreases(t *testing.T) {
	rdb, _ := newRateLimitMiniRedis(t)
	store := storage.NewRateLimitStore(rdb, nil)
	ctx := context.Background()

	_, r0, _, _ := store.CheckAndIncrement(ctx, "t1", "u-rem", 5)
	_, r1, _, _ := store.CheckAndIncrement(ctx, "t1", "u-rem", 5)
	assert.Greater(t, r0, r1, "remaining should decrease with each request")
}
