package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// semCacheTTLDefault is the default semantic cache TTL.
// Override with SEMANTIC_CACHE_TTL_HOURS env var.
const semCacheTTLDefault = 7 * 24 * time.Hour

var semCacheTTL = func() time.Duration {
	if h, err := strconv.Atoi(os.Getenv("SEMANTIC_CACHE_TTL_HOURS")); err == nil && h > 0 {
		return time.Duration(h) * time.Hour
	}
	return semCacheTTLDefault
}()

const (
	// semCacheThreshold is the max SimHash Hamming distance for a cache hit.
	// A one-word paraphrase of a short query (~8 tokens) measures a distance
	// of ~6, so 8 catches single-word rewrites with margin while still
	// rejecting unrelated queries (which measure ~25+).
	semCacheThreshold = 8
	semCacheBands     = 4 // number of 16-bit bands for LSH indexing
)

// SemanticCache stores LLM responses keyed by SimHash with approximate nearest-neighbour
// lookup via 4-band LSH. Two requests are considered equivalent when their token-set
// SimHash Hamming distance is ≤ semCacheThreshold.
type SemanticCache struct{ rdb *redis.Client }

func NewSemanticCache(rdb *redis.Client) *SemanticCache { return &SemanticCache{rdb: rdb} }

type semEntry struct {
	Hash     uint64 `json:"h"`
	Response []byte `json:"r"`
}

// Get returns a cached response if a semantically similar request exists.
func (c *SemanticCache) Get(ctx context.Context, tenantID, text string) ([]byte, bool) {
	h := SimHash64(text)
	candidates := c.candidates(ctx, tenantID, h)

	for _, raw := range candidates {
		var e semEntry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			continue
		}
		if HammingDistance(h, e.Hash) <= semCacheThreshold {
			return e.Response, true
		}
	}
	return nil, false
}

// Set stores a response under the SimHash of text and indexes it in LSH bands.
func (c *SemanticCache) Set(ctx context.Context, tenantID, text string, response []byte) {
	h := SimHash64(text)
	if h == 0 {
		return
	}
	entry, err := json.Marshal(semEntry{Hash: h, Response: response})
	if err != nil {
		return
	}
	hashHex := fmt.Sprintf("%016x", h)
	respKey := c.respKey(tenantID, hashHex)
	c.rdb.Set(ctx, respKey, entry, semCacheTTL)

	for band := 0; band < semCacheBands; band++ {
		bandVal := (h >> uint(band*16)) & 0xFFFF
		bandKey := c.bandKey(tenantID, band, bandVal)
		c.rdb.SAdd(ctx, bandKey, hashHex)
		c.rdb.Expire(ctx, bandKey, semCacheTTL)
	}
}

// IncrHit increments the semantic cache hit counter for a tenant/month.
func (c *SemanticCache) IncrHit(ctx context.Context, tenantID, yearMonth string) {
	c.rdb.Incr(ctx, fmt.Sprintf("sem_cache_hits:%s:%s", tenantID, yearMonth))
}

func (c *SemanticCache) candidates(ctx context.Context, tenantID string, h uint64) []string {
	seen := map[string]struct{}{}
	var out []string
	for band := 0; band < semCacheBands; band++ {
		bandVal := (h >> uint(band*16)) & 0xFFFF
		bandKey := c.bandKey(tenantID, band, bandVal)
		members, err := c.rdb.SMembers(ctx, bandKey).Result()
		if err != nil {
			continue
		}
		for _, hashHex := range members {
			if _, exists := seen[hashHex]; exists {
				continue
			}
			seen[hashHex] = struct{}{}
			raw, err := c.rdb.Get(ctx, c.respKey(tenantID, hashHex)).Result()
			if err == nil {
				out = append(out, raw)
			}
		}
	}
	return out
}

func (c *SemanticCache) respKey(tenantID, hashHex string) string {
	return fmt.Sprintf("sc:r:%s:%s", tenantID, hashHex)
}

func (c *SemanticCache) bandKey(tenantID string, band int, bandVal uint64) string {
	return fmt.Sprintf("sc:b:%s:%d:%04x", tenantID, band, bandVal)
}
