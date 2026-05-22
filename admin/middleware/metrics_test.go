package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/middleware"
)

func freshAdminMetrics() (*middleware.AdminMetrics, *prometheus.Registry) {
	reg := prometheus.NewRegistry()
	m := middleware.NewAdminMetrics(reg)
	return m, reg
}

func adminCounterValue(t *testing.T, reg *prometheus.Registry, name string, labels prometheus.Labels) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, metric := range mf.GetMetric() {
			match := true
			for k, v := range labels {
				found := false
				for _, lp := range metric.GetLabel() {
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
				if c := metric.GetCounter(); c != nil {
					return c.GetValue()
				}
			}
		}
	}
	return 0
}

func buildAdminApp(m *middleware.AdminMetrics, statusCode int) *fiber.App {
	app := fiber.New()
	app.Use(middleware.NewAdminMetricsMiddleware(m))
	app.Get("/api/admin/users", func(c *fiber.Ctx) error {
		return c.SendStatus(statusCode)
	})
	return app
}

func TestAdminMetricsMiddleware_RequestsTotal(t *testing.T) {
	m, reg := freshAdminMetrics()
	app := buildAdminApp(m, 200)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	v := adminCounterValue(t, reg, "admin_requests_total", prometheus.Labels{
		"path":        "/api/admin/users",
		"method":      "GET",
		"status_code": "200",
	})
	assert.Equal(t, float64(1), v)
}

func TestAdminMetricsMiddleware_ErrorStatusCounted(t *testing.T) {
	m, reg := freshAdminMetrics()
	app := buildAdminApp(m, 500)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 500, resp.StatusCode)

	v := adminCounterValue(t, reg, "admin_requests_total", prometheus.Labels{
		"path":        "/api/admin/users",
		"method":      "GET",
		"status_code": "500",
	})
	assert.Equal(t, float64(1), v)
}

func TestAdminMetricsMiddleware_MultipleRequests(t *testing.T) {
	m, reg := freshAdminMetrics()
	app := buildAdminApp(m, 200)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
		_, err := app.Test(req)
		require.NoError(t, err)
	}

	v := adminCounterValue(t, reg, "admin_requests_total", prometheus.Labels{
		"path":        "/api/admin/users",
		"method":      "GET",
		"status_code": "200",
	})
	assert.Equal(t, float64(3), v)
}

func TestAdminMetricsMiddleware_DurationRecorded(t *testing.T) {
	m, reg := freshAdminMetrics()
	app := buildAdminApp(m, 200)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	_, err := app.Test(req)
	require.NoError(t, err)

	mfs, err := reg.Gather()
	require.NoError(t, err)

	found := false
	for _, mf := range mfs {
		if mf.GetName() == "admin_request_duration_seconds" {
			for _, metric := range mf.GetMetric() {
				if h := metric.GetHistogram(); h != nil && h.GetSampleCount() > 0 {
					found = true
				}
			}
		}
	}
	assert.True(t, found, "expected at least one duration observation")
}
