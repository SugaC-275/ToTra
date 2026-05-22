package middleware

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
)

// AdminMetrics holds all Prometheus collectors for the admin service.
type AdminMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
}

// NewAdminMetrics registers and returns an AdminMetrics using the provided
// registerer. Pass prometheus.DefaultRegisterer for production; pass a fresh
// prometheus.NewRegistry() in tests to avoid conflicts between test runs.
func NewAdminMetrics(reg prometheus.Registerer) *AdminMetrics {
	m := &AdminMetrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "admin",
			Name:      "requests_total",
			Help:      "Total number of admin API requests.",
		}, []string{"path", "method", "status_code"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "admin",
			Name:      "request_duration_seconds",
			Help:      "Latency distribution of admin API requests.",
			Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5},
		}, []string{"path", "method"}),
	}

	reg.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
	)

	return m
}

// NewAdminMetricsMiddleware returns a Fiber middleware that records per-request
// Prometheus metrics for the admin service.
func NewAdminMetricsMiddleware(m *AdminMetrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		elapsed := time.Since(start).Seconds()
		path := c.Path()
		method := c.Method()
		statusCode := strconv.Itoa(c.Response().StatusCode())

		m.RequestsTotal.WithLabelValues(path, method, statusCode).Inc()
		m.RequestDuration.WithLabelValues(path, method).Observe(elapsed)

		return err
	}
}
