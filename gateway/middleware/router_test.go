package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

// stubLatency implements LatencyQuerier with fixed responses.
type stubLatency struct {
	p95   float64
	err   error
	calls []string
}

func (s *stubLatency) P95Latency(_ context.Context, model string) (float64, error) {
	s.calls = append(s.calls, model)
	return s.p95, s.err
}

func (s *stubLatency) RecordLatency(_ context.Context, _ string, _ int64) error {
	return nil
}

// stubInflight implements InflightCounter with an in-memory counter.
type stubInflight struct {
	count int64
	err   error
}

func (s *stubInflight) Increment(_ context.Context, _ string) error { s.count++; return s.err }
func (s *stubInflight) Decrement(_ context.Context, _ string) error {
	if s.count > 0 {
		s.count--
	}
	return s.err
}
func (s *stubInflight) Count(_ context.Context, _ string) (int64, error) { return s.count, s.err }

type captureRecorder struct{ events []middleware.RoutingEvent }

func (r *captureRecorder) Record(_ context.Context, e middleware.RoutingEvent) (int64, error) {
	r.events = append(r.events, e)
	return 1, nil
}

func setupRouterApp(rec middleware.RoutingRecorder) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error { return c.SendString(string(c.Body())) })
	return app
}

func TestAutoRouter_PremiumModel_ShortBody_Downgraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 1, len(rec.events))
	assert.Equal(t, "claude-opus-4-7", rec.events[0].OriginalModel)
	assert.Equal(t, "claude-sonnet-4-6", rec.events[0].RoutedModel)
}

func TestAutoRouter_StandardModel_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	body := `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events))
}

func TestAutoRouter_PremiumModel_HighComplexity_NotDowngraded(t *testing.T) {
	rec := &captureRecorder{}
	app := setupRouterApp(rec)
	// system prompt (20) + 10 messages (20) + "analyze" (10) + ~400 bytes (4) = 54 → above threshold
	msgs := ""
	for i := 0; i < 10; i++ {
		msgs += `{"role":"user","content":"analyze step ` + strconv.Itoa(i) + `"},`
	}
	msgs += `{"role":"system","content":"system"}`
	body := `{"model":"claude-opus-4-7","messages":[` + msgs + `]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 0, len(rec.events), "high-complexity request must not be downgraded")
}

func TestAutoRouter_NilRecorder_NoPanic(t *testing.T) {
	app := setupRouterApp(nil)
	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestComplexityScore_ShortCleanRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`)
	score := middleware.ComplexityScore(body)
	assert.Less(t, score, 50, "short clean request should score below threshold")
}

func TestComplexityScore_LongBodyMaxesLengthSignal(t *testing.T) {
	longContent := strings.Repeat("x", 4000)
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"` + longContent + `"}]}`)
	score := middleware.ComplexityScore(body)
	assert.GreaterOrEqual(t, score, 40, "body ≥ 4000 bytes should contribute 40 points")
}

