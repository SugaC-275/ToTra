package handlers

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns a Fiber handler that serves the Prometheus metrics
// endpoint for the provided gatherer. Pass prometheus.DefaultGatherer for
// production; pass the same *prometheus.Registry used by NewAdminMetrics in
// tests.
func MetricsHandler(gatherer prometheus.Gatherer) fiber.Handler {
	h := promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{})
	return adaptor.HTTPHandler(h)
}
