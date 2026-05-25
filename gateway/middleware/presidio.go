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

// presidioEntity is one result from Presidio's /analyze endpoint.
type presidioEntity struct {
	EntityType string  `json:"entity_type"` // PERSON, LOCATION, EMAIL_ADDRESS, etc.
	Score      float64 `json:"score"`
	Start      int     `json:"start"`
	End        int     `json:"end"`
}

// presidioRequest is the body sent to Presidio's /analyze endpoint.
type presidioRequest struct {
	Text     string `json:"text"`
	Language string `json:"language"`
}

// NewPresidioMiddleware adds a second PII detection layer using Microsoft Presidio.
// It is activated only when PRESIDIO_ANALYZER_URL is set in the environment.
// Presidio catches semantic PII (PERSON names, LOCATION, ORG) that regex cannot.
// The middleware blocks requests where Presidio returns any entity with score >= threshold.
//
// Deploy Presidio analyzer sidecar:
//
//	docker run -p 5002:3000 mcr.microsoft.com/presidio-analyzer:latest
//
// Then set: PRESIDIO_ANALYZER_URL=http://localhost:5002
func NewPresidioMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	analyzerURL := os.Getenv("PRESIDIO_ANALYZER_URL")
	if analyzerURL == "" {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	endpoint := analyzerURL + "/analyze"

	threshold := 0.7
	client := &http.Client{Timeout: 3 * time.Second}

	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		if body == "" {
			return c.Next()
		}

		entities, err := callPresidio(c.Context(), client, endpoint, body, "en")
		if err != nil {
			slog.Warn("presidio: analyzer unavailable, skipping", "err", err)
			return c.Next()
		}

		for _, e := range entities {
			if e.Score >= threshold {
				user, _ := c.Locals("user").(*UserInfo)
				tid, uid := "", ""
				if user != nil {
					tid = user.TenantID
					uid = user.UserID
				}

				if siemChan != nil {
					select {
					case siemChan <- SIEMEvent{
						TenantID:  tid,
						EventType: "pii_violation",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   tid,
							"event_type":  "pii_violation",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id":   uid,
								"pii_type":  "presidio:" + e.EntityType,
								"score":     e.Score,
								"action":    "blocked",
								"path":      c.Path(),
							},
						},
					}:
					default:
					}
				}

				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: semantic PII detected (" + e.EntityType + ")",
						"type":    "pii_blocked",
					},
				})
			}
		}
		return c.Next()
	}
}

func callPresidio(ctx context.Context, client *http.Client, endpoint, text, language string) ([]presidioEntity, error) {
	reqBody, err := json.Marshal(presidioRequest{Text: text, Language: language})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var entities []presidioEntity
	if err := json.NewDecoder(resp.Body).Decode(&entities); err != nil {
		return nil, err
	}
	return entities, nil
}
