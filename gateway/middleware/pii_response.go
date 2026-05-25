package middleware

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// MaskPII scans text against all piiPatterns and replaces every match with
// [REDACTED:{type}]. It returns the (possibly modified) text, whether any
// replacement occurred, and the name of the first PII type found.
func MaskPII(text string) (masked string, found bool, piiType string) {
	result := text
	for _, rule := range piiPatterns {
		replaced := rule.re.ReplaceAllString(result, "[REDACTED:"+rule.name+"]")
		if replaced != result {
			if !found {
				// Record the first PII type encountered for SIEM reporting.
				piiType = rule.name
			}
			found = true
			result = replaced
		}
	}
	return result, found, piiType
}

// NewPIIResponseMiddleware returns a Fiber middleware that runs after c.Next()
// and scans the response body for PII. Any matches are replaced in-place with
// [REDACTED:{type}]. When masking occurs, the X-PII-Masked: true header is
// added and a SIEM event of type "pii_in_response" is emitted (if siemChan is
// non-nil). The request is never blocked — LLM responses are always forwarded.
func NewPIIResponseMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Run the downstream handler chain first.
		if err := c.Next(); err != nil {
			return err
		}

		body := string(c.Response().Body())
		if body == "" {
			return nil
		}

		masked, found, piiType := MaskPII(body)
		if !found {
			return nil
		}

		// Overwrite the response body with the masked version.
		c.Response().SetBodyString(masked)
		c.Set("X-PII-Masked", "true")

		if siemChan != nil {
			tid := ""
			uid := ""
			if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
				tid = user.TenantID
				uid = user.UserID
			}
			event := buildPIIResponseEvent(tid, uid, piiType, c.Path())
			select {
			case siemChan <- event:
			default: // drop if channel is full — never block the response path
			}
		}

		return nil
	}
}

func buildPIIResponseEvent(tenantID, userID, piiType, path string) SIEMEvent {
	detail := map[string]any{
		"user_id":  userID,
		"pii_type": piiType,
		"action":   "masked",
		"path":     path,
	}
	return SIEMEvent{
		TenantID:  tenantID,
		EventType: "pii_in_response",
		Payload: map[string]any{
			"source":      "totra",
			"tenant_id":   tenantID,
			"event_type":  "pii_in_response",
			"occurred_at": strings.TrimSpace(time.Now().UTC().Format(time.RFC3339)),
			"detail":      detail,
		},
	}
}
