package middleware

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/prometheus/client_golang/prometheus"
)

// GatewayMetrics holds all Prometheus collectors for the gateway.
type GatewayMetrics struct {
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	TokensTotal     *prometheus.CounterVec
	CacheHitsTotal  *prometheus.CounterVec
	ErrorsTotal     *prometheus.CounterVec
}

// NewGatewayMetrics registers and returns a GatewayMetrics using the provided registry.
// Pass prometheus.DefaultRegisterer for production; pass a fresh prometheus.NewRegistry()
// in tests to avoid conflicts between test runs.
func NewGatewayMetrics(reg prometheus.Registerer) *GatewayMetrics {
	m := &GatewayMetrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "gateway",
			Name:      "requests_total",
			Help:      "Total number of gateway requests.",
		}, []string{"tenant_id", "model", "status_code"}),

		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "gateway",
			Name:      "request_duration_seconds",
			Help:      "Latency distribution of gateway requests.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10},
		}, []string{"tenant_id", "model"}),

		TokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "gateway",
			Name:      "tokens_total",
			Help:      "Total tokens processed (prompt and completion).",
		}, []string{"tenant_id", "model", "type"}),

		CacheHitsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "gateway",
			Name:      "cache_hits_total",
			Help:      "Total cache hits by cache type (exact or semantic).",
		}, []string{"tenant_id", "cache_type"}),

		ErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "gateway",
			Name:      "errors_total",
			Help:      "Total gateway errors by error type.",
		}, []string{"tenant_id", "model", "error_type"}),
	}

	reg.MustRegister(
		m.RequestsTotal,
		m.RequestDuration,
		m.TokensTotal,
		m.CacheHitsTotal,
		m.ErrorsTotal,
	)

	return m
}

// modelFromBody attempts to parse the request body JSON to extract the model field.
// Returns empty string on any failure — the caller falls back to "unknown".
func modelFromBody(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

// NewMetricsMiddleware returns a Fiber middleware that records per-request
// Prometheus metrics. It must run after auth middleware (so c.Locals("user")
// is populated) and after any router middleware (so c.Locals("routed_model")
// may be set).
func NewMetricsMiddleware(m *GatewayMetrics) fiber.Handler {
	return func(c *fiber.Ctx) error {
		start := time.Now()

		err := c.Next()

		elapsed := time.Since(start).Seconds()

		// Resolve tenant_id from the auth local.
		tenantID := "unknown"
		if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
			tenantID = user.TenantID
		}

		// Resolve model name: prefer the post-routing model set by the proxy,
		// then fall back to parsing the request body.
		model := ""
		if rm, ok := c.Locals("routed_model").(string); ok {
			model = rm
		}
		if model == "" {
			model = modelFromBody(c.Body())
		}
		if model == "" {
			model = "unknown"
		}

		statusCode := strconv.Itoa(c.Response().StatusCode())

		m.RequestsTotal.WithLabelValues(tenantID, model, statusCode).Inc()
		m.RequestDuration.WithLabelValues(tenantID, model).Observe(elapsed)

		// Cache hit detection via response header set by proxy handler.
		cacheHeader := c.GetRespHeader("X-Cache")
		switch cacheHeader {
		case "HIT":
			m.CacheHitsTotal.WithLabelValues(tenantID, "exact").Inc()
		case "SEMANTIC-HIT":
			m.CacheHitsTotal.WithLabelValues(tenantID, "semantic").Inc()
		}

		// Count as an error for 4xx client errors (excluding 429 quota) and all 5xx.
		sc := c.Response().StatusCode()
		if sc >= 500 {
			m.ErrorsTotal.WithLabelValues(tenantID, model, "server_error").Inc()
		} else if sc >= 400 && sc != fiber.StatusTooManyRequests {
			m.ErrorsTotal.WithLabelValues(tenantID, model, "client_error").Inc()
		}

		return err
	}
}
