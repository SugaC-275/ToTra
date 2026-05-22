package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
	"github.com/yourorg/totra/gateway/tokenizer"
)

// StreamModelLookup is satisfied by *storage.PGModelLookup.
type StreamModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// NewStreamProxyHandler returns a Fiber handler that proxies streaming LLM
// requests. The request body must contain "stream": true and a "model" field.
// Responses are delivered as text/event-stream (SSE).
//
// Usage is recorded asynchronously after the stream completes using a token
// estimate derived from the request body length and the accumulated response.
func NewStreamProxyHandler(pool *pgxpool.Pool, usageStore *storage.UsageStore) fiber.Handler {
	modelLookup := storage.NewPGModelLookup(pool)
	return newStreamProxyHandler(modelLookup, usageStore)
}

// NewStreamProxyHandlerForTest is the exported inner constructor for testing.
// Production code should use NewStreamProxyHandler.
func NewStreamProxyHandlerForTest(modelLookup StreamModelLookup, usageStore UsageRecorder) fiber.Handler {
	return newStreamProxyHandler(modelLookup, usageStore)
}

// newStreamProxyHandler is the testable inner constructor that accepts interfaces.
func newStreamProxyHandler(modelLookup StreamModelLookup, usageStore UsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)
		body := c.Body()

		// Reject non-streaming requests — callers should use the regular proxy.
		if !requestHasStream(body) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": `"stream": true is required for this endpoint; use /v1/chat/completions for buffered responses`,
					"type":    "bad_request",
				},
			})
		}

		var reqFields struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &reqFields); err != nil || reqFields.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model field required", "type": "bad_request"},
			})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, reqFields.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured for your tenant", reqFields.Model),
					"type":    "model_not_found",
				},
			})
		}

		fwd, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("unsupported provider %q for model %q", modelCfg.Provider, reqFields.Model),
					"type":    "unsupported_provider",
				},
			})
		}

		start := time.Now()
		promptTokens := tokenizer.EstimateTokensFromBody(body)
		conversationID := c.Get("X-Conversation-ID")
		promptBytesOriginal, _ := c.Locals("compression_original_bytes").(int)
		promptBytesSaved, _ := c.Locals("compression_saved_bytes").(int)
		promptBytesCompressed := promptBytesOriginal - promptBytesSaved

		// Set SSE headers before writing any body.
		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("X-Accel-Buffering", "no")

		w := c.Context().Response.BodyWriter()
		var completionBytes int

		streamErr := fwd.ForwardStream(c.Context(), body, func(chunk []byte) error {
			completionBytes += len(chunk)
			_, writeErr := w.Write(chunk)
			return writeErr
		})

		if streamErr != nil {
			// If nothing has been written yet we can still return a JSON error.
			// Once the stream has started we cannot change the status code.
			if completionBytes == 0 {
				return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
					"error": fiber.Map{"message": "upstream error: " + streamErr.Error(), "type": "upstream_error"},
				})
			}
			// Stream already started — write an SSE error event so the client knows.
			errLine := fmt.Sprintf("data: {\"error\":{\"message\":%q}}\n\n", streamErr.Error())
			_, _ = w.Write([]byte(errLine))
		}

		// Estimate completion tokens from response byte count (~4 bytes/token).
		completionTokens := completionBytes / 4
		if completionTokens < 1 && completionBytes > 0 {
			completionTokens = 1
		}

		responseMS := int(time.Since(start).Milliseconds())
		scuCost := tokenizer.ToSCU(promptTokens, completionTokens, modelCfg.SCURate)

		usageStore.Record(&storage.UsageRecord{
			TenantID:              user.TenantID,
			UserID:                user.UserID,
			ModelConfigID:         modelCfg.ID,
			ConversationID:        conversationID,
			PromptTokens:          promptTokens,
			CompletionTokens:      completionTokens,
			SCUCost:               scuCost,
			USDCost:               0,
			ResponseMS:            responseMS,
			PromptBytesOriginal:   promptBytesOriginal,
			PromptBytesCompressed: promptBytesCompressed,
		})

		return nil
	}
}

// requestHasStream returns true when the JSON body contains "stream": true.
func requestHasStream(body []byte) bool {
	var m struct {
		Stream *bool `json:"stream"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return false
	}
	return m.Stream != nil && *m.Stream
}
