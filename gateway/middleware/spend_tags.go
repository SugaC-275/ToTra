package middleware

import (
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// NewSpendTagsMiddleware extracts spend tags from:
//  1. X-ToTra-Tags header: "feature-x,team-backend"  (canonical)
//  2. X-Tags header: same format                     (backwards-compatible alias)
//  3. Request body metadata.tags: ["feature-x","team-backend"]
//
// Tags are stored in c.Locals("spend_tags") for later pickup by the usage recorder
// and request logger. Tags flow into the audit trail and cost reports.
func NewSpendTagsMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		tags := extractTags(c)
		if len(tags) > 0 {
			c.Locals("spend_tags", tags)
		}
		return c.Next()
	}
}

func extractTags(c *fiber.Ctx) []string {
	// Prefer canonical header; fall back to short alias.
	rawHeader := c.Get("X-ToTra-Tags")
	if rawHeader == "" {
		rawHeader = c.Get("X-Tags")
	}

	if rawHeader != "" {
		var tags []string
		for _, t := range strings.Split(rawHeader, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		return dedup(tags)
	}

	// Fall back to JSON body metadata.tags.
	var body struct {
		Metadata struct {
			Tags []string `json:"tags"`
		} `json:"metadata"`
	}
	if json.Unmarshal(c.Body(), &body) == nil && len(body.Metadata.Tags) > 0 {
		return dedup(body.Metadata.Tags)
	}

	return nil
}

func dedup(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := in[:0]
	for _, t := range in {
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}
