package middleware

import (
	"encoding/json"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/tokenizer"
)

// modelContextLimits maps known model names to their context window sizes in tokens.
var modelContextLimits = map[string]int{
	"gpt-4o":             128_000,
	"gpt-4o-mini":        128_000,
	"gpt-4-turbo":        128_000,
	"gpt-4":              8_192,
	"claude-3-5-sonnet":  200_000,
	"claude-3-5-haiku":   200_000,
	"claude-3-opus":      200_000,
	"claude-3-haiku":     200_000,
	"claude-opus-4-7":    200_000,
	"claude-sonnet-4-6":  200_000,
	"gemini-1.5-pro":     1_048_576,
	"gemini-1.5-flash":   1_048_576,
	"gemini-2.0-flash":   1_048_576,
}

// contextFallbackChain lists candidate fallback models ordered by context window
// size ascending. When the current model's window is too small, the middleware
// selects the first entry in this chain whose limit exceeds the current model's.
var contextFallbackChain = []struct {
	Model string
	Limit int
}{
	{"gpt-4o", 128_000},
	{"claude-3-5-sonnet", 200_000},
	{"gemini-1.5-pro", 1_048_576},
}

const contextWindowThreshold = 0.90

// NewContextWindowFallbackMiddleware returns a Fiber middleware that estimates
// the token count of the incoming request before it reaches the proxy. If the
// estimate exceeds 90 % of the current model's context window limit, the request
// body is transparently patched to use the next larger model from
// contextFallbackChain.
//
// Response headers set on switch:
//
//	X-Context-Fallback-From – original model name
//	X-Context-Fallback-To   – replacement model name
func NewContextWindowFallbackMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()

		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &reqBody); err != nil || reqBody.Model == "" {
			return c.Next()
		}

		currentLimit, known := modelContextLimits[reqBody.Model]
		if !known {
			// Unknown model — do not interfere.
			return c.Next()
		}

		estimated := tokenizer.EstimateTokensFromBody(body)
		threshold := int(float64(currentLimit) * contextWindowThreshold)
		if estimated <= threshold {
			return c.Next()
		}

		// Find the first chain entry whose limit is strictly greater than the
		// current model's limit so we actually upgrade, not stay the same.
		newModel := ""
		for _, candidate := range contextFallbackChain {
			if candidate.Limit > currentLimit {
				newModel = candidate.Model
				break
			}
		}
		if newModel == "" {
			// Already at the largest available context — proceed as-is.
			return c.Next()
		}

		patched, err := patchModel(body, newModel)
		if err != nil {
			// Patching failed — let the request through unchanged.
			return c.Next()
		}

		c.Request().SetBody(patched)
		c.Set("X-Context-Fallback-From", reqBody.Model)
		c.Set("X-Context-Fallback-To", newModel)

		return c.Next()
	}
}
