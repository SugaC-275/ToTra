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

type ImagesModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

type ImagesUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

type imageGenRequest struct {
	Model string `json:"model"`
	N     int    `json:"n"`
}

// NewImagesGenerationHandler returns a Fiber handler for POST /v1/images/generations.
// Proxies to OpenAI DALL-E or other image generation providers.
func NewImagesGenerationHandler(lookup ImagesModelLookup, usageRecorder ImagesUsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		var req imageGenRequest
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

		imagesURL, err := buildImagesURL(modelCfg.Provider, modelCfg.BaseURL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error(), "type": "unsupported_provider"},
			})
		}

		start := time.Now()
		result, err := forwardImages(c.Context(), imagesURL, modelCfg.APIKey, c.Body())
		if err != nil {
			slog.Error("images upstream error", "tenant", user.TenantID, "model", req.Model, "err", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		responseMS := int(time.Since(start).Milliseconds())
		if usageRecorder != nil {
			n := req.N
			if n <= 0 {
				n = 1
			}
			usageRecorder.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				PromptTokens:  n,
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

func buildImagesURL(provider, baseURL string) (string, error) {
	base := strings.TrimRight(baseURL, "/")
	switch provider {
	case "openai", "azure":
		return base + "/images/generations", nil
	case "anthropic", "gemini", "bedrock":
		return "", fmt.Errorf("provider %q does not support image generation", provider)
	default:
		// stability, replicate, etc.
		return base + "/images/generations", nil
	}
}

func forwardImages(ctx context.Context, url, apiKey string, body []byte) (*proxyResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("images: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("images: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("images: read response: %w", err)
	}
	return &proxyResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}
