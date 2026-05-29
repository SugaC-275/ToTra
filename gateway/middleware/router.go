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

// LatencyQuerier is the read side of LatencyStore that router requires.
type LatencyQuerier interface {
	P95Latency(ctx context.Context, model string) (float64, error)
	RecordLatency(ctx context.Context, model string, latencyMs int64) error
}

// InflightCounter is the read/write side of InflightStore that router requires.
type InflightCounter interface {
	Increment(ctx context.Context, model string) error
	Decrement(ctx context.Context, model string) error
	Count(ctx context.Context, model string) (int64, error)
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

// RoutingSignals aggregates all routing dimensions on a 0–100 scale.
type RoutingSignals struct {
	ComplexityScore int
	P95LatencyMs    float64
	InflightCount   int64
	CostPerMToken   float64
}

// TenantRoutingPolicy holds per-tenant weights and governance constraints.
// Weights are expected to sum to 1.0; the router does not enforce this.
type TenantRoutingPolicy struct {
	WComplexity    float64 // default 0.4
	WLatency       float64 // default 0.3
	WCost          float64 // default 0.3
	SovereignModel string  // overrides model when PII is detected
	EUOnly         bool    // only route to EU endpoints when true
}

var defaultPolicy = TenantRoutingPolicy{
	WComplexity: 0.4,
	WLatency:    0.3,
	WCost:       0.3,
}

// MultiSignalScore returns a 0–1 composite score where higher means "prefer a
// lighter/cheaper model". Each signal is normalised independently before being
// combined with the policy weights.
//
// Normalisation caps:
//   - Complexity: 100 pts → 1.0
//   - P95 latency: 2000 ms → 1.0  (above this the model is considered slow)
//   - Cost: 10 $/M tokens → 1.0
func MultiSignalScore(signals RoutingSignals, policy TenantRoutingPolicy) float64 {
	normComplexity := clamp01(float64(signals.ComplexityScore) / 100.0)
	normLatency := clamp01(signals.P95LatencyMs / 2000.0)
	normCost := clamp01(signals.CostPerMToken / 10.0)

	// InflightCount feeds into the latency weight: high concurrency inflates
	// effective latency pressure proportionally.
	if signals.InflightCount > 0 {
		concurrencyPressure := clamp01(float64(signals.InflightCount) / 50.0)
		normLatency = clamp01(normLatency + concurrencyPressure*0.2)
	}

	return policy.WComplexity*normComplexity +
		policy.WLatency*normLatency +
		policy.WCost*normCost
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// RouterOptions configures optional dependencies for NewAutoRouterMiddleware.
// Nil fields cause the router to degrade gracefully to the prior behaviour.
type RouterOptions struct {
	Latency  LatencyQuerier
	Inflight InflightCounter
}

func NewAutoRouterMiddleware(rec RoutingRecorder, opts ...RouterOptions) fiber.Handler {
	var opt RouterOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	return func(c *fiber.Ctx) error {
		body := c.Body()
		score := ComplexityScore(body)

		var reqBody struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &reqBody); err != nil || reqBody.Model == "" {
			return c.Next()
		}

		threshold := routerThreshold()

		// Governance: budget pressure lowers the threshold (more aggressive downgrade).
		if budgetPct, ok := c.Locals("budget_remaining_pct").(float64); ok && budgetPct < 20 {
			threshold -= 20
			if threshold < 0 {
				threshold = 0
			}
		}

		// Governance: PII forces sovereign model regardless of complexity.
		if piiDetected, ok := c.Locals("pii_detected").(bool); ok && piiDetected {
			policy := tenantPolicy(c)
			if policy.SovereignModel != "" {
				return sovereignRoute(c, body, reqBody.Model, policy.SovereignModel, rec, score)
			}
		}

		// Healthcare: PHI detected → must route to BAA-compliant model only.
		// This is a hard block (not a soft downgrade) — 451 if no BAA model available.
		if phiDetected, ok := c.Locals("phi_detected").(bool); ok && phiDetected {
			// The model lookup happens later in the proxy handler, but we need to
			// signal that BAA enforcement is required.
			c.Locals("require_baa", true)
		}

		// Financial: PFI detected → signal proxy handler to enforce FINRA compliance.
		if pfiDetected, ok := c.Locals("pfi_detected").(bool); ok && pfiDetected {
			c.Locals("require_finra", true)
		}

		// Multi-signal routing when stores are wired in.
		effectiveScore := score
		if opt.Latency != nil && opt.Inflight != nil {
			signals := gatherSignals(c.Context(), reqBody.Model, score, opt)
			policy := tenantPolicy(c)
			composite := MultiSignalScore(signals, policy)
			// Map composite (0–1) back onto the 0–100 scale so the same threshold
			// applies uniformly regardless of whether stores are present.
			effectiveScore = int(composite * 100)
		}

		if effectiveScore >= threshold {
			// Track in-flight even for requests that are not downgraded.
			trackInflight(c, body, reqBody.Model, opt, rec, score)
			return c.Next()
		}

		cheaper, ok := premiumToStandard[reqBody.Model]
		if !ok {
			trackInflight(c, body, reqBody.Model, opt, rec, score)
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

		if opt.Inflight != nil {
			_ = opt.Inflight.Increment(c.Context(), cheaper)
			defer func() {
				_ = opt.Inflight.Decrement(c.Context(), cheaper)
				if opt.Latency != nil {
					// Latency is measured by the handler that processes the request;
					// the router cannot know actual upstream latency at this point, so
					// we skip recording here. Callers that wrap the handler can call
					// RecordLatency directly.
					_ = opt.Latency
				}
			}()
		}

		return c.Next()
	}
}

// sovereignRoute replaces the request model with the tenant's sovereign model
// and proceeds without downgrade logic.
func sovereignRoute(
	c *fiber.Ctx,
	body []byte,
	originalModel, sovereignModel string,
	rec RoutingRecorder,
	score int,
) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return c.Next()
	}
	newModel, _ := json.Marshal(sovereignModel)
	raw["model"] = newModel
	newBody, err := json.Marshal(raw)
	if err != nil {
		return c.Next()
	}
	c.Request().SetBody(newBody)
	c.Set("X-Routed-From", originalModel)
	c.Set("X-Routed-To", sovereignModel)
	c.Set("X-Sovereign-Routed", "true")

	if rec != nil {
		user, _ := c.Locals("user").(*UserInfo)
		uid, tid := "", ""
		if user != nil {
			uid, tid = user.UserID, user.TenantID
		}
		rec.Record(c.Context(), RoutingEvent{ //nolint:errcheck
			TenantID: tid, UserID: uid,
			OriginalModel: originalModel, RoutedModel: sovereignModel,
			BodyLen: len(body), ComplexityScore: score,
		})
	}
	return c.Next()
}

