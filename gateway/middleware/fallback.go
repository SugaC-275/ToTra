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

// FallbackLookup is implemented by storage layers that can resolve a fallback model.
type FallbackLookup interface {
	GetFallbackModel(ctx context.Context, tenantID, primaryModelName string) (*ModelConfig, error)
}

// NewFallbackMiddleware returns a Fiber middleware that transparently retries a
// failed proxy request (502/503) using the tenant's configured fallback model.
//
// Behaviour:
//   - Calls c.Next() to let the upstream proxy handler run.
//   - If the response status is 502 or 503, queries FallbackLookup for a
//     fallback model associated with the requested primary model.
//   - If a fallback exists, it patches the request body with the fallback model
//     name, re-invokes the supplied proxyHandler, and returns its response.
//   - Sets c.Locals("fallback_used", true) and c.Locals("fallback_model_name", name)
//     when a fallback is actually used, so downstream logging can record it.
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

		// Look up the fallback model for this tenant + primary model.
		fallback, err := lookup.GetFallbackModel(c.Context(), user.TenantID, reqBody.Model)
		if err != nil {
			log.Printf("fallback middleware: lookup error for tenant=%s model=%s: %v", user.TenantID, reqBody.Model, err)
			return nil
		}
		if fallback == nil {
			// No fallback configured — return the original 502/503.
			return nil
		}

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

		// Re-run the proxy handler with the fallback model.
		return proxyHandler(c)
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
