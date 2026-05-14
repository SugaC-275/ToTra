package middleware

import (
	"context"
	"log"
	"regexp"

	"github.com/gofiber/fiber/v2"
)

type PolicyRule struct {
	Name    string `json:"name"`
	Pattern string `json:"pattern"`
	Action  string `json:"action"`
}

type PolicyRuleGetter interface {
	GetRules(ctx context.Context, tenantID string) ([]*PolicyRule, error)
}

func NewPolicyMiddleware(store PolicyRuleGetter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}
		rules, err := store.GetRules(c.Context(), user.TenantID)
		if err != nil {
			log.Printf("policy middleware: %v", err)
			return c.Next()
		}
		body := string(c.Body())
		for _, rule := range rules {
			re, err := regexp.Compile(rule.Pattern)
			if err != nil {
				continue
			}
			if re.MatchString(body) {
				if rule.Action == "block" {
					return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
						"error": fiber.Map{
							"message": "request blocked by policy rule: " + rule.Name,
							"type":    "policy_blocked",
						},
					})
				}
				log.Printf("policy rule matched (log): tenant=%s rule=%s", user.TenantID, rule.Name)
			}
		}
		return c.Next()
	}
}
