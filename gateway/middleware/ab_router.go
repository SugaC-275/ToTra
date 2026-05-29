package middleware

import (
	"context"
	"encoding/json"
	"log"
	"math/rand/v2"

	"github.com/gofiber/fiber/v2"
)

// ABRoute holds the data needed by the A/B router middleware.
// Mirrors storage.ABRoute without importing the storage package.
type ABRoute struct {
	ID        string
	Name      string
	ModelA    string
	ModelB    string
	PctB      int
	EvalSuite *string
}

// ABRouteQuerier is implemented by storage.ABRouteStore.
type ABRouteQuerier interface {
	GetActiveRoute(ctx context.Context, tenantID, modelName string) (*ABRoute, error)
}

// ABEvalSubmitter is an optional callback that asynchronously submits an
// inference request+response pair to an eval suite. It must not block.
type ABEvalSubmitter interface {
	SubmitAsync(tenantID, suiteID, model, variant string, requestBody, responseBody []byte)
}

// NewABRouterMiddleware checks whether there is an active A/B route for the
// resolved model and, when one exists, may swap the model to model_b before
// the request reaches the upstream proxy.
//
// After the upstream returns, if the route has an eval_suite, it asynchronously
// submits the response for the variant that actually ran.
//
// Locals set by this middleware:
//   - "ab_variant"  — "a" or "b"
//   - "ab_route_id" — the route UUID
func NewABRouterMiddleware(store ABRouteQuerier, evalSubmitter ABEvalSubmitter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if store == nil {
			return c.Next()
		}

		user, ok := c.Locals("user").(*UserInfo)
		if !ok || user == nil {
			return c.Next()
		}

		var req struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(c.Body(), &req); err != nil || req.Model == "" {
			return c.Next()
		}

		route, err := store.GetActiveRoute(c.Context(), user.TenantID, req.Model)
		if err != nil {
			log.Printf("ab_router: lookup error tenant=%s model=%s: %v", user.TenantID, req.Model, err)
			return c.Next()
		}
		if route == nil {
			return c.Next()
		}

		// Capture original body before any mutation.
		originalBody := make([]byte, len(c.Body()))
		copy(originalBody, c.Body())

		// Decide variant.
		variant := "a"
		if rand.IntN(100) < route.PctB {
			variant = "b"
		}

		c.Locals("ab_variant", variant)
		c.Locals("ab_route_id", route.ID)

		if variant == "b" {
			patched, err := patchModel(c.Body(), route.ModelB)
			if err != nil {
				log.Printf("ab_router: patch body error: %v", err)
				return c.Next()
			}
			c.Request().SetBody(patched)
		}

		// Run the rest of the chain (ultimately the proxy handler).
		if err := c.Next(); err != nil {
			return err
		}

		// After response: asynchronously submit to eval suite if configured.
		if route.EvalSuite != nil && *route.EvalSuite != "" && evalSubmitter != nil {
			respBody := make([]byte, len(c.Response().Body()))
			copy(respBody, c.Response().Body())

			actualModel := route.ModelA
			if variant == "b" {
				actualModel = route.ModelB
			}
			evalSubmitter.SubmitAsync(user.TenantID, *route.EvalSuite, actualModel, variant, originalBody, respBody)
		}

		return nil
	}
}
