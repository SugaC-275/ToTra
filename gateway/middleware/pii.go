package middleware

import (
	"regexp"

	"github.com/gofiber/fiber/v2"
)

type piiRule struct {
	name string
	re   *regexp.Regexp
}

var piiPatterns = []*piiRule{
	{name: "china_phone", re: regexp.MustCompile(`1[3-9]\d{9}`)},
	{name: "china_id_card", re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "credit_card", re: regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
	{name: "email", re: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)},
	{name: "bank_account", re: regexp.MustCompile(`\b\d{16,19}\b`)},
}

// ViolationRecorder is the interface PIIMiddleware uses to record violations.
// This avoids an import cycle with the storage package.
type ViolationRecorder interface {
	RecordViolation(tenantID, userID, piiType, action, requestPath string)
}

// NewPIIMiddleware creates a PII detection middleware.
// recorder may be nil (no persistence). tenantID is used as a fallback when
// no authenticated user is present in the context; pass "" to always pull from
// the "user" local set by NewAuthMiddleware.
func NewPIIMiddleware(recorder ViolationRecorder, tenantID string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
				if recorder != nil {
					tid := tenantID
					uid := ""
					if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
						tid = user.TenantID
						uid = user.UserID
					}
					recorder.RecordViolation(tid, uid, rule.name, "blocked", c.Path())
				}
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: potential PII detected (" + rule.name + ")",
						"type":    "pii_blocked",
					},
				})
			}
		}
		return c.Next()
	}
}
