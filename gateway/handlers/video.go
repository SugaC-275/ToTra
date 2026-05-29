package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
)

// VideoModelLookup is satisfied by *storage.PGModelLookup.
type VideoModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// VideoUsageRecorder is satisfied by *storage.UsageStore.
type VideoUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

type videoGenRequest struct {
	Model string `json:"model"`
}

// NewVideoGenerationHandler returns a Fiber handler for POST /v1/video/generations.
// It follows the same pattern as NewImagesGenerationHandler: extract model,
// look up config, delegate to the provider adapter, record usage.
func NewVideoGenerationHandler(lookup VideoModelLookup, usageRecorder VideoUsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		var req videoGenRequest
		if err := json.Unmarshal(c.Body(), &req); err != nil || req.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model field required", "type": "bad_request"},
			})
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, req.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured", req.Model),
					"type":    "model_not_found",
				},
			})
		}

		adapter, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("unsupported provider %q", modelCfg.Provider),
					"type":    "unsupported_provider",
				},
			})
		}

		start := time.Now()
		result, usage, err := adapter.Forward(c.Context(), c.Body())
		if err != nil {
			slog.Error("video upstream error",
				"tenant", user.TenantID,
				"model", req.Model,
				"err", err,
			)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
				"error": "upstream unavailable",
			})
		}

		responseMS := int(time.Since(start).Milliseconds())
		if usageRecorder != nil && usage != nil {
			usageRecorder.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				PromptTokens:  usage.PromptTokens,
				ResponseMS:    responseMS,
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