// gatherSignals fetches live P95 latency and inflight count for model.
// On any error the signal is left at its zero value so routing degrades
// gracefully rather than failing the request.
func gatherSignals(ctx context.Context, model string, complexityScore int, opt RouterOptions) RoutingSignals {
	sig := RoutingSignals{ComplexityScore: complexityScore}
	if p95, err := opt.Latency.P95Latency(ctx, model); err == nil {
		sig.P95LatencyMs = p95
	}
	if count, err := opt.Inflight.Count(ctx, model); err == nil {
		sig.InflightCount = count
	}
	return sig
}

// trackInflight starts an inflight counter for requests that are not
// downgraded (pass-through). The counter is decremented in a deferred call.
func trackInflight(c *fiber.Ctx, _ []byte, model string, opt RouterOptions, _ RoutingRecorder, _ int) {
	if opt.Inflight == nil {
		return
	}
	_ = opt.Inflight.Increment(c.Context(), model)
	c.Locals("_inflight_model", model)
}

// tenantPolicy returns the routing policy for the current request's tenant.
// Falls back to defaultPolicy when no tenant context is present.
func tenantPolicy(c *fiber.Ctx) TenantRoutingPolicy {
	if p, ok := c.Locals("tenant_routing_policy").(TenantRoutingPolicy); ok {
		return p
	}
	return defaultPolicy
}
