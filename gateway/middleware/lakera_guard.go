package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

type lakeraRequest struct {
	Input []map[string]string `json:"input"`
}

type lakeraResponse struct {
	Results []struct {
		Flagged bool `json:"flagged"`
	} `json:"results"`
}

// NewLakeraGuardMiddleware calls Lakera Guard to detect prompt injection and jailbreak attempts.
// LAKERA_GUARD_API_KEY env var enables it. Blocks requests where flagged=true.
// On timeout or error, passes through (fail-open).
func NewLakeraGuardMiddleware() fiber.Handler {
	apiKey := os.Getenv("LAKERA_GUARD_API_KEY")
	if apiKey == "" {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	client := &http.Client{Timeout: 3 * time.Second}

	return func(c *fiber.Ctx) error {
		var reqBody struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(c.Body(), &reqBody); err != nil || len(reqBody.Messages) == 0 {
			return c.Next()
		}

		// Send last user message to Lakera for injection detection.
		var userContent string
		for _, m := range reqBody.Messages {
			if m.Role == "user" {
				userContent = m.Content
			}
		}
		if userContent == "" {
			return c.Next()
		}

		lakeraReq := lakeraRequest{
			Input: []map[string]string{{"role": "user", "content": userContent}},
		}
		body, err := json.Marshal(lakeraReq)
		if err != nil {
			return c.Next()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"https://api.lakera.ai/v1/prompt_injection", bytes.NewReader(body))
		if err != nil {
			return c.Next()
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+apiKey)

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("lakera: request failed, passing through", "err", err)
			return c.Next()
		}
		defer resp.Body.Close()

		var lakeraResp lakeraResponse
		if err := json.NewDecoder(resp.Body).Decode(&lakeraResp); err != nil {
			return c.Next()
		}

		if len(lakeraResp.Results) > 0 && lakeraResp.Results[0].Flagged {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "request blocked by content safety guardrail",
					"type":    "guardrail_blocked",
					"code":    "lakera_injection_detected",
				},
			})
		}
		return c.Next()
	}
}
