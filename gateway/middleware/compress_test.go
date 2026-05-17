package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/middleware"
)

func TestCompressBody_NoOp_ShortCleanRequest(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello world"}]}`)
	result, orig, comp := middleware.CompressBody(body)
	assert.Equal(t, orig, comp, "clean request should not be compressed")
	assert.Equal(t, body, result)
}

func TestCompressBody_MultipleNewlines_Collapsed(t *testing.T) {
	// JSON-encode a string with 4 consecutive newlines
	input := "line1\n\n\n\nline2"
	bodyMap := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "user", "content": input},
		},
	}
	body, _ := json.Marshal(bodyMap)

	result, orig, comp := middleware.CompressBody(body)
	assert.Less(t, comp, orig, "compressed size should be smaller")

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	msgs := out["messages"].([]interface{})
	content := msgs[0].(map[string]interface{})["content"].(string)
	assert.Equal(t, "line1\n\nline2", content, "4 newlines should collapse to 2")
}

func TestCompressBody_TrailingWhitespace_Removed(t *testing.T) {
	input := "hello   \nworld  \nfoo"
	bodyMap := map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": input}},
	}
	body, _ := json.Marshal(bodyMap)

	result, orig, comp := middleware.CompressBody(body)
	assert.LessOrEqual(t, comp, orig)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	msgs := out["messages"].([]interface{})
	content := msgs[0].(map[string]interface{})["content"].(string)
	assert.Equal(t, "hello\nworld\nfoo", content)
}

func TestCompressBody_SeparatorLines_Normalized(t *testing.T) {
	input := "section1\n----------\nsection2\n========\nsection3"
	bodyMap := map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": input}},
	}
	body, _ := json.Marshal(bodyMap)

	result, orig, comp := middleware.CompressBody(body)
	assert.Less(t, comp, orig)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	msgs := out["messages"].([]interface{})
	content := msgs[0].(map[string]interface{})["content"].(string)
	assert.Equal(t, "section1\n---\nsection2\n===\nsection3", content)
}

func TestCompressBody_NonStringContent_Untouched(t *testing.T) {
	// Vision requests use array content — must not be touched
	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image_url","image_url":{"url":"data:..."}}]}]}`)
	result, orig, comp := middleware.CompressBody(body)
	assert.Equal(t, orig, comp, "array content should not be modified")
	assert.Equal(t, body, result)
}

func TestCompressBody_InvalidJSON_ReturnsOriginal(t *testing.T) {
	body := []byte(`not valid json {{{`)
	result, orig, comp := middleware.CompressBody(body)
	assert.Equal(t, orig, comp)
	assert.Equal(t, body, result)
}

func TestCompressBody_NoMessagesField_ReturnsOriginal(t *testing.T) {
	body := []byte(`{"model":"gpt-4o","prompt":"hello world"}`)
	result, orig, comp := middleware.CompressBody(body)
	assert.Equal(t, orig, comp)
	assert.Equal(t, body, result)
}

func TestCompressBody_MultipleMessages_AllCompressed(t *testing.T) {
	bodyMap := map[string]interface{}{
		"model": "gpt-4o",
		"messages": []map[string]string{
			{"role": "system", "content": "you are helpful\n\n\n\nbe concise"},
			{"role": "user", "content": "question\n\n\n\nanswer please"},
		},
	}
	body, _ := json.Marshal(bodyMap)

	result, orig, comp := middleware.CompressBody(body)
	assert.Less(t, comp, orig)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	msgs := out["messages"].([]interface{})
	assert.Equal(t, "you are helpful\n\nbe concise", msgs[0].(map[string]interface{})["content"])
	assert.Equal(t, "question\n\nanswer please", msgs[1].(map[string]interface{})["content"])
}

func TestNewCompressMiddleware_SetsLocals_WhenSavings(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewCompressMiddleware())

	var savedBytes int
	app.Post("/test", func(c *fiber.Ctx) error {
		if v, ok := c.Locals("compression_saved_bytes").(int); ok {
			savedBytes = v
		}
		return c.SendStatus(200)
	})

	bodyMap := map[string]interface{}{
		"model":    "gpt-4o",
		"messages": []map[string]string{{"role": "user", "content": "line1\n\n\n\n\n\nline2"}},
	}
	body, _ := json.Marshal(bodyMap)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Greater(t, savedBytes, 0, "compression_saved_bytes should be set in Locals")
}

func TestNewCompressMiddleware_NoLocals_WhenNoSavings(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewCompressMiddleware())

	var localsSet bool
	app.Post("/test", func(c *fiber.Ctx) error {
		_, localsSet = c.Locals("compression_saved_bytes").(int)
		return c.SendStatus(200)
	})

	body := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.False(t, localsSet, "compression_saved_bytes should NOT be set when nothing was compressed")
}

func TestNewCompressMiddleware_EmptyBody_Passthrough(t *testing.T) {
	app := fiber.New()
	app.Use(middleware.NewCompressMiddleware())
	app.Get("/health", func(c *fiber.Ctx) error { return c.SendStatus(200) })

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
}
