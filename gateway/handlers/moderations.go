package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

type ModerationsModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

type ModerationsUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

type moderationRequest struct {
	Input string `json:"input"`
	Model string `json:"model"`
}

// NewModerationsHandler returns a Fiber handler for POST /v1/moderations.
// Proxies to the configured provider's moderation endpoint (OpenAI-compatible).
func NewModerationsHandler(lookup ModerationsModelLookup, usageRecorder ModerationsUsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		var req moderationRequest
		if err := json.Unmarshal(c.Body(), &req); err != nil || req.Input == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "input field required", "type": "bad_request"},
			})
		}

		// Default to omni-moderation-latest if no model specified.
		if req.Model == "" {
			req.Model = "omni-moderation-latest"
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, req.Model)
		if err != nil || modelCfg == nil {
			// Fall back to any OpenAI model config for the tenant.
			modelCfg, err = lookup.GetByName(c.Context(), user.TenantID, "gpt-4o")
			if err != nil || modelCfg == nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": fiber.Map{
						"message": fmt.Sprintf("no moderation-compatible model configured for tenant"),
						"type":    "model_not_found",
					},
				})
			}
		}

		moderationURL, err := buildModerationURL(modelCfg.Provider, modelCfg.BaseURL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error(), "type": "unsupported_provider"},
			})
		}

		start := time.Now()
		result, err := forwardModeration(c.Context(), moderationURL, modelCfg.APIKey, c.Body())
		if err != nil {
			slog.Error("moderation upstream error", "tenant", user.TenantID, "err", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		responseMS := int(time.Since(start).Milliseconds())
		if usageRecorder != nil {
			usageRecorder.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				PromptTokens:  len(strings.Fields(req.Input)),
				ResponseMS:    responseMS,
			})
		}

		for k, vs := range result.Header {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}

func buildModerationURL(provider, baseURL string) (string, error) {
	base := strings.TrimRight(baseURL, "/")
	switch provider {
	case "openai", "azure":
		return base + "/moderations", nil
	case "anthropic", "gemini", "bedrock":
		return "", fmt.Errorf("provider %q does not support moderation endpoint", provider)
	default:
		return base + "/moderations", nil
	}
}

func forwardModeration(ctx context.Context, url, apiKey string, body []byte) (*proxyResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("moderation: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("moderation: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("moderation: read response: %w", err)
	}
	return &proxyResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}
