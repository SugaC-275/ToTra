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

// EmbeddingsModelLookup is satisfied by *storage.PGModelLookup.
type EmbeddingsModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// EmbeddingsUsageRecorder is satisfied by *storage.UsageStore.
type EmbeddingsUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

// embeddingsRequest captures only the fields needed from the request body.
type embeddingsRequest struct {
	Model string `json:"model"`
}

// embeddingsUsageResponse captures the usage block from a provider response.
type embeddingsUsageResponse struct {
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
	} `json:"usage"`
}

// NewEmbeddingsHandler returns a Fiber handler for POST /v1/embeddings.
// It requires the auth middleware to have set c.Locals("user").
func NewEmbeddingsHandler(lookup EmbeddingsModelLookup, usageRecorder EmbeddingsUsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		// Peek at the model field without fully deserializing the body.
		var req embeddingsRequest
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

		embURL, err := buildEmbeddingsURL(modelCfg.Provider, modelCfg.BaseURL)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error(), "type": "unsupported_provider"},
			})
		}

		start := time.Now()
		result, promptTokens, err := forwardEmbeddings(c.Context(), embURL, modelCfg.APIKey, c.Body())
		if err != nil {
			slog.Error("embeddings upstream error", "tenant", user.TenantID, "model", req.Model, "err", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		responseMS := int(time.Since(start).Milliseconds())
		if usageRecorder != nil {
			usdCost := 0.0
			if modelCfg.PricePerMInput != nil && promptTokens > 0 {
				usdCost = float64(promptTokens) / 1_000_000 * *modelCfg.PricePerMInput
			}
			usageRecorder.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				PromptTokens:  promptTokens,
				USDCost:       usdCost,
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

// buildEmbeddingsURL returns the provider-specific embeddings endpoint URL.
func buildEmbeddingsURL(provider, baseURL string) (string, error) {
	base := strings.TrimRight(baseURL, "/")
	switch provider {
	case "openai", "azure":
		// OpenAI: https://api.openai.com/v1  → .../embeddings
		// Azure:  https://<host>/openai/deployments/<deploy> → .../embeddings
		return base + "/embeddings", nil
	case "anthropic", "gemini":
		// These providers do not expose an embeddings endpoint via the same path.
		return "", fmt.Errorf("provider %q does not support embeddings", provider)
	default:
		// Generic: just append /embeddings and let the upstream decide.
		return base + "/embeddings", nil
	}
}

// proxyResult is a minimal HTTP response container used internally.
type proxyResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// forwardEmbeddings POSTs body to url with the given apiKey and returns the
// raw provider response plus the reported prompt_tokens count.
func forwardEmbeddings(ctx context.Context, url, apiKey string, body []byte) (*proxyResult, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("embeddings: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("embeddings: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("embeddings: read response: %w", err)
	}

	promptTokens := 0
	var usageResp embeddingsUsageResponse
	if err := json.Unmarshal(respBody, &usageResp); err == nil {
		promptTokens = usageResp.Usage.PromptTokens
	}

	return &proxyResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       respBody,
	}, promptTokens, nil
}