func TestComplexityScore_SystemPromptAdds20(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"system","content":"you are helpful"},{"role":"user","content":"hello"}]}`)
	score := middleware.ComplexityScore(body)
	assert.GreaterOrEqual(t, score, 20, "system prompt should add 20 points")
}

func TestComplexityScore_ComplexKeywordAdds10(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"please analyze the situation"}]}`)
	scoreWith := middleware.ComplexityScore(body)
	bodyPlain := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"please describe the situation"}]}`)
	scorePlain := middleware.ComplexityScore(bodyPlain)
	assert.Equal(t, 10, scoreWith-scorePlain, "complex keyword should add exactly 10 points")
}

func TestComplexityScore_HighComplexityAboveThreshold(t *testing.T) {
	// system prompt (20) + 10 messages (20) + "analyze" keyword (10) + moderate length (~5) = 55+
	msgs := ""
	for i := 0; i < 10; i++ {
		msgs += `{"role":"user","content":"analyze step ` + strconv.Itoa(i) + `"},`
	}
	msgs += `{"role":"system","content":"you are a helpful assistant"}`
	body := []byte(`{"model":"claude-opus-4-7","messages":[` + msgs + `]}`)
	score := middleware.ComplexityScore(body)
	assert.GreaterOrEqual(t, score, 50, "high-complexity request should score at or above default threshold")
}

// --- MultiSignalScore unit tests ---

func TestMultiSignalScore_ZeroSignals_ZeroScore(t *testing.T) {
	score := middleware.MultiSignalScore(middleware.RoutingSignals{}, middleware.TenantRoutingPolicy{
		WComplexity: 0.4, WLatency: 0.3, WCost: 0.3,
	})
	assert.Equal(t, 0.0, score)
}

func TestMultiSignalScore_FullComplexity_WeightedCorrectly(t *testing.T) {
	sig := middleware.RoutingSignals{ComplexityScore: 100}
	policy := middleware.TenantRoutingPolicy{WComplexity: 0.4, WLatency: 0.3, WCost: 0.3}
	score := middleware.MultiSignalScore(sig, policy)
	// Only complexity contributes: 1.0 * 0.4 = 0.4
	assert.InDelta(t, 0.4, score, 0.001)
}

func TestMultiSignalScore_HighLatency_IncreasesScore(t *testing.T) {
	sig := middleware.RoutingSignals{P95LatencyMs: 2000}
	policy := middleware.TenantRoutingPolicy{WComplexity: 0.4, WLatency: 0.3, WCost: 0.3}
	score := middleware.MultiSignalScore(sig, policy)
	// Latency at cap (1.0 * 0.3 = 0.3) but concurrency pressure is 0
	assert.InDelta(t, 0.3, score, 0.001)
}

func TestMultiSignalScore_HighInflight_BoostsLatencyComponent(t *testing.T) {
	// High inflight (50) adds full concurrency pressure on top of zero base latency.
	sig := middleware.RoutingSignals{InflightCount: 50}
	policy := middleware.TenantRoutingPolicy{WComplexity: 0.4, WLatency: 0.3, WCost: 0.3}
	scoreHigh := middleware.MultiSignalScore(sig, policy)

	sig2 := middleware.RoutingSignals{InflightCount: 0}
	scoreLow := middleware.MultiSignalScore(sig2, policy)

	assert.Greater(t, scoreHigh, scoreLow, "higher inflight count must yield a higher composite score")
}

func TestMultiSignalScore_ClampedAt1(t *testing.T) {
	sig := middleware.RoutingSignals{
		ComplexityScore: 200,
		P95LatencyMs:    99999,
		InflightCount:   9999,
		CostPerMToken:   999,
	}
	policy := middleware.TenantRoutingPolicy{WComplexity: 0.4, WLatency: 0.3, WCost: 0.3}
	score := middleware.MultiSignalScore(sig, policy)
	assert.LessOrEqual(t, score, 1.0)
}

// --- Governance routing tests ---

func setupRouterAppWithOpts(rec middleware.RoutingRecorder, opts middleware.RouterOptions) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec, opts))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error { return c.SendString(string(c.Body())) })
	return app
}

func TestAutoRouter_PIIDetected_UsesSovereignModel(t *testing.T) {
	rec := &captureRecorder{}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		c.Locals("pii_detected", true)
		c.Locals("tenant_routing_policy", middleware.TenantRoutingPolicy{
			WComplexity:    0.4,
			WLatency:       0.3,
			WCost:          0.3,
			SovereignModel: "local-llama",
		})
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error { return c.SendString(string(c.Body())) })

	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"my SSN is 123-45-6789"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "true", resp.Header.Get("X-Sovereign-Routed"))
	assert.Equal(t, "local-llama", resp.Header.Get("X-Routed-To"))
}

func TestAutoRouter_PIIDetected_NoSovereignModel_NormalRouting(t *testing.T) {
	rec := &captureRecorder{}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		c.Locals("pii_detected", true)
		// no sovereign model set
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error { return c.SendString(string(c.Body())) })

	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Empty(t, resp.Header.Get("X-Sovereign-Routed"))
}

func TestAutoRouter_LowBudget_AggressiveDowngrade(t *testing.T) {
	rec := &captureRecorder{}
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "tid", UserID: "uid"})
		// Budget at 10% → threshold reduced by 20, so even a score of 40 triggers downgrade.
		c.Locals("budget_remaining_pct", float64(10))
		return c.Next()
	})
	app.Use(middleware.NewAutoRouterMiddleware(rec))
	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error { return c.SendString(string(c.Body())) })

	// Body that scores ~40 (system prompt=20 + a couple messages) — normally kept at premium.
	body := `{"model":"claude-opus-4-7","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, "claude-sonnet-4-6", resp.Header.Get("X-Routed-To"),
		"low budget should force downgrade for moderate-complexity requests")
}

func TestAutoRouter_MultiSignal_StoreErrors_DegradeGracefully(t *testing.T) {
	rec := &captureRecorder{}
	opts := middleware.RouterOptions{
		Latency:  &stubLatency{err: errors.New("redis down")},
		Inflight: &stubInflight{err: errors.New("redis down")},
	}
	app := setupRouterAppWithOpts(rec, opts)

	body := `{"model":"claude-opus-4-7","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode, "store errors must not break the request")
}
