package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	cooldownFailKeyPrefix   = "gw:cooldown:fail:"
	cooldownActiveKeyPrefix = "gw:cooldown:active:"
)

// CooldownStore tracks per-provider failure counts in Redis and activates a
// short-lived cooling period once the failure threshold is reached.
//
// Redis key layout:
//
//	gw:cooldown:fail:{provider}   – INCR counter; TTL = failureWindow
//	gw:cooldown:active:{provider} – presence flag; TTL = cooldownTTL
type CooldownStore struct {
	rdb              *redis.Client
	failureThreshold int
	cooldownTTL      time.Duration
	failureWindow    time.Duration
}

// NewCooldownStore returns a CooldownStore with production defaults:
//
//	failureThreshold = 3
//	cooldownTTL      = 60 s
//	failureWindow    = 2 min
func NewCooldownStore(rdb *redis.Client) *CooldownStore {
	return &CooldownStore{
		rdb:              rdb,
		failureThreshold: 3,
		cooldownTTL:      60 * time.Second,
		failureWindow:    2 * time.Minute,
	}
}

// MarkFailure records one failure for provider. When the consecutive failure
// count reaches failureThreshold the active-cooldown key is set.
func (s *CooldownStore) MarkFailure(ctx context.Context, provider string) error {
	failKey := cooldownFailKeyPrefix + provider
	activeKey := cooldownActiveKeyPrefix + provider

	// Atomically increment failure counter and set its TTL on first write.
	script := redis.NewScript(`
		local count = redis.call('INCR', KEYS[1])
		if count == 1 then
			redis.call('PEXPIRE', KEYS[1], ARGV[1])
		end
		return count
	`)

	windowMs := s.failureWindow.Milliseconds()
	res, err := script.Run(ctx, s.rdb, []string{failKey}, windowMs).Int64()
	if err != nil {
		return fmt.Errorf("cooldown_store: mark failure: %w", err)
	}

	if res >= int64(s.failureThreshold) {
		// Activate cooling period. SETNX so parallel goroutines don't reset TTL.
		if err := s.rdb.Set(ctx, activeKey, "1", s.cooldownTTL).Err(); err != nil {
			return fmt.Errorf("cooldown_store: set active: %w", err)
		}
	}

	return nil
}

// MarkSuccess resets the failure counter for provider so that sporadic errors
// do not accumulate across unrelated request bursts.
func (s *CooldownStore) MarkSuccess(ctx context.Context, provider string) error {
	failKey := cooldownFailKeyPrefix + provider
	if err := s.rdb.Del(ctx, failKey).Err(); err != nil {
		return fmt.Errorf("cooldown_store: mark success: %w", err)
	}
	return nil
}

// IsCooling returns true while the active-cooldown key exists in Redis.
func (s *CooldownStore) IsCooling(ctx context.Context, provider string) (bool, error) {
	activeKey := cooldownActiveKeyPrefix + provider
	exists, err := s.rdb.Exists(ctx, activeKey).Result()
	if err != nil {
		return false, fmt.Errorf("cooldown_store: is cooling: %w", err)
	}
	return exists > 0, nil
}

// CooldownRemaining returns the time left on the active-cooldown key, or zero
// if the provider is not currently cooling.
func (s *CooldownStore) CooldownRemaining(ctx context.Context, provider string) (time.Duration, error) {
	activeKey := cooldownActiveKeyPrefix + provider
	ttl, err := s.rdb.PTTL(ctx, activeKey).Result()
	if err != nil {
		return 0, fmt.Errorf("cooldown_store: ttl: %w", err)
	}
	// PTTL returns -2 when key is absent, -1 when key has no expiry.
	if ttl < 0 {
		return 0, nil
	}
	return ttl, nil
}
