package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// --- stubs ---

type stubModelLookup struct{ cfg *storage.ModelConfig }

func (s *stubModelLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return s.cfg, nil
}

type stubParser struct {
	result *storage.ParseResult
	err    error
}

func (s *stubParser) Parse(_ context.Context, _ string, _ []byte) (*storage.ParseResult, error) {
	return s.result, s.err
}

type stubViolationRecorder struct{ called bool }

func (s *stubViolationRecorder) RecordViolation(_, _, _, _, _ string) { s.called = true }

type stubUsageRecorder struct{}

func (s *stubUsageRecorder) Record(_ *storage.UsageRecord) {}

func multipartRequest(t *testing.T, fileContent, message, model string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "test.pdf")
	require.NoError(t, err)
	fw.Write([]byte(fileContent))
	w.WriteField("message", message)
	w.WriteField("model", model)
	w.Close()
	req := httptest.NewRequest(http.MethodPost, "/v1/files/chat", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func makeApp(lookup handlers.ModelLookup, parser handlers.FileParser, recorder middleware.ViolationRecorder, usage handlers.UsageRecorder) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &middleware.UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Post("/v1/files/chat", handlers.NewFileChatHandler(lookup, parser, recorder, usage))
	return app
}

// --- tests ---

func TestFileChatHandler_PIIDetected_Returns422(t *testing.T) {
	lookup := &stubModelLookup{cfg: &storage.ModelConfig{ID: "m1", Provider: "openai", BaseURL: "http://x", APIKey: "k", SCURate: 1.0}}
	parser := &stubParser{result: &storage.ParseResult{Text: "call 13800001234", PageCount: 1}}
	recorder := &stubViolationRecorder{}

	resp, err := makeApp(lookup, parser, recorder, &stubUsageRecorder{}).
		Test(multipartRequest(t, "fake pdf", "summarize", "gpt-4"))
	require.NoError(t, err)
	assert.Equal(t, 422, resp.StatusCode)
	assert.True(t, recorder.called)
}

func TestFileChatHandler_LocalModel_Returns400(t *testing.T) {
	lookup := &stubModelLookup{cfg: &storage.ModelConfig{ID: "m1", Provider: "local", BaseURL: "http://x", APIKey: "", SCURate: 0.1}}
	parser := &stubParser{result: &storage.ParseResult{Text: "clean text", PageCount: 1}}

	resp, err := makeApp(lookup, parser, &stubViolationRecorder{}, &stubUsageRecorder{}).
		Test(multipartRequest(t, "fake", "summarize", "llama"))
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	errMap := body["error"].(map[string]interface{})
	assert.Equal(t, "local_model_not_supported", errMap["type"])
}

func TestFileChatHandler_MissingFile_Returns400(t *testing.T) {
	lookup := &stubModelLookup{cfg: &storage.ModelConfig{Provider: "openai"}}
	req := httptest.NewRequest(http.MethodPost, "/v1/files/chat", bytes.NewReader([]byte("not multipart")))
	req.Header.Set("Content-Type", "application/json")

	resp, err := makeApp(lookup, &stubParser{}, &stubViolationRecorder{}, &stubUsageRecorder{}).Test(req)
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestFileChatHandler_UnsupportedFormat_Returns400(t *testing.T) {
	lookup := &stubModelLookup{cfg: &storage.ModelConfig{ID: "m1", Provider: "openai", BaseURL: "http://x", APIKey: "k", SCURate: 1.0}}
	parser := &stubParser{err: fmt.Errorf("unsupported format")}

	resp, err := makeApp(lookup, parser, &stubViolationRecorder{}, &stubUsageRecorder{}).
		Test(multipartRequest(t, "data", "summarize", "gpt-4"))
	require.NoError(t, err)
	assert.Equal(t, 400, resp.StatusCode)
}
