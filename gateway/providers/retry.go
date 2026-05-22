package providers

import (
	"context"
	"fmt"
	"time"
)

const (
	retryBaseDelay = 500 * time.Millisecond
	retryMaxDelay  = 5 * time.Second
)

// RetryAdapter wraps any Adapter with exponential backoff retry.
// It retries only on upstream (5xx-class) errors, not on client (4xx) errors.
type RetryAdapter struct {
	inner       Adapter
	maxAttempts int
}

// NewRetryAdapter creates a RetryAdapter. maxAttempts must be >= 1.
func NewRetryAdapter(inner Adapter, maxAttempts int) *RetryAdapter {
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	return &RetryAdapter{inner: inner, maxAttempts: maxAttempts}
}

// Forward calls the inner adapter and retries on 5xx responses or transport errors.
// 4xx responses are returned immediately without retrying (they are client errors).
// Backoff starts at 500 ms and doubles each attempt, capped at 5 s.
func (r *RetryAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var (
		result *ForwardResult
		usage  *Usage
		err    error
		delay  = retryBaseDelay
	)

	for attempt := 0; attempt < r.maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, fmt.Errorf("retry: context cancelled while waiting for backoff: %w", ctx.Err())
			case <-time.After(delay):
			}
			delay *= 2
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
		}

		result, usage, err = r.inner.Forward(ctx, body)
		if err != nil {
			// Transport-level error: retry.
			continue
		}
		if result.StatusCode >= 500 {
			// Upstream server error: retry.
			continue
		}
		// 2xx, 3xx, 4xx — do not retry.
		return result, usage, nil
	}

	// All attempts exhausted.
	if err != nil {
		return nil, nil, fmt.Errorf("retry: all %d attempts failed: %w", r.maxAttempts, err)
	}
	return result, usage, nil
}

// ForwardStream delegates streaming directly to the inner adapter without retry.
// Retrying a stream is not safe because the response has already started being
// delivered; errors mid-stream must be handled by the caller.
func (r *RetryAdapter) ForwardStream(ctx context.Context, body []byte, onChunk func([]byte) error) error {
	return r.inner.ForwardStream(ctx, body, onChunk)
}

// BuildFilePrompt delegates directly to the inner adapter.
func (r *RetryAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	return r.inner.BuildFilePrompt(model, docText, userMessage)
}
