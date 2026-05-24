package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/yourorg/totra/gateway/config"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
	"github.com/yourorg/totra/gateway/tokenizer"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("redis: %v", err)
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("postgres parse config: %v", err)
	}
	poolCfg.MaxConns = 20
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	quotaStore := storage.NewQuotaStore(rdb)
	usageStore := storage.NewUsageStore(pool)
	agentStore := storage.NewAgentStore(rdb, pool, cfg.AgentLoopLimit)
	pgUserLookup := storage.NewPGUserLookup(pool)
	pgUserQuota := storage.NewPGUserQuota(pool)
	piiStore := storage.NewPIIStore(pool, 256)
	requestCache := storage.NewRequestCache(rdb)
	semanticCache := storage.NewSemanticCache(rdb)
	routingStore := storage.NewRoutingStore(pool)
	policyRuleStore := storage.NewPolicyRuleStore(pool, rdb)
	siemGatewayStore := storage.NewSIEMGatewayStore(pool)
	siemChan := make(chan middleware.SIEMEvent, 1000)
	enqueuer := &siemEnqueuer{ch: siemChan, store: siemGatewayStore}
	go enqueuer.run(context.Background())
	parserClient := storage.NewParserClient(cfg.ParserURL)
	fileLookup := storage.NewPGModelLookup(pool)
	rateLimitStore := storage.NewRateLimitStore(rdb, pool)
	gatewayMetrics := middleware.NewGatewayMetrics(prometheus.DefaultRegisterer)
	modelLookup := storage.NewPGModelLookup(pool)

	app := fiber.New(fiber.Config{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), 3*time.Second)
		defer cancel()
		if err := pool.Ping(ctx); err != nil {
			slog.Error("health: postgres unreachable", "err", err)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "unavailable", "detail": "db unreachable",
			})
		}
		if err := rdb.Ping(ctx).Err(); err != nil {
			slog.Error("health: redis unreachable", "err", err)
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"status": "unavailable", "detail": "redis unreachable",
			})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	})

	metricsToken := os.Getenv("METRICS_TOKEN")
	app.Get("/metrics", func(c *fiber.Ctx) error {
		if metricsToken != "" {
			auth := c.Get("Authorization")
			if auth != "Bearer "+metricsToken {
				return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
			}
		}
		return handlers.MetricsHandler(prometheus.DefaultGatherer)(c)
	})

	v1 := app.Group("/v1",
		middleware.NewMetricsMiddleware(gatewayMetrics),
		middleware.NewAuthMiddleware(pgUserLookup),
		middleware.NewRateLimiterMiddleware(rateLimitStore),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		middleware.NewPIIMiddleware(piiStore, "", siemChan),
		middleware.NewPolicyMiddleware(policyRuleStore, siemChan),
		middleware.NewCompressMiddleware(),
		middleware.NewAutoRouterMiddleware(routingStore),
		middleware.NewAgentMiddleware(agentStore),
	)

	proxyHandler := makeProxyHandler(pool, usageStore, agentStore, requestCache, semanticCache, routingStore)
	fallbackMw := middleware.NewFallbackMiddleware(modelLookup, proxyHandler)
	v1.Post("/chat/completions", fallbackMw, proxyHandler)
	v1.Post("/messages", fallbackMw, proxyHandler)
	v1.Post("/chat/completions/stream", handlers.NewStreamProxyHandler(pool, usageStore))

	app.Post("/v1/files/chat",
		middleware.NewAuthMiddleware(pgUserLookup),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		handlers.NewFileChatHandler(fileLookup, parserClient, piiStore, usageStore),
	)

	log.Printf("Gateway listening on :%s", cfg.Port)
	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit
	log.Println("shutting down gateway...")
	if err := app.ShutdownWithTimeout(15 * time.Second); err != nil {
		log.Printf("shutdown error: %v", err)
	}
	log.Println("gateway stopped")
}

