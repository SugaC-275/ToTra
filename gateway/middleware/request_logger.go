package middleware

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// RequestLogInserter is satisfied by *storage.RequestLogStore.
// Using an interface here prevents an import cycle since storage already imports middleware.
type RequestLogInserter interface {
	InsertRaw(ctx context.Context, log *RawRequestLog) error
}

// RawRequestLog carries the raw fields captured by the logger middleware.
// storage.RequestLogStore maps this to its own type.
type RawRequestLog struct {
	TenantID     string
	UserID       string
	StatusCode   int
	LatencyMS    int
	RequestBody  []byte
	ResponseBody []byte
	Tags         []string // spend tags from X-ToTra-Tags / X-Tags / metadata.tags
}

// NewRequestLoggerMiddleware returns a Fiber handler that captures the request
// and response bodies for every /v1/ path (excluding /metrics and /health) and
// persists them asynchronously so that latency is unaffected.
func NewRequestLoggerMiddleware(store RequestLogInserter) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Only capture API traffic, not observability endpoints.
		path := c.Path()
		if !strings.HasPrefix(path, "/v1/") ||
			strings.HasPrefix(path, "/v1/metrics") ||
			strings.HasPrefix(path, "/v1/health") {
			return c.Next()
		}

		// Snapshot request body before the handler consumes it.
		reqBody := make([]byte, len(c.Body()))
		copy(reqBody, c.Body())

		start := time.Now()

		if err := c.Next(); err != nil {
			return err
		}

		statusCode := c.Response().StatusCode()
		if statusCode == 0 {
			return nil
		}

		latencyMS := int(time.Since(start).Milliseconds())

		// c.Response().Body() is available after Next() returns.
		rawResp := c.Response().Body()
		var respBody []byte
		if len(rawResp) > 0 {
			respBody = make([]byte, len(rawResp))
			copy(respBody, rawResp)
		}

		user, _ := c.Locals("user").(*UserInfo)
		if user == nil {
			return nil
		}

		tags, _ := c.Locals("spend_tags").([]string)
		record := &RawRequestLog{
			TenantID:     user.TenantID,
			UserID:       user.UserID,
			StatusCode:   statusCode,
			LatencyMS:    latencyMS,
			RequestBody:  reqBody,
			ResponseBody: respBody,
			Tags:         tags,
		}

		// Non-blocking insert — must not add to the request path latency.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := store.InsertRaw(ctx, record); err != nil {
				slog.Warn("request_logger: insert failed", "err", err)
			}
		}()

		return nil
	}
}
