package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// TopSavedModel is a per-model savings breakdown included in CacheStats.
type TopSavedModel struct {
	Model      string  `json:"model"`
	Hits       int64   `json:"hits"`
	SavingsUSD float64 `json:"savings_usd"`
}

// CacheStats is the response body for GET /v1/cache/stats.
type CacheStats struct {
	TenantID            string          `json:"tenant_id"`
	Period              string          `json:"period"`
	ExactHits           int64           `json:"exact_hits"`
	SemanticHits        int64           `json:"semantic_hits"`
	TotalHits           int64           `json:"total_hits"`
	EstimatedSavingsUSD float64         `json:"estimated_savings_usd"`
	AvgLatencySavedMS   int             `json:"avg_latency_saved_ms"`
	TopSavedModels      []TopSavedModel `json:"top_saved_models"`
}

// setCacheConfigPayload is the request body for PUT /v1/cache/config.
type setCacheConfigPayload struct {
	ExactTTLSeconds    int  `json:"exact_ttl_seconds"`
	SemanticTTLSeconds int  `json:"semantic_ttl_seconds"`
	SemanticEnabled    bool `json:"semantic_enabled"`
}

// RegisterCacheStatsRoutes mounts cache management endpoints on router.
// Routes:
//
//	GET    /v1/cache/stats   — aggregate hit + savings stats for the caller's tenant
//	DELETE /v1/cache/clear   — flush all cache entries for the caller's tenant
//	GET    /v1/cache/config  — retrieve per-tenant TTL config
//	PUT    /v1/cache/config  — set per-tenant TTL config
func RegisterCacheStatsRoutes(
	router fiber.Router,
	pool *pgxpool.Pool,
	reqCache *storage.RequestCache,
	semCache *storage.SemanticCache,
	cfgStore *storage.CacheConfigStore,
) {
	router.Get("/cache/stats", getCacheStats(pool, reqCache, semCache))
	router.Delete("/cache/clear", clearTenantCache(reqCache, semCache))
	router.Get("/cache/config", getCacheConfig(cfgStore))
	router.Put("/cache/config", setCacheConfig(cfgStore))
}

// ── handlers ─────────────────────────────────────────────────────────────────

func getCacheStats(pool *pgxpool.Pool, reqCache *storage.RequestCache, semCache *storage.SemanticCache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}

		period := c.Query("period", "30d")
		months := periodToMonths(period)

		var exactHits, semHits int64
		now := time.Now().UTC()
		for i := 0; i < months; i++ {
			ym := now.AddDate(0, -i, 0).Format("2006-01")
			exactHits += reqCache.GetHitCount(c.Context(), user.TenantID, ym)
			semHits += semCache.GetHitCount(c.Context(), user.TenantID, ym)
		}

		savingsUSD, topModels, err := querySavings(c.Context(), pool, user.TenantID, period)
		if err != nil {
			savingsUSD = 0
			topModels = []TopSavedModel{}
		}

		stats := CacheStats{
			TenantID:            user.TenantID,
			Period:              period,
			ExactHits:           exactHits,
			SemanticHits:        semHits,
			TotalHits:           exactHits + semHits,
			EstimatedSavingsUSD: savingsUSD,
			AvgLatencySavedMS:   estimateAvgLatencySavedMS(exactHits+semHits),
			TopSavedModels:      topModels,
		}
		return c.JSON(stats)
	}
}

func clearTenantCache(reqCache *storage.RequestCache, semCache *storage.SemanticCache) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}

		if err := semCache.FlushTenant(c.Context(), user.TenantID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("flush semantic cache: %v", err)})
		}
		if err := reqCache.FlushTenant(c.Context(), user.TenantID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fmt.Sprintf("flush exact cache: %v", err)})
		}
		return c.JSON(fiber.Map{"status": "ok", "tenant_id": user.TenantID})
	}
}

func getCacheConfig(cfgStore *storage.CacheConfigStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		cfg, err := cfgStore.Get(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{
			"tenant_id":            cfg.TenantID,
			"exact_ttl_seconds":    cfg.ExactTTL,
			"semantic_ttl_seconds": cfg.SemanticTTL,
			"semantic_enabled":     cfg.SemanticEnabled,
		})
	}
}

func setCacheConfig(cfgStore *storage.CacheConfigStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if user.Role != "admin" {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "admin role required"})
		}
		var p setCacheConfigPayload
		if err := c.BodyParser(&p); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if p.ExactTTLSeconds <= 0 {
			p.ExactTTLSeconds = 3600
		}
		if p.SemanticTTLSeconds <= 0 {
			p.SemanticTTLSeconds = 7200
		}
		if err := cfgStore.Set(c.Context(), user.TenantID, p.ExactTTLSeconds, p.SemanticTTLSeconds, p.SemanticEnabled); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// periodToMonths maps a period string to the number of calendar months to sum.
func periodToMonths(period string) int {
	switch period {
	case "7d":
		return 1
	case "90d":
		return 3
	default: // "30d"
		return 1
	}
}

// periodToInterval converts period to a Postgres interval string.
func periodToInterval(period string) string {
	switch period {
	case "7d":
		return "7 days"
	case "90d":
		return "90 days"
	default:
		return "30 days"
	}
}

// querySavings sums usd_saved from gateway_routing_events and groups by model.
// If usd_saved is null (no pricing data), falls back to $0 rather than failing.
func querySavings(ctx context.Context, pool *pgxpool.Pool, tenantID, period string) (float64, []TopSavedModel, error) {
	interval := periodToInterval(period)

	var totalSavings float64
	err := pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(usd_saved), 0)
		   FROM gateway_routing_events
		  WHERE tenant_id = $1
		    AND created_at > NOW() - $2::interval`,
		tenantID, interval,
	).Scan(&totalSavings)
	if err != nil {
		return 0, nil, err
	}

	rows, err := pool.Query(ctx,
		`SELECT COALESCE(routed_model, original_model) AS model,
		        COUNT(*) AS hits,
		        COALESCE(SUM(usd_saved), 0) AS savings
		   FROM gateway_routing_events
		  WHERE tenant_id = $1
		    AND created_at > NOW() - $2::interval
		    AND usd_saved IS NOT NULL
		  GROUP BY 1
		  ORDER BY savings DESC
		  LIMIT 5`,
		tenantID, interval,
	)
	if err != nil {
		return totalSavings, nil, err
	}
	defer rows.Close()

	var top []TopSavedModel
	for rows.Next() {
		var m TopSavedModel
		if err := rows.Scan(&m.Model, &m.Hits, &m.SavingsUSD); err != nil {
			continue
		}
		top = append(top, m)
	}
	if top == nil {
		top = []TopSavedModel{}
	}
	return totalSavings, top, nil
}

// estimateAvgLatencySavedMS returns a rough estimate: cache hits typically
// avoid ~800 ms of upstream latency. Returns 0 when no hits occurred.
func estimateAvgLatencySavedMS(totalHits int64) int {
	if totalHits == 0 {
		return 0
	}
	return 800
}
