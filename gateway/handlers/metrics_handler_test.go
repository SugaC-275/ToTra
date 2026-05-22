package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
)

func TestMetricsHandler_ServesPrometheusFormat(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := middleware.NewGatewayMetrics(reg)

	// Increment one counter so there is something to scrape.
	m.RequestsTotal.WithLabelValues("t1", "gpt-4o", "200").Inc()

	app := fiber.New()
	app.Get("/metrics", handlers.MetricsHandler(reg))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
}

func TestMetricsHandler_ContainsGatewayMetricNames(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := middleware.NewGatewayMetrics(reg)

	m.RequestsTotal.WithLabelValues("t1", "gpt-4o", "200").Inc()
	m.CacheHitsTotal.WithLabelValues("t1", "exact").Add(3)
	m.ErrorsTotal.WithLabelValues("t1", "gpt-4o", "server_error").Inc()

	app := fiber.New()
	app.Get("/metrics", handlers.MetricsHandler(reg))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	assert.Contains(t, body, "gateway_requests_total")
	assert.Contains(t, body, "gateway_cache_hits_total")
	assert.Contains(t, body, "gateway_errors_total")
}
