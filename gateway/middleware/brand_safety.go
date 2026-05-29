package middleware

import (
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// BrandSafetyConfig defines the keyword blocklist for a tenant (or all tenants
// when TenantID is empty).
type BrandSafetyConfig struct {
	BlockedKeywords []string // matched case-insensitively against request body
	TenantID        string   // empty = applies to all tenants
}

// NewBrandSafetyMiddleware returns a Fiber middleware that blocks requests
// whose body contains any configured keyword. It sets X-Brand-Safety-Check:
// passed on clean requests and returns HTTP 400 with a structured error body
// on violations.
func NewBrandSafetyMiddleware(configs []BrandSafetyConfig) fiber.Handler {
	// Pre-lowercase all keywords once at startup.
	type compiledConfig struct {
		keywords []string
		tenantID string
	}
	compiled := make([]compiledConfig, 0, len(configs))
	for _, cfg := range configs {
		lc := make([]string, 0, len(cfg.BlockedKeywords))
		for _, kw := range cfg.BlockedKeywords {
			if kw != "" {
				lc = append(lc, strings.ToLower(kw))
			}
		}
		compiled = append(compiled, compiledConfig{keywords: lc, tenantID: cfg.TenantID})
	}

	return func(c *fiber.Ctx) error {
		tenantID := ""
		if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
			tenantID = user.TenantID
		}

		bodyLower := strings.ToLower(string(c.Body()))

		for _, cfg := range compiled {
			// Skip configs scoped to a different tenant.
			if cfg.tenantID != "" && cfg.tenantID != tenantID {
				continue
			}
			for _, kw := range cfg.keywords {
				if strings.Contains(bodyLower, kw) {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
						"error": fiber.Map{
							"message":          "Request blocked by brand safety policy",
							"type":             "brand_safety_violation",
							"matched_keyword":  kw,
						},
					})
				}
			}
		}

		c.Set("X-Brand-Safety-Check", "passed")
		return c.Next()
	}
}

// LoadBrandSafetyConfig reads the BRAND_SAFETY_KEYWORDS environment variable.
// Format: comma-separated keywords, e.g. "competitor1,competitor2,badword".
// Returns nil when the variable is absent or empty.
func LoadBrandSafetyConfig() []BrandSafetyConfig {
	raw := strings.TrimSpace(os.Getenv("BRAND_SAFETY_KEYWORDS"))
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	keywords := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			keywords = append(keywords, p)
		}
	}
	if len(keywords) == 0 {
		return nil
	}

	return []BrandSafetyConfig{
		{BlockedKeywords: keywords}, // TenantID="" applies to all tenants
	}
}
