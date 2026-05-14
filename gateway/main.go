package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/yourorg/totra/gateway/config"
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

	pool, err := pgxpool.New(context.Background(), cfg.PostgresDSN)
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

	app := fiber.New(fiber.Config{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	})

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	v1 := app.Group("/v1",
		middleware.NewAuthMiddleware(pgUserLookup),
		middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
		middleware.NewPIIMiddleware(piiStore, ""),
		middleware.NewAgentMiddleware(agentStore),
	)

	proxyHandler := makeProxyHandler(pool, usageStore, agentStore)
	v1.Post("/chat/completions", proxyHandler)
	v1.Post("/messages", proxyHandler)

	log.Printf("Gateway listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}

func makeProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore, agentStore *storage.AgentStore) fiber.Handler {
	modelLookup := storage.NewPGModelLookup(pool)

	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)
		start := time.Now()

		var reqBody struct {
			Model string `json:"model"`
		}
		if err := c.BodyParser(&reqBody); err != nil || reqBody.Model == "" {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "model field required"}})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, reqBody.Model)
		if err != nil || modelCfg == nil {
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("model %q not configured for your tenant", reqBody.Model),
			}})
		}

		var fwd interface {
			Forward(ctx context.Context, body []byte) (*providers.ForwardResult, *providers.Usage, error)
		}
		switch modelCfg.Provider {
		case "openai":
			fwd = providers.NewOpenAIAdapter(modelCfg.BaseURL, modelCfg.APIKey)
		case "anthropic":
			fwd = providers.NewAnthropicAdapter(modelCfg.BaseURL, modelCfg.APIKey)
		case "local":
			fwd = providers.NewLocalAdapter(modelCfg.BaseURL)
		default:
			return c.Status(400).JSON(fiber.Map{"error": fiber.Map{"message": "unsupported provider"}})
		}

		result, usage, err := fwd.Forward(c.Context(), c.Body())
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": fiber.Map{"message": "upstream error: " + err.Error()}})
		}

		responseMS := int(time.Since(start).Milliseconds())
		scuCost := tokenizer.ToSCU(usage.PromptTokens, usage.CompletionTokens, modelCfg.SCURate)
		conversationID := c.Get("X-Conversation-ID") // empty string if absent

		usageStore.Record(&storage.UsageRecord{
			TenantID:         user.TenantID,
			UserID:           user.UserID,
			ModelConfigID:    modelCfg.ID,
			ConversationID:   conversationID,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			SCUCost:          scuCost,
			USDCost:          0,
			ResponseMS:       responseMS,
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
