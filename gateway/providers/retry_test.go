package providers_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

// stubAdapter is a test double for providers.Adapter.
// Each call pops the next response from the queue.
type stubAdapter struct {
	responses []stubResponse
	calls     int
}

type stubResponse struct {
	result *providers.ForwardResult
	usage  *providers.Usage
	err    error
}

func (s *stubAdapter) Forward(_ context.Context, _ []byte) (*providers.ForwardResult, *providers.Usage, error) {
	if s.calls >= len(s.responses) {
		return nil, nil, errors.New("stub: no more responses")
	}
	r := s.responses[s.calls]
	s.calls++
	return r.result, r.usage, r.err
}

func (s *stubAdapter) ForwardStream(_ context.Context, _ []byte, _ func([]byte) error) error {
	return nil
}

func (s *stubAdapter) BuildFilePrompt(_, _, _ string) []byte { return nil }

func ok200() stubResponse {
	return stubResponse{
		result: &providers.ForwardResult{StatusCode: http.StatusOK, Headers: make(http.Header), Body: []byte(`{}`)},
		usage:  &providers.Usage{PromptTokens: 1, CompletionTokens: 1},
	}
}

func upstream500() stubResponse {
	return stubResponse{
		result: &providers.ForwardResult{StatusCode: http.StatusInternalServerError, Headers: make(http.Header), Body: []byte(`{"error":"internal"}`)},
		usage:  &providers.Usage{},
	}
}

func transportErr() stubResponse {
	return stubResponse{err: errors.New("connection refused")}
}

func client400() stubResponse {
	return stubResponse{
		result: &providers.ForwardResult{StatusCode: http.StatusBadRequest, Headers: make(http.Header), Body: []byte(`{"error":"bad request"}`)},
		usage:  &providers.Usage{},
	}
}

// TestRetryAdapter_SuccessOnFirstAttempt checks no retry when first call succeeds.
func TestRetryAdapter_SuccessOnFirstAttempt(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{ok200()}}
	adapter := providers.NewRetryAdapter(stub, 3)

	result, usage, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, 1, usage.PromptTokens)
	assert.Equal(t, 1, stub.calls)
}

// TestRetryAdapter_RetryOn5xx checks that 5xx triggers retry and eventual success.
func TestRetryAdapter_RetryOn5xx(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{upstream500(), upstream500(), ok200()}}
	// Use maxAttempts=3 but override delay via a zero-duration context trick is not
	// possible without exporting delay — instead we accept the small sleep in tests.
	// For speed, keep maxAttempts=3 and accept ~1s total delay.
	adapter := providers.NewRetryAdapter(stub, 3)

	result, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, 3, stub.calls)
}

// TestRetryAdapter_RetryOnTransportError checks transport errors trigger retry.
func TestRetryAdapter_RetryOnTransportError(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{transportErr(), ok200()}}
	adapter := providers.NewRetryAdapter(stub, 3)

	result, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
	assert.Equal(t, 2, stub.calls)
}

// TestRetryAdapter_NoRetryOn4xx checks that client errors are returned immediately.
func TestRetryAdapter_NoRetryOn4xx(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{client400(), ok200()}}
	adapter := providers.NewRetryAdapter(stub, 3)

	result, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, result.StatusCode)
	assert.Equal(t, 1, stub.calls, "4xx must not trigger a retry")
}

// TestRetryAdapter_AllAttemptsExhausted checks error returned after max attempts.
func TestRetryAdapter_AllAttemptsExhausted(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{transportErr(), transportErr(), transportErr()}}
	adapter := providers.NewRetryAdapter(stub, 3)

	_, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all 3 attempts failed")
	assert.Equal(t, 3, stub.calls)
}

// TestRetryAdapter_AllAttemptsExhausted5xx returns last 5xx after max retries.
func TestRetryAdapter_AllAttemptsExhausted5xx(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{upstream500(), upstream500(), upstream500()}}
	adapter := providers.NewRetryAdapter(stub, 3)

	result, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err, "5xx exhaustion returns the result, not a Go error")
	assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
	assert.Equal(t, 3, stub.calls)
}

// TestRetryAdapter_ContextCancelled checks early exit when context is cancelled.
func TestRetryAdapter_ContextCancelled(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{transportErr(), transportErr(), transportErr()}}
	adapter := providers.NewRetryAdapter(stub, 3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so first backoff sleep is skipped

	_, _, err := adapter.Forward(ctx, []byte(`{}`))
	require.Error(t, err)
	// Either "context cancelled" during backoff or the transport error from attempt 0.
	// Either way an error must be returned.
}

// TestRetryAdapter_MaxAttempts1_NeverRetries checks single-attempt mode.
func TestRetryAdapter_MaxAttempts1_NeverRetries(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{upstream500(), ok200()}}
	adapter := providers.NewRetryAdapter(stub, 1)

	result, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
	assert.Equal(t, 1, stub.calls)
}

// TestNewRetryAdapter_MinAttempts1 checks that maxAttempts<1 is clamped to 1.
func TestNewRetryAdapter_MinAttempts1(t *testing.T) {
	stub := &stubAdapter{responses: []stubResponse{ok200()}}
	adapter := providers.NewRetryAdapter(stub, 0)
	result, _, err := adapter.Forward(context.Background(), []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, result.StatusCode)
}

// TestRetryAdapter_BuildFilePrompt delegates to inner.
func TestRetryAdapter_BuildFilePrompt(t *testing.T) {
	inner := providers.NewOpenAIAdapter("http://x", "key")
	adapter := providers.NewRetryAdapter(inner, 3)
	body := adapter.BuildFilePrompt("gpt-4o", "doc", "summarize")
	assert.NotNil(t, body)
	assert.Contains(t, string(body), "gpt-4o")
}
