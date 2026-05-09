package middleware

import (
	"regexp"

	"github.com/gofiber/fiber/v2"
)

var piiPatterns = []*piiRule{
	{name: "china_phone", re: regexp.MustCompile(`1[3-9]\d{9}`)},
	{name: "china_id_card", re: regexp.MustCompile(`\b\d{17}[\dXx]\b`)},
	{name: "credit_card", re: regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`)},
}

type piiRule struct {
	name string
	re   *regexp.Regexp
}

func NewPIIMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := string(c.Body())
		for _, rule := range piiPatterns {
			if rule.re.MatchString(body) {
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
