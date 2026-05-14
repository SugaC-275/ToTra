package middleware

import (
	"context"
	"encoding/json"

	"github.com/gofiber/fiber/v2"
)

type RoutingEvent struct {
	TenantID, UserID, OriginalModel, RoutedModel string
	BodyLen int
}

type RoutingRecorder interface {
	Record(ctx context.Context, e RoutingEvent)
}

const complexityThreshold = 400

var premiumToStandard = map[string]string{
	"claude-opus-4-7": "claude-sonnet-4-6",
	"claude-opus-4-5": "claude-sonnet-4-6",
	"gpt-4-turbo":     "gpt-4o-mini",
	"o1":              "gpt-4o-mini",
	"o1-preview":      "gpt-4o-mini",
}

func NewAutoRouterMiddleware(rec RoutingRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()
		if len(body) >= complexityThreshold {
			return c.Next()
		}
		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &reqBody); err != nil || reqBody.Model == "" {
			return c.Next()
		}
		cheaper, ok := premiumToStandard[reqBody.Model]
		if !ok {
			return c.Next()
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			return c.Next()
		}
		newModel, _ := json.Marshal(cheaper)
		raw["model"] = newModel
		newBody, err := json.Marshal(raw)
		if err != nil {
			return c.Next()
		}
		c.Request().SetBody(newBody)
		c.Set("X-Routed-From", reqBody.Model)
		c.Set("X-Routed-To", cheaper)
		if rec != nil {
			user, _ := c.Locals("user").(*UserInfo)
			uid, tid := "", ""
			if user != nil {
				uid, tid = user.UserID, user.TenantID
			}
			rec.Record(c.Context(), RoutingEvent{TenantID: tid, UserID: uid, OriginalModel: reqBody.Model, RoutedModel: cheaper, BodyLen: len(body)})
		}
		return c.Next()
	}
}
