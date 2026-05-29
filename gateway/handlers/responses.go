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

// ResponsesModelLookup is satisfied by *storage.PGModelLookup.
type ResponsesModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// ResponsesUsageRecorder is satisfied by *storage.UsageStore.
type ResponsesUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

// responsesRequest captures the fields ToTra needs from the Responses API body.
type responsesRequest struct {
	Model  string `json:"model"`
	Stream *bool  `json:"stream"`
}

// responsesUsageBody captures the usage block from a non-streaming Responses API response.
// The Responses API uses input_tokens / output_tokens (not prompt_tokens / completion_tokens).
type responsesUsageBody struct {
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// NewResponsesHandler returns a Fiber handler for POST /v1/responses.
// It supports both streaming (SSE) and non-streaming requests.
func NewResponsesHandler(lookup ResponsesModelLookup, usageRecorder ResponsesUsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		body := c.Body()
		var req responsesRequest
		if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model field required", "type": "bad_request"},
			})
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, req.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured for your tenant", req.Model),
					"type":    "model_not_found",
				},
			})
		}

		base := strings.TrimRight(modelCfg.BaseURL, "/")
		upstreamURL := base + "/responses"

		isStream := req.Stream != nil && *req.Stream

		if isStream {
			return handleResponsesStream(c, user, modelCfg, upstreamURL, body, usageRecorder)
		}
		return handleResponsesBuffered(c, user, modelCfg, req.Model, upstreamURL, body, usageRecorder)
	}
}

// handleResponsesBuffered handles non-streaming Responses API calls.
func handleResponsesBuffered(
	c *fiber.Ctx,
	user *middleware.UserInfo,
	modelCfg *storage.ModelConfig,
	modelName, upstreamURL string,
	body []byte,
	usageRecorder ResponsesUsageRecorder,
) error {
	start := time.Now()
	result, err := forwardResponses(c.Context(), upstreamURL, modelCfg.APIKey, body)
	if err != nil {
		slog.Error("responses upstream error", "tenant", user.TenantID, "model", modelName, "err", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
	}

	responseMS := int(time.Since(start).Milliseconds())

	if usageRecorder != nil {
		var usageResp responsesUsageBody
		_ = json.Unmarshal(result.Body, &usageResp)
		usageRecorder.Record(&storage.UsageRecord{
			TenantID:         user.TenantID,
			UserID:           user.UserID,
			ModelConfigID:    modelCfg.ID,
			PromptTokens:     usageResp.Usage.InputTokens,
			CompletionTokens: usageResp.Usage.OutputTokens,
			ResponseMS:       responseMS,
		})
	}

	for k, vs := range result.Header {
		for _, v := range vs {
			c.Set(k, v)
		}
	}
	return c.Status(result.StatusCode).Send(result.Body)
}

// handleResponsesStream handles streaming Responses API calls using SSE passthrough.
func handleResponsesStream(
	c *fiber.Ctx,
	user *middleware.UserInfo,
	modelCfg *storage.ModelConfig,
	upstreamURL string,
	body []byte,
	usageRecorder ResponsesUsageRecorder,
) error {
	httpReq, err := http.NewRequestWithContext(c.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "failed to build upstream request"})
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if modelCfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+modelCfg.APIKey)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		slog.Error("responses stream upstream error", "tenant", user.TenantID, "err", err)
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
	}
	defer resp.Body.Close()

	// Set SSE headers.
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")
	c.Status(resp.StatusCode)

	w := c.Context().Response.BodyWriter()
	var completionBytes int

	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			completionBytes += n
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				break
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			slog.Error("responses stream read error", "tenant", user.TenantID, "err", readErr)
			break
		}
	}

	responseMS := int(time.Since(start).Milliseconds())
	if usageRecorder != nil {
		// Token counts not available mid-stream; record byte-estimated values.
		completionTokens := completionBytes / 4
		usageRecorder.Record(&storage.UsageRecord{
			TenantID:         user.TenantID,
			UserID:           user.UserID,
			ModelConfigID:    modelCfg.ID,
			CompletionTokens: completionTokens,
			ResponseMS:       responseMS,
		})
	}

	return nil
}

// forwardResponses issues a single POST to the upstream Responses endpoint.
func forwardResponses(ctx context.Context, url, apiKey string, body []byte) (*proxyResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("responses: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("responses: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("responses: read response: %w", err)
	}
	return &proxyResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}
