package middleware

import (
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"
)

// NewResponsePIIScanMiddleware scans LLM response bodies for PHI/PII after they arrive.
// Non-blocking — never modifies or delays the response. On a match it sets the
// X-Response-PII-Detected header and emits a SIEM event. The middleware is only active
// when the healthcare bundle is enabled on the tenant, indicated by the require_baa
// local set by NewPHIMiddleware (or any earlier middleware that marks PHI risk).
//
// siemChan may be nil (SIEM delivery is skipped).
func NewResponsePIIScanMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Execute the handler chain first — response is written by the proxy handler.
		if err := c.Next(); err != nil {
			return err
		}

		// Only scan healthcare-bundle requests to avoid redundant work.
		requireBAA, _ := c.Locals("require_baa").(bool)
		if !requireBAA {
			return nil
		}
		// Only scan successful LLM responses.
		if c.Response().StatusCode() != fiber.StatusOK {
			return nil
		}

		body := string(c.Response().Body())
		if body == "" {
			return nil
		}

		piiType, found := ScanForPII(body)
		if !found {
			return nil
		}

		// Set header so downstream (e.g. API clients) can detect the event.
		c.Response().Header.Set("X-Response-PII-Detected", piiType)

		tid := ""
		uid := ""
		if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
			tid = user.TenantID
			uid = user.UserID
		}

		slog.Warn("PHI detected in LLM response",
			"tenant", tid,
			"user", uid,
			"pii_type", piiType,
			"path", c.Path(),
		)

		if siemChan != nil {
			select {
			case siemChan <- SIEMEvent{
				TenantID:  tid,
				EventType: "response_phi_detected",
				Payload: map[string]any{
					"source":      "totra",
					"tenant_id":   tid,
					"event_type":  "response_phi_detected",
					"occurred_at": time.Now().UTC().Format(time.RFC3339),
					"detail": map[string]any{
						"user_id":  uid,
						"pii_type": piiType,
						"action":   "logged",
						"path":     c.Path(),
					},
				},
			}:
			default: // drop if channel full
			}
		}

		return nil
	}
}
