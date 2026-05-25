package middleware

import (
	"context"
	"os"

	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.39.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const tracerName = "totra.gateway"

// InitOTEL initialises the global OTel trace provider.
// Reads OTEL_EXPORTER_OTLP_ENDPOINT (OTLP HTTP) and OTEL_SERVICE_NAME
// (default "totra-gateway"). Falls back to a no-op provider when the
// endpoint env var is unset. The returned shutdown func must be called on exit.
func InitOTEL(ctx context.Context) (shutdown func(context.Context) error, err error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(_ context.Context) error { return nil }, nil
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "totra-gateway"
	}

	exp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	shutdown = func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}
	return shutdown, nil
}

// NewOTELMiddleware returns a Fiber middleware that creates a "llm.request"
// span for every request. Must run after auth middleware.
// Attributes: llm.tenant_id, llm.model, llm.provider, llm.cache_hit,
// llm.fallback_used, http.method, http.route, http.status_code.
// Sets span status to Error on HTTP 5xx.
func NewOTELMiddleware() fiber.Handler {
	tracer := otel.Tracer(tracerName)

	return func(c *fiber.Ctx) error {
		ctx, span := tracer.Start(c.UserContext(), "llm.request",
			trace.WithSpanKind(trace.SpanKindServer),
		)
		c.SetUserContext(ctx)
		defer span.End()

		// Run the rest of the handler chain first so response fields are available.
		chainErr := c.Next()

		// --- resolve attribute values ---

		tenantID := "unknown"
		if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
			tenantID = user.TenantID
		}

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

		provider := ""
		if rp, ok := c.Locals("routed_provider").(string); ok {
			provider = rp
		}

		cacheHit := c.GetRespHeader("X-Cache")

		fallbackUsed := false
		if fu, ok := c.Locals("fallback_used").(bool); ok {
			fallbackUsed = fu
		}

		statusCode := c.Response().StatusCode()

		// --- set span attributes ---

		attrs := []attribute.KeyValue{
			attribute.String("llm.tenant_id", tenantID),
			attribute.String("llm.model", model),
			attribute.String("http.method", c.Method()),
			attribute.String("http.route", c.Path()),
			attribute.Int("http.status_code", statusCode),
			attribute.Bool("llm.fallback_used", fallbackUsed),
		}
		if provider != "" {
			attrs = append(attrs, attribute.String("llm.provider", provider))
		}
		if cacheHit != "" {
			attrs = append(attrs, attribute.String("llm.cache_hit", cacheHit))
		}
		span.SetAttributes(attrs...)

		if statusCode >= 500 {
			span.SetStatus(codes.Error, "upstream error")
		}

		return chainErr
	}
}
