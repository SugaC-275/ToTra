package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/gateway/middleware"
)

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