func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore, agentStore *storage.AgentStore, cache *storage.RequestCache, semCache *storage.SemanticCache, routingStore *storage.RoutingStore) fiber.Handler {
	modelLookup := storage.NewPGModelLookup(pool)

	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)
		start := time.Now()
		body := string(c.Body())
		yearMonth := time.Now().UTC().Format("2006-01")

		// Exact cache check.
		cacheKey := storage.CacheKey(user.TenantID, body)
		if cached, ok := cache.Get(c.Context(), cacheKey); ok {
			cache.IncrHit(c.Context(), user.TenantID, yearMonth)
			c.Set("X-Cache", "HIT")
			return c.Status(200).Send(cached)
		}

		// Semantic cache check (similar but not identical requests).
		if cached, ok := semCache.Get(c.Context(), user.TenantID, body); ok {
			semCache.IncrHit(c.Context(), user.TenantID, yearMonth)
			c.Set("X-Cache", "SEMANTIC-HIT")
			return c.Status(200).Send(cached)
		}

		var reqBody struct{ Model string `json:"model"` }
		if err := c.BodyParser(&reqBody); err != nil || reqBody.Model == "" {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "model field required"}})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, reqBody.Model)
		if err != nil || modelCfg == nil {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("model %q not configured for your tenant", reqBody.Model),
			}})
		}

		var fwd providers.Adapter
		fwd, err = providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("unsupported provider %q for model %q", modelCfg.Provider, reqBody.Model),
			}})
		}

		result, usage, err := fwd.Forward(c.Context(), c.Body())
		if err != nil {
			slog.Error("upstream forward error", "tenant", user.TenantID, "model", reqBody.Model, "err", err)
			return c.Status(502).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		if result.StatusCode == 200 {
			cache.Set(c.Context(), cacheKey, result.Body, 24*time.Hour)
			semCache.Set(c.Context(), user.TenantID, body, result.Body)
		}

		// Back-fill token counts and USD savings for routed requests.
		if routingEventID, ok := c.Locals("routing_event_id").(int64); ok && routingEventID > 0 {
			if originalModelName, ok := c.Locals("original_model_name").(string); ok && originalModelName != "" && usage != nil {
				origModelCfg, _ := modelLookup.GetByName(c.Context(), user.TenantID, originalModelName)
				var origPrice, routedPrice *storage.ModelPrice
				if origModelCfg != nil && origModelCfg.PricePerMInput != nil && origModelCfg.PricePerMOutput != nil {
					origPrice = &storage.ModelPrice{
						PricePerMInput:  *origModelCfg.PricePerMInput,
						PricePerMOutput: *origModelCfg.PricePerMOutput,
					}
				}
				if modelCfg.PricePerMInput != nil && modelCfg.PricePerMOutput != nil {
					routedPrice = &storage.ModelPrice{
						PricePerMInput:  *modelCfg.PricePerMInput,
						PricePerMOutput: *modelCfg.PricePerMOutput,
					}
				}
				go routingStore.UpdateTokensAndSavings(
					context.Background(), routingEventID,
					usage.PromptTokens, usage.CompletionTokens,
					origPrice, routedPrice,
				)
			}
		}

		responseMS := int(time.Since(start).Milliseconds())
		scuCost := tokenizer.ToSCU(usage.PromptTokens, usage.CompletionTokens, modelCfg.SCURate)
		conversationID := c.Get("X-Conversation-ID") // empty string if absent

		promptBytesOriginal, _ := c.Locals("compression_original_bytes").(int)
		promptBytesSaved, _ := c.Locals("compression_saved_bytes").(int)
		promptBytesCompressed := promptBytesOriginal - promptBytesSaved

		usageStore.Record(&storage.UsageRecord{
			TenantID:              user.TenantID,
			UserID:                user.UserID,
			ModelConfigID:         modelCfg.ID,
			ConversationID:        conversationID,
			PromptTokens:          usage.PromptTokens,
			CompletionTokens:      usage.CompletionTokens,
			SCUCost:               scuCost,
			USDCost:               0,
			ResponseMS:            responseMS,
			PromptBytesOriginal:   promptBytesOriginal,
			PromptBytesCompressed: promptBytesCompressed,
		})

		if agentMode, _ := c.Locals("agent_mode").(bool); agentMode && conversationID != "" {
			toolCallCount, _ := c.Locals("agent_tool_call_count").(int)
			agentStore.Record(&storage.AgentRecord{
				TenantID:       user.TenantID,
				UserID:         user.UserID,
				ConversationID: conversationID,
				ToolCallCount:  toolCallCount,
				IsDeadLoop:     false,
			})
		}

		for k, vs := range result.Headers {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}

type siemEnqueuer struct {
	ch    <-chan middleware.SIEMEvent
	store *storage.SIEMGatewayStore
}

func (e *siemEnqueuer) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-e.ch:
			configs, err := e.store.GetActiveConfigs(ctx, ev.TenantID, ev.EventType)
			if err != nil {
				log.Printf("siem enqueuer: get configs: %v", err)
				continue
			}
			for _, cfg := range configs {
				if err := e.store.EnqueueDelivery(ctx, ev.TenantID, cfg.ID, ev.EventType, ev.Payload); err != nil {
					log.Printf("siem enqueuer: enqueue config %s: %v", cfg.ID, err)
				}
			}
		}
	}
}
