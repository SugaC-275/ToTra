package storage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/storage"
)

func TestCacheKey_Deterministic(t *testing.T) {
	k1 := storage.CacheKey("tenant-a", `{"model":"gpt-4o","messages":[]}`)
	k2 := storage.CacheKey("tenant-a", `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, k1, k2)
}

func TestCacheKey_DifferentTenantsDifferentKeys(t *testing.T) {
	k1 := storage.CacheKey("tenant-a", `{"model":"gpt-4o"}`)
	k2 := storage.CacheKey("tenant-b", `{"model":"gpt-4o"}`)
	assert.NotEqual(t, k1, k2)
}

func TestCacheKey_DifferentBodyDifferentKeys(t *testing.T) {
	k1 := storage.CacheKey("t", `{"model":"gpt-4o","messages":[{"content":"hello"}]}`)
	k2 := storage.CacheKey("t", `{"model":"gpt-4o","messages":[{"content":"world"}]}`)
	assert.NotEqual(t, k1, k2)
}

func TestCacheKey_HasPrefix(t *testing.T) {
	k := storage.CacheKey("t", "body")
	assert.Contains(t, k, "req_cache:")
}
