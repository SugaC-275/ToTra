package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/handlers"
	"github.com/yourorg/totra/admin/middleware"
)

func TestAdminMetricsHandler_ServesPrometheusFormat(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := middleware.NewAdminMetrics(reg)

	// Seed a counter so the scrape body is non-empty.
	m.RequestsTotal.WithLabelValues("/api/admin/users", "GET", "200").Inc()

	app := fiber.New()
	app.Get("/metrics", handlers.MetricsHandler(reg))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
}

func TestAdminMetricsHandler_ContainsAdminMetricNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := middleware.NewAdminMetrics(reg)

	m.RequestsTotal.WithLabelValues("/api/admin/users", "GET", "200").Inc()
	m.RequestsTotal.WithLabelValues("/api/admin/quota", "POST", "201").Add(2)
	// An observation is required for the histogram to appear in the scrape output.
	m.RequestDuration.WithLabelValues("/api/admin/users", "GET").Observe(0.05)

	app := fiber.New()
	app.Get("/metrics", handlers.MetricsHandler(reg))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	assert.Contains(t, body, "admin_requests_total")
	assert.Contains(t, body, "admin_request_duration_seconds")
}
