package middleware

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

// NewModelAliasMiddleware silently rewrites the model field in the request body
// when the authenticated user has a virtual key alias configured.
// Example: key A requests "gpt-4" → transparently routed to "gpt-4o-mini".
func NewModelAliasMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil || len(user.ModelAliases) == 0 {
			return c.Next()
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(c.Body(), &raw); err != nil {
			return c.Next()
		}

		var modelName string
		if err := json.Unmarshal(raw["model"], &modelName); err != nil || modelName == "" {
			return c.Next()
		}

		// Check for exact match first, then wildcard override from virtual keys.
		alias, ok := user.ModelAliases[modelName]
		if (!ok || alias == "") && user.ModelAliases["*"] != "" {
			alias = user.ModelAliases["*"]
			ok = true
		}
		if !ok || alias == "" {
			return c.Next()
		}

		newModel, _ := json.Marshal(alias)
		raw["model"] = newModel
		newBody, err := json.Marshal(raw)
		if err != nil {
			return c.Next()
		}

		c.Request().SetBody(newBody)
		c.Set("X-Model-Alias-From", modelName)
		c.Set("X-Model-Alias-To", alias)
		return c.Next()
	}
}
