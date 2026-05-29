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

type aporiaCheckRequest struct {
	Messages []map[string]string `json:"messages"`
	Model    string              `json:"model,omitempty"`
}

type aporiaCheckResponse struct {
	Status string `json:"status"` // "passed" | "failed"
	Reason string `json:"reason,omitempty"`
}

// NewAporiaMiddleware calls Aporia to validate requests against configured guardrails.
// APORIA_API_KEY and APORIA_PROJECT_ID env vars enable it. Blocks when status=failed.
// On timeout or error, passes through (fail-open).
func NewAporiaMiddleware() fiber.Handler {
	apiKey := os.Getenv("APORIA_API_KEY")
	projectID := os.Getenv("APORIA_PROJECT_ID")
	if apiKey == "" || projectID == "" {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	client := &http.Client{Timeout: 3 * time.Second}
	endpoint := "https://api.aporia.com/v1/guardrails/check"

	return func(c *fiber.Ctx) error {
		var reqBody struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(c.Body(), &reqBody); err != nil || len(reqBody.Messages) == 0 {
			return c.Next()
		}

		msgs := make([]map[string]string, 0, len(reqBody.Messages))
		for _, m := range reqBody.Messages {
			msgs = append(msgs, map[string]string{"role": m.Role, "content": m.Content})
		}

		payload := aporiaCheckRequest{Messages: msgs, Model: reqBody.Model}
		body, err := json.Marshal(payload)
		if err != nil {
			return c.Next()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return c.Next()
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-APORIA-API-KEY", apiKey)
		req.Header.Set("X-APORIA-PROJECT-ID", projectID)

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("aporia: request failed, passing through", "err", err)
			return c.Next()
		}
		defer resp.Body.Close()

		var aporiaResp aporiaCheckResponse
		if err := json.NewDecoder(resp.Body).Decode(&aporiaResp); err != nil {
			return c.Next()
		}

		if aporiaResp.Status == "failed" {
			reason := aporiaResp.Reason
			if reason == "" {
				reason = "blocked by Aporia guardrail"
			}
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": reason,
					"type":    "guardrail_blocked",
					"code":    "aporia_policy_violation",
				},
			})
		}
		return c.Next()
	}
}
