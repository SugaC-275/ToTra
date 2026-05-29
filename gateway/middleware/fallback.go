package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"log"

	"github.com/gofiber/fiber/v2"
)

// ModelConfig holds the configuration needed by the fallback middleware.
// It mirrors the fields used from storage.ModelConfig without importing that package.
type ModelConfig struct {
	ID       string
	Name     string
	Provider string
	APIKey   string
	BaseURL  string
}

// FallbackLookup is implemented by storage layers that can resolve a fallback chain.
type FallbackLookup interface {
	GetFallbackChain(ctx context.Context, tenantID, primaryModelName string) ([]*ModelConfig, error)
}

// NewFallbackMiddleware returns a Fiber middleware that transparently retries a
// failed proxy request (502/503) using the tenant's configured fallback chain.
//
// Behaviour:
//   - Calls c.Next() to let the upstream proxy handler run.
//   - If the response status is 502 or 503, queries FallbackLookup for the
//     ordered fallback chain for the requested primary model.
//   - Iterates the chain in order; for each fallback it patches the request body
//     with the fallback model name, re-invokes the supplied proxyHandler, and
//     stops as soon as the response status is below 500.
//   - Sets c.Locals("fallback_used", true) and c.Locals("fallback_model_name", name)
//     for the last attempted fallback so downstream logging can record it.
//
// proxyHandler must be the same fiber.Handler that handles the primary request
// (e.g. makeProxyHandler's return value in main.go).
func NewFallbackMiddleware(lookup FallbackLookup, proxyHandler fiber.Handler) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Run the primary proxy handler.
		if err := c.Next(); err != nil {
			return err
		}

		status := c.Response().StatusCode()
		if status != fiber.StatusBadGateway && status != fiber.StatusServiceUnavailable {
			// Primary request succeeded or failed with a non-retryable code.
			return nil
		}

		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return nil
		}

		// Extract the model name from the original request body.
		originalBody := c.Body()
		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(originalBody, &reqBody); err != nil || reqBody.Model == "" {
			return nil
		}

		// Look up the full fallback chain for this tenant + primary model.
		chain, err := lookup.GetFallbackChain(c.Context(), user.TenantID, reqBody.Model)
		if err != nil {
			log.Printf("fallback middleware: chain lookup error for tenant=%s model=%s: %v", user.TenantID, reqBody.Model, err)
			return nil
		}
		if len(chain) == 0 {
			// No fallback configured — return the original 502/503.
			return nil
		}

		for _, fallback := range chain {
			// Patch the request body: replace "model" with the fallback model name.
			patchedBody, err := patchModel(originalBody, fallback.Name)
			if err != nil {
				log.Printf("fallback middleware: patch body: %v", err)
				return nil
			}

			// Swap in the patched body so the proxy handler sees it.
			c.Request().SetBody(patchedBody)

			// Mark fallback in locals before re-invoking the proxy.
			c.Locals("fallback_used", true)
			c.Locals("fallback_model_name", fallback.Name)

			// Re-run the proxy handler with this fallback model.
			if err := proxyHandler(c); err != nil {
				return err
			}

			// If the response is now successful, stop iterating.
			if c.Response().StatusCode() < fiber.StatusInternalServerError {
				break
			}
		}

		return nil
	}
}

// patchModel replaces the "model" field in a JSON object with newModel.
func patchModel(body []byte, newModel string) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&raw); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(newModel)
	if err != nil {
		return nil, err
	}
	raw["model"] = encoded
	return json.Marshal(raw)
}
