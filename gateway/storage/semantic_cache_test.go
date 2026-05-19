package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func TestSemanticCache_MissOnEmpty(t *testing.T) {
	rdb := newTestRedis(t)
	sc := storage.NewSemanticCache(rdb)
	_, hit := sc.Get(context.Background(), "t1", "how do I sort a list in Go?")
	assert.False(t, hit)
}

func TestSemanticCache_ExactHit(t *testing.T) {
	rdb := newTestRedis(t)
	sc := storage.NewSemanticCache(rdb)
	ctx := context.Background()
	sc.Set(ctx, "t1", "how do I sort a list in Go?", []byte(`{"answer":"use sort.Slice"}`))
	body, hit := sc.Get(ctx, "t1", "how do I sort a list in Go?")
	require.True(t, hit)
	assert.Equal(t, []byte(`{"answer":"use sort.Slice"}`), body)
}

func TestSemanticCache_SimilarTextHit(t *testing.T) {
	rdb := newTestRedis(t)
	sc := storage.NewSemanticCache(rdb)
	ctx := context.Background()
	sc.Set(ctx, "t1", "how do I sort a list in Go?", []byte(`{"answer":"use sort.Slice"}`))
	_, hit := sc.Get(ctx, "t1", "how can I sort a list in Go?")
	assert.True(t, hit, "paraphrased query should hit semantic cache")
}

func TestSemanticCache_UnrelatedTextMiss(t *testing.T) {
	rdb := newTestRedis(t)
	sc := storage.NewSemanticCache(rdb)
	ctx := context.Background()
	sc.Set(ctx, "t1", "how do I sort a list in Go?", []byte(`{"answer":"use sort.Slice"}`))
	_, hit := sc.Get(ctx, "t1", "explain quantum entanglement in simple terms for a five year old")
	assert.False(t, hit, "unrelated query should not hit semantic cache")
}

func TestSemanticCache_TenantIsolation(t *testing.T) {
	rdb := newTestRedis(t)
	sc := storage.NewSemanticCache(rdb)
	ctx := context.Background()
	sc.Set(ctx, "tenant-A", "what is the capital of France?", []byte(`{"answer":"Paris"}`))
	_, hit := sc.Get(ctx, "tenant-B", "what is the capital of France?")
	assert.False(t, hit, "cached entry for tenant-A should not be visible to tenant-B")
}

func TestSemanticCache_EmptyTextNoOp(t *testing.T) {
	rdb := newTestRedis(t)
	sc := storage.NewSemanticCache(rdb)
	ctx := context.Background()
	sc.Set(ctx, "t1", "", []byte(`{}`))
	_, hit := sc.Get(ctx, "t1", "")
	assert.False(t, hit, "empty text SimHash is 0 and should not be stored")
}
