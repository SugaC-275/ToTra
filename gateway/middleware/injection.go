package middleware

import (
	"os"
	"regexp"
	"time"

	"github.com/gofiber/fiber/v2"
)

// injectionPatterns matches well-known prompt injection techniques.
var injectionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(?:all\s+)?(?:previous|above|prior)\s+instructions?`),
	regexp.MustCompile(`(?i)forget\s+(?:everything|all|your|the\s+(?:previous|above))`),
	regexp.MustCompile(`(?i)(?:you\s+are\s+now|pretend\s+(?:to\s+be|you\s+are)|act\s+as\s+(?:a\s+)?(?:DAN|jailbreak|unrestricted|evil\s+AI))`),
	regexp.MustCompile(`(?i)\bDAN\b.*(?:do\s+anything|no\s+restrictions?|ignore\s+(?:rules?|guidelines?|policies?))`),
	regexp.MustCompile(`(?i)(?:new\s+system\s+prompt|override\s+(?:system\s+)?instructions?|system\s*:\s*you\s+(?:must|will|shall))`),
	regexp.MustCompile(`(?i)jailbreak(?:ed|ing|\s+mode)?`),
	// CRLF injection trying to inject a new role turn
	regexp.MustCompile(`(?i)\\n\s*(?:system|assistant)\s*:\\n`),
	// Prompt leaking / extraction attacks
	regexp.MustCompile(`(?i)(?:repeat|print|output|reveal|show)\s+(?:your\s+)?(?:system\s+prompt|initial\s+instructions?|original\s+instructions?|confidential\s+instructions?)`),
	regexp.MustCompile(`(?i)(?:from\s+now\s+on|starting\s+now|henceforth)\s+you\s+(?:will|must|shall|are)\s+ignore`),
	// Base64/encoded instruction bypass
	regexp.MustCompile(`(?i)(?:base64|rot13|hex)\s+(?:encoded?|decode[d]?)\s+(?:instructions?|prompt|command)`),
	// Many-shot jailbreak / grandma exploit
	regexp.MustCompile(`(?i)(?:my\s+grandma\s+used\s+to|roleplay\s+as|pretend\s+this\s+is\s+fiction)\s+.*(?:instructions?|steps?\s+to|how\s+to)`),
}

// NewInjectionMiddleware blocks requests matching known prompt injection patterns.
// Set env INJECTION_DETECTION=off to disable (useful in dev).
func NewInjectionMiddleware(siemChan chan<- SIEMEvent) fiber.Handler {
	if os.Getenv("INJECTION_DETECTION") == "off" {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	return func(c *fiber.Ctx) error {
		body := string(c.Body())

		for _, re := range injectionPatterns {
			if re.MatchString(body) {
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
						EventType: "prompt_injection",
						Payload: map[string]any{
							"source":      "totra",
							"tenant_id":   tid,
							"event_type":  "prompt_injection",
							"occurred_at": time.Now().UTC().Format(time.RFC3339),
							"detail": map[string]any{
								"user_id": uid,
								"action":  "blocked",
								"path":    c.Path(),
							},
						},
					}:
					default:
					}
				}

				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: potential prompt injection detected",
						"type":    "injection_blocked",
					},
				})
			}
		}
		return c.Next()
	}
}
