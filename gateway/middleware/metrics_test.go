package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/middleware"
)

// freshMetrics returns a GatewayMetrics backed by an isolated registry so
// tests never collide with each other or with the default global registry.
func freshMetrics() (*middleware.GatewayMetrics, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	m := middleware.NewGatewayMetrics(reg)
	return m, reg
}

func counterValue(t *testing.T, reg *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			match := true
			for k, v := range labels {
				found := false
				for _, lp := range m.GetLabel() {
					if lp.GetName() == k && lp.GetValue() == v {
						found = true
						break
					}
				}
				if !found {
					match = false
					break
				}
			}
			if match {
				if c := m.GetCounter(); c != nil {
					return c.GetValue()
				}
			}
		}
	}
	return 0
}

func buildMetricsApp(m *middleware.GatewayMetrics, tenantID, model string, statusCode int, cacheHeader string) *fiber.App {
	app := fiber.New()

	// Simulate auth middleware setting user local.
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{UserID: "u1", TenantID: tenantID})
		return c.Next()
	})

	app.Use(middleware.NewMetricsMiddleware(m))

	app.Post("/v1/chat/completions", func(c *fiber.Ctx) error {
		if cacheHeader != "" {
			c.Set("X-Cache", cacheHeader)
		}
		return c.Status(statusCode).JSON(fiber.Map{"model": model})
	})

	return app
}

func doRequest(t *testing.T, app *fiber.App, body string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode
}

// ---------------------------------------------------------------------------

func TestMetricsMiddleware_RequestsTotal(t *testing.T) {
	m, reg := freshMetrics()
	app := buildMetricsApp(m, "tenant-1", "gpt-4o", 200, "")

	sc := doRequest(t, app, `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, 200, sc)

	v := counterValue(t, reg, "gateway_requests_total", prometheus.Labels{
		"tenant_id":   "tenant-1",
		"model":       "gpt-4o",
		"status_code": "200",
	})
	assert.Equal(t, float64(1), v)
}

func TestMetricsMiddleware_CacheHitExact(t *testing.T) {
	m, reg := freshMetrics()
	app := buildMetricsApp(m, "tenant-2", "claude-sonnet-4-6", 200, "HIT")

	doRequest(t, app, `{"model":"claude-sonnet-4-6","messages":[]}`)

	v := counterValue(t, reg, "gateway_cache_hits_total", prometheus.Labels{
		"tenant_id":  "tenant-2",
		"cache_type": "exact",
	})
	assert.Equal(t, float64(1), v)
}

func TestMetricsMiddleware_CacheHitSemantic(t *testing.T) {
	m, reg := freshMetrics()
	app := buildMetricsApp(m, "tenant-3", "claude-sonnet-4-6", 200, "SEMANTIC-HIT")

	doRequest(t, app, `{"model":"claude-sonnet-4-6","messages":[]}`)

	v := counterValue(t, reg, "gateway_cache_hits_total", prometheus.Labels{
		"tenant_id":  "tenant-3",
		"cache_type": "semantic",
	})
	assert.Equal(t, float64(1), v)
}

func TestMetricsMiddleware_ServerError(t *testing.T) {
	m, reg := freshMetrics()
	app := buildMetricsApp(m, "tenant-4", "gpt-4o", 500, "")

	sc := doRequest(t, app, `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, 500, sc)

	v := counterValue(t, reg, "gateway_errors_total", prometheus.Labels{
		"tenant_id":  "tenant-4",
		"model":      "gpt-4o",
		"error_type": "server_error",
	})
	assert.Equal(t, float64(1), v)
}

func TestMetricsMiddleware_ClientError(t *testing.T) {
	m, reg := freshMetrics()
	app := buildMetricsApp(m, "tenant-5", "gpt-4o", 400, "")

	sc := doRequest(t, app, `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, 400, sc)

	v := counterValue(t, reg, "gateway_errors_total", prometheus.Labels{
		"tenant_id":  "tenant-5",
		"model":      "gpt-4o",
		"error_type": "client_error",
	})
	assert.Equal(t, float64(1), v)
}

func TestMetricsMiddleware_QuotaExceededNotCountedAsError(t *testing.T) {
	m, reg := freshMetrics()
	app := buildMetricsApp(m, "tenant-6", "gpt-4o", 429, "")

	sc := doRequest(t, app, `{"model":"gpt-4o","messages":[]}`)
	assert.Equal(t, 429, sc)

	// 429 must NOT appear in errors_total.
	v := counterValue(t, reg, "gateway_errors_total", prometheus.Labels{
		"tenant_id":  "tenant-6",
		"model":      "gpt-4o",
		"error_type": "client_error",
	})
	assert.Equal(t, float64(0), v)
}

func TestMetricsMiddleware_UnknownTenantWhenNoUser(t *testing.T) {
	m, reg := freshMetrics()

	// App without auth middleware — no user local set.
	app := fiber.New()
	app.Use(middleware.NewMetricsMiddleware(m))
	app.Get("/probe", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	v := counterValue(t, reg, "gateway_requests_total", prometheus.Labels{
		"tenant_id":   "unknown",
		"model":       "unknown",
		"status_code": "200",
	})
	assert.Equal(t, float64(1), v)
}
