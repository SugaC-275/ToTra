package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
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
	"github.com/yourorg/totra/gateway/providers/secrets"
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
	cooldownStore := storage.NewCooldownStore(rdb)
	mcpServerStore := storage.NewMCPServerStore(pool)
	batchStore := storage.NewBatchStore(pool)
	abTestStore := storage.NewABTestStore(pool)
	fineTuningStore := storage.NewFineTuningStore(pool)
	filesStore := storage.NewFilesStore(pool)
	perkeyBudgetStore := storage.NewPerkeyBudgetStore(pool)
	deptStore := storage.NewDepartmentStore(pool)
	promptStore := storage.NewPromptStore(pool)
	apiKeyStore := storage.NewAPIKeyStore(pool)
	requestLogStore := storage.NewRequestLogStore(pool)
	virtualKeyStore := storage.NewVirtualKeyStore(pool)
	policyEventStore := storage.NewPolicyEventStore(pool)
	guardrailStore := storage.NewGuardrailStore(pool, rdb)
	abRouteStore := storage.NewABRouteStore(pool)
	sessionStore := storage.NewSessionStore(pool)
	threadStore := storage.NewThreadStore(pool)
	secretResolver := secrets.New()
	callbackCfg := middleware.LoadCallbackConfig()
	exactCacheTTL := exactCacheDuration()

	if otelShutdown, otelErr := middleware.InitOTEL(context.Background()); otelErr != nil {
		log.Printf("otel: init failed: %v (tracing disabled)", otelErr)
	} else {
		defer otelShutdown(context.Background())
	}

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
		middleware.NewOTELMiddleware(),
		middleware.NewMetricsMiddleware(gatewayMetrics),
		middleware.NewAuthMiddlewareWithJWT(pgUserLookup, virtualKeyStore, cfg.JWTSecret),
		middleware.NewRateLimiterMiddleware(rateLimitStore),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		middleware.NewPerkeyBudgetMiddleware(perkeyBudgetStore),
		middleware.NewDeptBudgetMiddleware(deptStore),
		middleware.NewInjectionMiddleware(siemChan),
		middleware.NewLakeraGuardMiddleware(),
		middleware.NewAporiaMiddleware(),
		middleware.NewPIIMiddleware(piiStore, "", siemChan),
		middleware.NewPHIMiddleware(siemChan),
		middleware.NewPFIMiddleware(siemChan),
		middleware.NewPresidioMiddleware(siemChan),
		middleware.NewPolicyMiddlewareWithGuardrails(policyRuleStore, siemChan, policyEventStore, guardrailStore),
		middleware.NewCompressMiddleware(),
		middleware.NewContextWindowFallbackMiddleware(),
		middleware.NewSpendTagsMiddleware(),
		middleware.NewModelAliasMiddleware(),
		middleware.NewABTestMiddleware(abTestStore),
		middleware.NewABRouterMiddleware(abRouteStore, nil),
		middleware.NewAutoRouterMiddleware(routingStore),
		middleware.NewAgentMiddleware(agentStore),
		middleware.NewCallbackMiddleware(callbackCfg),
		middleware.NewRequestLoggerMiddleware(requestLogStore),
	)

	// Brand safety middleware — applied to all v1 routes when keywords are configured.
	if brandSafetyCfg := middleware.LoadBrandSafetyConfig(); len(brandSafetyCfg) > 0 {
		v1.Use(middleware.NewBrandSafetyMiddleware(brandSafetyCfg))
	}

	proxyHandler := makeProxyHandler(pool, usageStore, agentStore, requestCache, semanticCache, routingStore, cooldownStore, apiKeyStore, secretResolver, exactCacheTTL, sessionStore)
	fallbackMw := middleware.NewFallbackMiddleware(modelLookup, proxyHandler)
	piiResponseMw := middleware.NewPIIResponseMiddleware(siemChan)
	v1.Post("/chat/completions", piiResponseMw, fallbackMw, proxyHandler)
	v1.Post("/messages", piiResponseMw, fallbackMw, proxyHandler)
	v1.Post("/chat/completions/stream", handlers.NewStreamProxyHandler(pool, usageStore))
	v1.Post("/embeddings", handlers.NewEmbeddingsHandler(storage.NewPGModelLookup(pool), usageStore))
	v1.Post("/audio/transcriptions", handlers.NewAudioTranscriptionHandler(storage.NewPGModelLookup(pool), usageStore, siemChan))
	v1.Post("/audio/translations", handlers.NewAudioTranslationHandler(storage.NewPGModelLookup(pool), usageStore, siemChan))
	v1.Post("/images/generations", handlers.NewImagesGenerationHandler(storage.NewPGModelLookup(pool), usageStore))
	v1.Post("/video/generations", handlers.NewVideoGenerationHandler(storage.NewPGModelLookup(pool), usageStore))
	v1.Post("/audio/speech", handlers.NewAudioSpeechHandler(storage.NewPGModelLookup(pool), usageStore))
	v1.Post("/rerank", handlers.NewRerankHandler(storage.NewPGModelLookup(pool), usageStore))
	v1.Post("/moderations", handlers.NewModerationsHandler(storage.NewPGModelLookup(pool), usageStore))
	v1.Post("/mcp/chat", handlers.NewMCPHandler(mcpServerStore, storage.NewPGModelLookup(pool), usageStore))
	handlers.RegisterBatchRoutes(v1, batchStore, storage.NewPGModelLookup(pool))
	handlers.RegisterAssistantsRoutes(v1, storage.NewPGModelLookup(pool), usageStore, sessionStore, threadStore)
	v1.Post("/responses", handlers.NewResponsesHandler(storage.NewPGModelLookup(pool), usageStore))
	handlers.RegisterFineTuningRoutes(v1, fineTuningStore, storage.NewPGModelLookup(pool))
	handlers.RegisterFilesRoutes(v1, filesStore, storage.NewPGModelLookup(pool))
	vsStore := storage.NewVectorStoreStore(pool)
	handlers.RegisterVectorStoreRoutes(v1, vsStore, storage.NewPGModelLookup(pool))
	handlers.RegisterBudgetRoutes(v1, perkeyBudgetStore)
	handlers.RegisterDepartmentRoutes(v1, deptStore)
	handlers.RegisterFailoverRoutes(v1, modelLookup)
	costEstimator := handlers.NewCostEstimator(pool)
	handlers.RegisterCostEstimateRoute(v1, costEstimator)
	handlers.RegisterPromptRoutes(v1, promptStore)
	handlers.RegisterKeyRotationRoutes(v1, apiKeyStore)
	handlers.RegisterRequestLogRoutes(v1, requestLogStore)

	admin := app.Group("/admin",
		middleware.NewAuthMiddlewareWithJWT(pgUserLookup, virtualKeyStore, cfg.JWTSecret),
	)
	handlers.RegisterVirtualKeyRoutes(admin, virtualKeyStore)
	handlers.RegisterSessionRoutes(v1, admin, sessionStore)
	handlers.RegisterComplianceReportRoutes(v1, pool)
	cacheConfigStore := storage.NewCacheConfigStore(pool)
	handlers.RegisterCacheStatsRoutes(v1, pool, requestCache, semanticCache, cacheConfigStore)
	webhookStore := storage.NewWebhookStore(pool)
	webhookDispatcher := handlers.NewWebhookDispatcher(webhookStore)
	handlers.RegisterWebhookRoutes(v1, webhookStore, webhookDispatcher)
	baaStore := storage.NewBAAStore(pool)
	handlers.RegisterBAARoutes(v1, baaStore)

	evalStore := storage.NewEvalStore(pool)
	handlers.RegisterEvalRoutes(v1, evalStore, promptStore, storage.NewPGModelLookup(pool), webhookDispatcher)
	handlers.RegisterABRouteRoutes(v1, abRouteStore)

	batchWorker := handlers.NewBatchWorker(batchStore, storage.NewPGModelLookup(pool), 5*time.Second)
	go batchWorker.Run(context.Background())

	ftPoller := handlers.NewFineTuningPoller(fineTuningStore, storage.NewPGModelLookup(pool), webhookDispatcher)
	go ftPoller.Run(context.Background())

	pricingSyncer := handlers.NewPricingSyncer(pool, 24*time.Hour)
	go pricingSyncer.Run(context.Background())

	realtimeSessionStore := storage.NewRealtimeSessionStore(rdb, pool)
	realtimeDeps := handlers.RealtimeDeps{
		ModelLookup:   storage.NewPGModelLookup(pool),
		QuotaStore:    quotaStore,
		QuotaFetcher:  pgUserQuota,
		UsageStore:    usageStore,
		BundleChecker: modelLookup,
		SessionStore:  realtimeSessionStore,
	}
	// /v1/realtime is mounted separately from the v1 group because WebSocket
	// upgrade is incompatible with the body-oriented middleware chain.
	app.Get("/v1/realtime",
		middleware.NewAuthMiddlewareWithJWT(pgUserLookup, nil, cfg.JWTSecret),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		handlers.NewRealtimeHandler(realtimeDeps),
	)
	// /admin/realtime/sessions — list active and recent sessions with PII counts.
	handlers.RegisterRealtimeRoutes(app, realtimeDeps)

	app.Post("/v1/files/chat",
		middleware.NewAuthMiddlewareWithJWT(pgUserLookup, nil, cfg.JWTSecret),
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

// exactCacheDuration reads EXACT_CACHE_TTL_HOURS (default 7 days).
func exactCacheDuration() time.Duration {
	if h, err := strconv.Atoi(os.Getenv("EXACT_CACHE_TTL_HOURS")); err == nil && h > 0 {
		return time.Duration(h) * time.Hour
	}
	return 20 * 24 * time.Hour
}

func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore, agentStore *storage.AgentStore, cache *storage.RequestCache, semCache *storage.SemanticCache, routingStore *storage.RoutingStore, cooldownStore *storage.CooldownStore, apiKeyStore *storage.APIKeyStore, secretResolver *secrets.Resolver, exactCacheTTL time.Duration, sessionStore *storage.SessionStore) fiber.Handler {
	modelLookup := storage.NewPGModelLookup(pool)

	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)
		start := time.Now()
		body := string(c.Body())
		yearMonth := time.Now().UTC().Format("2006-01")

		// Session resolution: GetOrCreate from header; propagate ID back in response.
		incomingSessionID := c.Get("X-ToTra-Session-Id")
		var activeSession *storage.Session
		if sessionStore != nil {
			activeSession, _ = sessionStore.GetOrCreate(c.Context(), user.TenantID, user.UserID, incomingSessionID)
			if activeSession != nil {
				c.Set("X-ToTra-Session-Id", activeSession.ID)
			}
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

		// Healthcare compliance: block PHI requests to non-BAA-compliant models.
		if requireBAA, _ := c.Locals("require_baa").(bool); requireBAA && !modelCfg.BAACompliant {
			return c.Status(451).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "PHI detected: request must be routed to a BAA-compliant model",
					"type":    "compliance_error",
				},
			})
		}

		// Financial compliance: warn when PFI is routed to a non-FINRA model.
		// Warn-only (not a block) — financial teams may intentionally use non-FINRA models.
		if requireFINRA, _ := c.Locals("require_finra").(bool); requireFINRA && !modelCfg.FINRACompliant {
			slog.Warn("PFI detected routed to non-FINRA model", "tenant", user.TenantID, "model", reqBody.Model)
			c.Set("X-Compliance-Warning", "pfi-detected-non-finra-model")
		}
		// SOX: flag all requests for audit when enabled on the model config.
		if modelCfg.SOXAuditEnabled {
			c.Locals("sox_audit", true)
		}

		// Bundle-level provider enforcement (healthcare → HIPAA-eligible, government → GovCloud).
		if err := handlers.CheckBundleCompliance(c, user, modelCfg, modelLookup); err != nil {
			return err
		}

		// Cache lookup — skipped entirely when cache_disabled is set on the model.
		cacheKey := storage.CacheKey(user.TenantID, body)
		if !modelCfg.CacheDisabled {
			if cached, ok := cache.Get(c.Context(), cacheKey); ok {
				cache.IncrHit(c.Context(), user.TenantID, yearMonth)
				c.Set("X-Cache", "HIT")
				return c.Status(200).Send(cached)
			}
			if cached, ok := semCache.Get(c.Context(), user.TenantID, body); ok {
				semCache.IncrHit(c.Context(), user.TenantID, yearMonth)
				c.Set("X-Cache", "SEMANTIC-HIT")
				return c.Status(200).Send(cached)
			}
		}

		if cooling, _ := cooldownStore.IsCooling(c.Context(), modelCfg.Provider); cooling {
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": fiber.Map{"message": fmt.Sprintf("provider %q is temporarily unavailable, retrying shortly", modelCfg.Provider)},
			})
		}

		// Attach estimated cost header before forwarding (fast inline lookup).
		if inputCost, outputCost, ok := handlers.LookupPricing(pool, c.Context(), reqBody.Model); ok {
			tokens := handlers.EstimateChatTokens(c.Body())
			estimatedUSD := float64(tokens)*inputCost + 500*outputCost
			c.Set("X-Estimated-Cost-USD", fmt.Sprintf("%.8f", estimatedUSD))
		}

		apiKey := secretResolver.Resolve(c.Context(), modelCfg.APIKey)

		// Try multi-key rotation first; fall back to model_config.api_key when not configured.
		if keyID, rotatedKey, kerr := apiKeyStore.GetNextKey(c.Context(), modelCfg.ID); kerr == nil && rotatedKey != "" {
			apiKey = rotatedKey
			c.Locals("api_key_id", keyID)
		}

		var fwd providers.Adapter
		fwd, err = providers.New(modelCfg.Provider, modelCfg.BaseURL, apiKey)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("unsupported provider %q for model %q", modelCfg.Provider, reqBody.Model),
			}})
		}

		result, usage, err := fwd.Forward(c.Context(), c.Body())
		if err != nil {
			slog.Error("upstream forward error", "tenant", user.TenantID, "model", reqBody.Model, "err", err)
			_ = cooldownStore.MarkFailure(context.Background(), modelCfg.Provider)
			if keyID, ok := c.Locals("api_key_id").(string); ok && keyID != "" {
				go apiKeyStore.MarkFailure(context.Background(), keyID)
			}
			return c.Status(502).JSON(fiber.Map{"error": "upstream unavailable"})
		}
		if result.StatusCode >= 500 {
			_ = cooldownStore.MarkFailure(context.Background(), modelCfg.Provider)
			if keyID, ok := c.Locals("api_key_id").(string); ok && keyID != "" {
				go apiKeyStore.MarkFailure(context.Background(), keyID)
			}
		} else if result.StatusCode == 429 {
			if keyID, ok := c.Locals("api_key_id").(string); ok && keyID != "" {
				go apiKeyStore.MarkFailure(context.Background(), keyID)
			}
		} else if result.StatusCode == 200 {
			_ = cooldownStore.MarkSuccess(context.Background(), modelCfg.Provider)
			if keyID, ok := c.Locals("api_key_id").(string); ok && keyID != "" {
				go apiKeyStore.MarkSuccess(context.Background(), keyID)
			}
		}

		if result.StatusCode == 200 && !modelCfg.CacheDisabled {
			cache.Set(c.Context(), cacheKey, result.Body, exactCacheTTL)
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
		tags, _ := c.Locals("spend_tags").([]string)

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
			Tags:                  tags,
		})

		// Update session governance stats async (non-blocking).
		if sessionStore != nil && activeSession != nil {
			totalTokens := int64(usage.PromptTokens + usage.CompletionTokens)
			piiHit := c.Locals("pii_hit") != nil
			sessionStore.UpdateStatsAsync(activeSession.ID, totalTokens, 0, piiHit)
		}

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
