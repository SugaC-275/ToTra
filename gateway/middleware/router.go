package middleware

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type RoutingEvent struct {
	TenantID, UserID, OriginalModel, RoutedModel string
	BodyLen         int
	ComplexityScore int
}

type RoutingRecorder interface {
	Record(ctx context.Context, e RoutingEvent) (int64, error)
}

var premiumToStandard = map[string]string{
	"claude-opus-4-7": "claude-sonnet-4-6",
	"claude-opus-4-5": "claude-sonnet-4-6",
	"gpt-4-turbo":     "gpt-4o-mini",
	"o1":              "gpt-4o-mini",
	"o1-preview":      "gpt-4o-mini",
}

var complexKeywords = []string{
	"分析", "推理", "审查", "对比", "架构", "重构",
	"evaluate", "analyze", "compare", "refactor",
}

// ComplexityScore returns a 0–100 complexity estimate for the given request body.
// Exported so it can be tested independently of the middleware.
func ComplexityScore(body []byte) int {
	score := 0

	// Signal 1: body length (40 pts max)
	lenRatio := float64(len(body)) / 4000.0
	if lenRatio > 1.0 {
		lenRatio = 1.0
	}
	score += int(40 * lenRatio)

	var req struct {
		Messages []struct {
			Role string `json:"role"`
		} `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil {
		return score // length signal only on parse failure
	}

	// Signal 2: message count (20 pts max)
	msgRatio := float64(len(req.Messages)) / 10.0
	if msgRatio > 1.0 {
		msgRatio = 1.0
	}
	score += int(20 * msgRatio)

	// Signal 3: system prompt (20 pts)
	for _, m := range req.Messages {
		if m.Role == "system" {
			score += 20
			break
		}
	}

	// Signal 4: tool_calls or tools field present (10 pts)
	bodyStr := string(body)
	if strings.Contains(bodyStr, `"tool_calls"`) || strings.Contains(bodyStr, `"tools"`) {
		score += 10
	}

	// Signal 5: complex keyword (10 pts)
	for _, kw := range complexKeywords {
		if strings.Contains(bodyStr, kw) {
			score += 10
			break
		}
	}

	return score
}

func routerThreshold() int {
	if v := os.Getenv("ROUTER_COMPLEXITY_THRESHOLD"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	return 50
}

func NewAutoRouterMiddleware(rec RoutingRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		body := c.Body()
		score := ComplexityScore(body)
		if score >= routerThreshold() {
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
			id, err := rec.Record(c.Context(), RoutingEvent{
				TenantID: tid, UserID: uid,
				OriginalModel: reqBody.Model, RoutedModel: cheaper,
				BodyLen: len(body), ComplexityScore: score,
			})
			if err == nil && id > 0 {
				c.Locals("routing_event_id", id)
				c.Locals("original_model_name", reqBody.Model)
			}
		}
		return c.Next()
	}
}
