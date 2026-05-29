package middleware

import (
	"context"
	"encoding/json"
	"math/rand/v2"

	"github.com/gofiber/fiber/v2"
)

// ABTestEntry mirrors storage.ABTest without importing the storage package,
// avoiding an import cycle (storage → middleware → storage).
type ABTestEntry struct {
	ID        string
	Name      string
	ModelA    string
	ModelB    string
	SplitPctB int // percentage of traffic sent to ModelB
}

// ABTestQuerier is implemented by storage.ABTestStore.
type ABTestQuerier interface {
	GetActiveForModel(ctx context.Context, tenantID, model string) (*ABTestEntry, error)
}

// NewABTestMiddleware routes a configurable percentage of traffic for a model to
// an alternate model. The decision is stored in X-AB-Test-* headers for observability.
func NewABTestMiddleware(store ABTestQuerier) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if store == nil {
			return c.Next()
		}

		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}

		var req struct{ Model string `json:"model"` }
		if err := json.Unmarshal(c.Body(), &req); err != nil || req.Model == "" {
			return c.Next()
		}

		test, err := store.GetActiveForModel(c.Context(), user.TenantID, req.Model)
		if err != nil || test == nil {
			return c.Next()
		}

		roll := rand.IntN(100)
		var chosen string
		if roll < test.SplitPctB {
			chosen = test.ModelB
		} else {
			chosen = test.ModelA
		}

		if chosen == req.Model {
			c.Set("X-AB-Test", test.Name)
			c.Set("X-AB-Test-Variant", "A")
			return c.Next()
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(c.Body(), &raw); err != nil {
			return c.Next()
		}
		newModel, _ := json.Marshal(chosen)
		raw["model"] = newModel
		newBody, err := json.Marshal(raw)
		if err != nil {
			return c.Next()
		}
		c.Request().SetBody(newBody)
		c.Set("X-AB-Test", test.Name)
		c.Set("X-AB-Test-Variant", "B")
		c.Set("X-AB-Test-Original", req.Model)
		c.Set("X-AB-Test-Chosen", chosen)
		return c.Next()
	}
}
