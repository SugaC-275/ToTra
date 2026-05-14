# File Upload + PII Scanning Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow employees to upload PDF/DOCX/PPTX files alongside a question; the gateway extracts text via a Python parser microservice, scans for PII (blocking the request if found), then injects the clean text into a prompt and forwards to any cloud LLM.

**Architecture:** Phase 1 replaces the hard-coded `switch` provider selection with a registry pattern (`providers.Adapter` interface + `providers.New()`), adding Gemini support and a `BuildFilePrompt` method to every adapter. Phase 2 adds a Python FastAPI parser microservice, a `ParserClient` in Go, a `ScanForPII` helper exported from the middleware package, and a `FileChatHandler` that orchestrates the full upload → scan → forward flow.

**Tech Stack:** Go 1.23 + Fiber v2 + pgx/v5 + net/http · Python 3.12 + FastAPI + pdfplumber + python-docx + python-pptx · Docker Compose

---

## File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `gateway/providers/registry.go` | `Adapter` interface + `Register`/`New` registry functions |
| Create | `gateway/providers/registry_test.go` | Tests for registry lookup |
| Create | `gateway/providers/gemini.go` | Gemini adapter: `Forward` + `BuildFilePrompt` + `init()` |
| Create | `gateway/providers/gemini_test.go` | Tests for Gemini adapter |
| Modify | `gateway/providers/openai.go` | Add `init()` registration + `BuildFilePrompt` |
| Modify | `gateway/providers/anthropic.go` | Add `init()` registration + `BuildFilePrompt` |
| Modify | `gateway/providers/local.go` | Add `init()` registration + `BuildFilePrompt` (returns nil) |
| Modify | `gateway/main.go` | Replace switch with `providers.New()` + wire `/v1/files/chat` |
| Modify | `gateway/config/config.go` | Add `ParserURL` field |
| Create | `gateway/storage/parser_client.go` | HTTP client to Python parser service |
| Create | `gateway/storage/parser_client_test.go` | Tests using httptest.Server |
| Modify | `gateway/middleware/pii.go` | Export `ScanForPII(text string) (string, bool)` |
| Modify | `gateway/middleware/pii_test.go` | Tests for `ScanForPII` |
| Create | `gateway/handlers/file_chat.go` | `NewFileChatHandler` — orchestrates upload → scan → forward |
| Create | `gateway/handlers/file_chat_test.go` | Handler tests with stub dependencies |
| Create | `parser/main.py` | FastAPI app: `POST /parse` |
| Create | `parser/requirements.txt` | Python deps |
| Create | `parser/Dockerfile` | python:3.12-slim image |
| Modify | `docker-compose.yml` | Add `parser` service |
| Modify | `.env` | Add `PARSER_PORT` |

---

## Task 1: Provider Registry — Adapter Interface + Registry

**Files:**
- Create: `gateway/providers/registry.go`
- Create: `gateway/providers/registry_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/providers/registry_test.go
package providers_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

type fakeAdapter struct{}

func (a *fakeAdapter) Forward(_ context.Context, _ []byte) (*providers.ForwardResult, *providers.Usage, error) {
	return &providers.ForwardResult{StatusCode: 200}, &providers.Usage{}, nil
}
func (a *fakeAdapter) BuildFilePrompt(_, _, _ string) []byte { return []byte("{}") }

func TestRegistry_RegisterAndNew(t *testing.T) {
	providers.Register("test-fake-xyz", func(_, _ string) providers.Adapter { return &fakeAdapter{} })
	got, err := providers.New("test-fake-xyz", "http://x", "key")
	require.NoError(t, err)
	assert.NotNil(t, got)
}

func TestRegistry_New_UnknownProvider(t *testing.T) {
	_, err := providers.New("no-such-provider-abc", "http://x", "key")
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestRegistry" -v 2>&1 | head -10
```
Expected: compile error — `providers.Register`, `providers.New`, `providers.Adapter` undefined.

- [ ] **Step 3: Create registry.go**

```go
// gateway/providers/registry.go
package providers

import (
	"context"
	"fmt"
)

// Adapter is the interface every LLM provider must implement.
type Adapter interface {
	Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error)
	// BuildFilePrompt constructs the full request body to send to the provider
	// when the user uploads a file. Returns nil for providers that do not
	// support file uploads (e.g. local).
	BuildFilePrompt(model, docText, userMessage string) []byte
}

// AdapterFactory creates an Adapter for a given base URL and API key.
type AdapterFactory func(baseURL, apiKey string) Adapter

var registry = map[string]AdapterFactory{}

// Register adds a provider factory to the registry. Called from init() in each
// adapter file so new providers can be added without touching main.go.
func Register(providerType string, factory AdapterFactory) {
	registry[providerType] = factory
}

// New looks up a provider by type and returns a fresh Adapter.
func New(providerType, baseURL, apiKey string) (Adapter, error) {
	factory, ok := registry[providerType]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %q", providerType)
	}
	return factory(baseURL, apiKey), nil
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestRegistry" -v
```
Expected: 2 tests PASS.

- [ ] **Step 5: Verify gateway builds**

```bash
cd /path/to/worktree/gateway && go build ./...
```
Expected: no errors. (Existing adapters don't implement `Adapter` yet, but nothing forces them to until `providers.New()` is used.)

- [ ] **Step 6: Commit**

```bash
git add gateway/providers/registry.go gateway/providers/registry_test.go
git commit -m "feat(gateway): Adapter interface + provider registry — Register/New for pluggable LLM providers"
```

---

## Task 2: Update OpenAI Adapter — `init()` + `BuildFilePrompt`

**Files:**
- Modify: `gateway/providers/openai.go`
- Modify: `gateway/providers/openai_test.go`

- [ ] **Step 1: Write failing test**

Add to `gateway/providers/openai_test.go`:

```go
func TestOpenAIAdapter_BuildFilePrompt(t *testing.T) {
	a := providers.NewOpenAIAdapter("http://x", "key")
	body := a.BuildFilePrompt("gpt-4o", "doc content here", "summarize it")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "gpt-4o", got["model"])
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].(map[string]interface{})["role"])
	assert.Equal(t, "user", msgs[1].(map[string]interface{})["role"])
}
```

Add `"encoding/json"` to the import block in `openai_test.go`.

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestOpenAIAdapter_BuildFilePrompt" -v 2>&1 | head -10
```
Expected: compile error — `BuildFilePrompt` method not found.

- [ ] **Step 3: Add `init()` and `BuildFilePrompt` to openai.go**

At the top of `gateway/providers/openai.go`, after `package providers`, add:

```go
import "encoding/json"
```

(Merge with the existing import block.)

Add these two functions at the bottom of `gateway/providers/openai.go`:

```go
func init() {
	Register("openai", func(baseURL, apiKey string) Adapter {
		return NewOpenAIAdapter(baseURL, apiKey)
	})
}

func (a *OpenAIAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "以下是用户上传的文档内容：\n\n" + docText},
			{"role": "user", "content": userMessage},
		},
	}
	b, _ := json.Marshal(body)
	return b
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestOpenAIAdapter" -v
```
Expected: all OpenAI tests PASS.

- [ ] **Step 5: Commit**

```bash
git add gateway/providers/openai.go gateway/providers/openai_test.go
git commit -m "feat(gateway): OpenAI adapter — init() registration + BuildFilePrompt"
```

---

## Task 3: Update Anthropic Adapter — `init()` + `BuildFilePrompt`

**Files:**
- Modify: `gateway/providers/anthropic.go`
- Modify: `gateway/providers/anthropic_test.go`

- [ ] **Step 1: Write failing test**

Add to `gateway/providers/anthropic_test.go`:

```go
func TestAnthropicAdapter_BuildFilePrompt(t *testing.T) {
	a := providers.NewAnthropicAdapter("http://x", "key")
	body := a.BuildFilePrompt("claude-3-5-sonnet-20241022", "doc content", "summarize")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "claude-3-5-sonnet-20241022", got["model"])
	assert.Contains(t, got["system"].(string), "doc content")
	msgs := got["messages"].([]interface{})
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].(map[string]interface{})["role"])
}
```

Add `"encoding/json"` to the import block in `anthropic_test.go`.

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestAnthropicAdapter_BuildFilePrompt" -v 2>&1 | head -5
```
Expected: compile error.

- [ ] **Step 3: Add `init()` and `BuildFilePrompt` to anthropic.go**

Add `"encoding/json"` to the import block. Add at bottom of `gateway/providers/anthropic.go`:

```go
func init() {
	Register("anthropic", func(baseURL, apiKey string) Adapter {
		return NewAnthropicAdapter(baseURL, apiKey)
	})
}

func (a *AnthropicAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model":  model,
		"system": "以下是用户上传的文档内容：\n\n" + docText,
		"messages": []map[string]string{
			{"role": "user", "content": userMessage},
		},
	}
	b, _ := json.Marshal(body)
	return b
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestAnthropicAdapter" -v
```
Expected: all Anthropic tests PASS.

- [ ] **Step 5: Commit**

```bash
git add gateway/providers/anthropic.go gateway/providers/anthropic_test.go
git commit -m "feat(gateway): Anthropic adapter — init() registration + BuildFilePrompt"
```

---

## Task 4: Update Local Adapter — `init()` + `BuildFilePrompt`

**Files:**
- Modify: `gateway/providers/local.go`

No new test file — `BuildFilePrompt` returns nil for local (no file upload support). Test is covered by the handler tests in Task 9 (local model → 400).

- [ ] **Step 1: Add `init()` and `BuildFilePrompt` to local.go**

Add at bottom of `gateway/providers/local.go`:

```go
func init() {
	Register("local", func(baseURL, _ string) Adapter {
		return NewLocalAdapter(baseURL)
	})
}

// BuildFilePrompt returns nil — local models run on-prem so file upload
// scanning is unnecessary. The FileChatHandler checks for nil and returns 400.
func (a *LocalAdapter) BuildFilePrompt(_, _, _ string) []byte { return nil }
```

- [ ] **Step 2: Build and run all provider tests**

```bash
cd /path/to/worktree/gateway && go build ./... && go test ./providers/ -v
```
Expected: build clean, all tests PASS.

- [ ] **Step 3: Commit**

```bash
git add gateway/providers/local.go
git commit -m "feat(gateway): Local adapter — init() registration + BuildFilePrompt (no-op)"
```

---

## Task 5: Gemini Adapter

**Files:**
- Create: `gateway/providers/gemini.go`
- Create: `gateway/providers/gemini_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/providers/gemini_test.go
package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/providers"
)

func TestGeminiAdapter_Forward(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "gemini-1.5-pro")
		assert.Contains(t, r.URL.RawQuery, "key=test-gemini-key")
		assert.Equal(t, "", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5}}`))
	}))
	defer upstream.Close()

	a := providers.NewGeminiAdapter(upstream.URL, "test-gemini-key")
	body := `{"model":"gemini-1.5-pro","contents":[{"role":"user","parts":[{"text":"Hi"}]}]}`
	resp, usage, err := a.Forward(context.Background(), []byte(body))
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	assert.Equal(t, 10, usage.PromptTokens)
	assert.Equal(t, 5, usage.CompletionTokens)
}

func TestGeminiAdapter_BuildFilePrompt(t *testing.T) {
	a := providers.NewGeminiAdapter("http://x", "key")
	body := a.BuildFilePrompt("gemini-1.5-pro", "doc content", "summarize")
	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, "gemini-1.5-pro", got["model"])
	contents := got["contents"].([]interface{})
	require.Len(t, contents, 1)
	parts := contents[0].(map[string]interface{})["parts"].([]interface{})
	text := parts[0].(map[string]interface{})["text"].(string)
	assert.Contains(t, text, "doc content")
	assert.Contains(t, text, "summarize")
}

func TestGeminiAdapter_RegistryLookup(t *testing.T) {
	adapter, err := providers.New("gemini", "http://x", "key")
	require.NoError(t, err)
	assert.NotNil(t, adapter)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestGemini" -v 2>&1 | head -10
```
Expected: compile error — `providers.NewGeminiAdapter` undefined.

- [ ] **Step 3: Create gemini.go**

```go
// gateway/providers/gemini.go
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func init() {
	Register("gemini", func(baseURL, apiKey string) Adapter {
		return NewGeminiAdapter(baseURL, apiKey)
	})
}

type GeminiAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewGeminiAdapter(baseURL, apiKey string) *GeminiAdapter {
	return &GeminiAdapter{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

// Forward sends the request to Gemini. The model name is extracted from the
// request body and embedded in the URL path, as required by the Gemini API.
// Auth uses ?key= query parameter (no Bearer token).
func (a *GeminiAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Model == "" {
		return nil, nil, fmt.Errorf("gemini: model field missing in request body")
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", a.baseURL, req.Model, a.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: forward: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("gemini: read response: %w", err)
	}
	return &ForwardResult{StatusCode: resp.StatusCode, Headers: resp.Header, Body: respBody},
		extractGeminiUsage(respBody), nil
}

func (a *GeminiAdapter) BuildFilePrompt(model, docText, userMessage string) []byte {
	body := map[string]interface{}{
		"model": model,
		"contents": []map[string]interface{}{
			{"role": "user", "parts": []map[string]string{
				{"text": "以下是用户上传的文档内容：\n\n" + docText + "\n\n" + userMessage},
			}},
		},
	}
	b, _ := json.Marshal(body)
	return b
}

type geminiResponse struct {
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
}

func extractGeminiUsage(body []byte) *Usage {
	var r geminiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return &Usage{}
	}
	return &Usage{
		PromptTokens:     r.UsageMetadata.PromptTokenCount,
		CompletionTokens: r.UsageMetadata.CandidatesTokenCount,
	}
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -run "TestGemini" -v
```
Expected: 3 tests PASS.

- [ ] **Step 5: Run all provider tests**

```bash
cd /path/to/worktree/gateway && go test ./providers/ -v 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add gateway/providers/gemini.go gateway/providers/gemini_test.go
git commit -m "feat(gateway): Gemini adapter — Forward + BuildFilePrompt + registry init()"
```

---

## Task 6: Refactor gateway/main.go — Replace switch with Registry

**Files:**
- Modify: `gateway/main.go`

- [ ] **Step 1: Read gateway/main.go**

Read the full file to understand the current structure. The switch block to replace is inside `makeProxyHandler`, roughly:

```go
var fwd interface {
    Forward(ctx context.Context, body []byte) (*providers.ForwardResult, *providers.Usage, error)
}
switch modelCfg.Provider {
case "openai":
    fwd = providers.NewOpenAIAdapter(modelCfg.BaseURL, modelCfg.APIKey)
case "anthropic":
    fwd = providers.NewAnthropicAdapter(modelCfg.BaseURL, modelCfg.APIKey)
case "local":
    fwd = providers.NewLocalAdapter(modelCfg.BaseURL)
default:
    return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
        "message": fmt.Sprintf("model %q not configured for your tenant", reqBody.Model),
    }})
}
```

- [ ] **Step 2: Replace switch with registry lookup**

Replace the block above with:

```go
fwd, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
if err != nil {
    return c.Status(400).JSON(fiber.Map{"error": fiber.Map{
        "message": fmt.Sprintf("unsupported provider %q for model %q", modelCfg.Provider, reqBody.Model),
    }})
}
```

Remove any `err` variable that was previously declared above this block to avoid redeclaration errors. The variable `fwd` is now of type `providers.Adapter` (returned by `providers.New`), so update any usage of `fwd` to call `fwd.Forward(...)` directly.

- [ ] **Step 3: Build to confirm no errors**

```bash
cd /path/to/worktree/gateway && go build ./...
```
Expected: clean build.

- [ ] **Step 4: Run all gateway tests**

```bash
cd /path/to/worktree/gateway && go test ./... -count=1 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add gateway/main.go
git commit -m "refactor(gateway): replace provider switch with providers.New() registry lookup"
```

---

## Task 7: Config ParserURL + ParserClient

**Files:**
- Modify: `gateway/config/config.go`
- Create: `gateway/storage/parser_client.go`
- Create: `gateway/storage/parser_client_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/storage/parser_client_test.go
package storage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/gateway/storage"
)

func TestParserClient_Parse_PDF(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/parse", r.URL.Path)
		r.ParseMultipartForm(1 << 20)
		_, header, err := r.FormFile("file")
		require.NoError(t, err)
		assert.Equal(t, "test.pdf", header.Filename)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"extracted content","page_count":3}`))
	}))
	defer srv.Close()

	client := storage.NewParserClient(srv.URL)
	result, err := client.Parse(context.Background(), "test.pdf", []byte("fake pdf bytes"))
	require.NoError(t, err)
	assert.Equal(t, "extracted content", result.Text)
	assert.Equal(t, 3, result.PageCount)
}

func TestParserClient_Parse_UnsupportedFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"unsupported format"}`))
	}))
	defer srv.Close()

	client := storage.NewParserClient(srv.URL)
	_, err := client.Parse(context.Background(), "test.xls", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format")
}

func TestParserClient_Parse_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer srv.Close()

	client := storage.NewParserClient(srv.URL)
	_, err := client.Parse(context.Background(), "test.pdf", []byte("data"))
	require.Error(t, err)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./storage/ -run "TestParserClient" -v 2>&1 | head -10
```
Expected: compile error — `storage.NewParserClient`, `storage.ParseResult` undefined.

- [ ] **Step 3: Add ParserURL to config**

In `gateway/config/config.go`, add `ParserURL string` to the `Config` struct, and in `Load()` add:

```go
ParserURL: getEnv("PARSER_URL", "http://localhost:8090"),
```

- [ ] **Step 4: Create parser_client.go**

```go
// gateway/storage/parser_client.go
package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"time"
)

type ParseResult struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
}

type ParserClient struct {
	baseURL string
	client  *http.Client
}

func NewParserClient(baseURL string) *ParserClient {
	return &ParserClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Parse sends a file to the parser microservice and returns extracted text.
// Returns an error containing "unsupported format" for 400 responses.
func (c *ParserClient) Parse(ctx context.Context, filename string, data []byte) (*ParseResult, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filepath.Base(filename))
	if err != nil {
		return nil, fmt.Errorf("parser: create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("parser: write data: %w", err)
	}
	w.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/parse", &buf)
	if err != nil {
		return nil, fmt.Errorf("parser: create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("parser: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parser: read response: %w", err)
	}

	if resp.StatusCode == http.StatusBadRequest {
		return nil, fmt.Errorf("unsupported format")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("parser: status %d: %s", resp.StatusCode, respBody)
	}

	var result ParseResult
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parser: decode response: %w", err)
	}
	return &result, nil
}
```

- [ ] **Step 5: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./storage/ -run "TestParserClient" -v
```
Expected: 3 tests PASS.

- [ ] **Step 6: Build**

```bash
cd /path/to/worktree/gateway && go build ./...
```
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add gateway/config/config.go gateway/storage/parser_client.go gateway/storage/parser_client_test.go
git commit -m "feat(gateway): ParserClient + config.ParserURL — HTTP client for Python parser microservice"
```

---

## Task 8: Export ScanForPII from Middleware

**Files:**
- Modify: `gateway/middleware/pii.go`
- Modify: `gateway/middleware/pii_test.go`

- [ ] **Step 1: Write failing tests**

Add to `gateway/middleware/pii_test.go`:

```go
func TestScanForPII_Phone(t *testing.T) {
	piiType, found := middleware.ScanForPII("please call 13812345678 for details")
	assert.True(t, found)
	assert.Equal(t, "china_phone", piiType)
}

func TestScanForPII_Clean(t *testing.T) {
	_, found := middleware.ScanForPII("this text contains no sensitive information")
	assert.False(t, found)
}

func TestScanForPII_IDCard(t *testing.T) {
	piiType, found := middleware.ScanForPII("身份证: 110101199001011234")
	assert.True(t, found)
	assert.Equal(t, "china_id_card", piiType)
}
```

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./middleware/ -run "TestScanForPII" -v 2>&1 | head -10
```
Expected: compile error — `middleware.ScanForPII` undefined.

- [ ] **Step 3: Add ScanForPII to pii.go**

Add at the bottom of `gateway/middleware/pii.go`:

```go
// ScanForPII scans text against all PII patterns. Returns the matched PII type
// name and true on the first match; returns ("", false) if no PII is found.
func ScanForPII(text string) (piiType string, found bool) {
	for _, rule := range piiPatterns {
		if rule.re.MatchString(text) {
			return rule.name, true
		}
	}
	return "", false
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./middleware/ -v 2>&1 | tail -10
```
Expected: all tests PASS including the 3 new ones.

- [ ] **Step 5: Commit**

```bash
git add gateway/middleware/pii.go gateway/middleware/pii_test.go
git commit -m "feat(gateway): export ScanForPII — reusable PII scanning for file upload handler"
```

---

## Task 9: FileChatHandler

**Files:**
- Create: `gateway/handlers/file_chat.go`
- Create: `gateway/handlers/file_chat_test.go`

- [ ] **Step 1: Write failing tests**

```go
// gateway/handlers/file_chat_test.go
package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
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

type stubParser struct{ result *storage.ParseResult; err error }

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
```

Add `"fmt"` to the import block.

- [ ] **Step 2: Run to confirm FAIL**

```bash
cd /path/to/worktree/gateway && go test ./handlers/ -run "TestFileChatHandler" -v 2>&1 | head -10
```
Expected: compile error — `handlers` package does not exist.

- [ ] **Step 3: Create file_chat.go**

```go
// gateway/handlers/file_chat.go
package handlers

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/providers"
	"github.com/yourorg/totra/gateway/storage"
	"github.com/yourorg/totra/gateway/tokenizer"
)

const maxFileSizeBytes = 20 * 1024 * 1024 // 20 MB

// ModelLookup is satisfied by *storage.PGModelLookup.
type ModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// FileParser is satisfied by *storage.ParserClient.
type FileParser interface {
	Parse(ctx context.Context, filename string, data []byte) (*storage.ParseResult, error)
}

// UsageRecorder is satisfied by *storage.UsageStore.
type UsageRecorder interface {
	Record(r *storage.UsageRecord)
}

func NewFileChatHandler(
	modelLookup ModelLookup,
	parser FileParser,
	piiRecorder middleware.ViolationRecorder,
	usageRecorder UsageRecorder,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user := c.Locals("user").(*middleware.UserInfo)

		fileHeader, err := c.FormFile("file")
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "file field required", "type": "bad_request",
			}})
		}
		if fileHeader.Size > maxFileSizeBytes {
			return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{"error": fiber.Map{
				"message": "file exceeds 20MB limit", "type": "file_too_large",
			}})
		}

		message := c.FormValue("message")
		modelName := c.FormValue("model")
		if message == "" || modelName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "message and model fields required", "type": "bad_request",
			}})
		}

		modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": fmt.Sprintf("model %q not configured", modelName), "type": "model_not_found",
			}})
		}

		if modelCfg.Provider == "local" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "local models do not support file uploads", "type": "local_model_not_supported",
			}})
		}

		adapter, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
				"message": "unsupported provider: " + modelCfg.Provider, "type": "unsupported_provider",
			}})
		}

		f, err := fileHeader.Open()
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fiber.Map{
				"message": "failed to open file", "type": "internal_error",
			}})
		}
		defer f.Close()
		fileBytes, err := io.ReadAll(f)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": fiber.Map{
				"message": "failed to read file", "type": "internal_error",
			}})
		}

		parseResult, err := parser.Parse(c.Context(), fileHeader.Filename, fileBytes)
		if err != nil {
			if strings.Contains(err.Error(), "unsupported format") {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fiber.Map{
					"message": "unsupported file format (PDF, DOCX, PPTX only)", "type": "unsupported_format",
				}})
			}
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": fiber.Map{
				"message": "parser error: " + err.Error(), "type": "parser_error",
			}})
		}

		if piiType, found := middleware.ScanForPII(parseResult.Text); found {
			piiRecorder.RecordViolation(user.TenantID, user.UserID, piiType, "blocked", "/v1/files/chat")
			return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{"error": fiber.Map{
				"message": "file blocked: PII detected (" + piiType + ")", "type": "pii_blocked",
			}})
		}

		start := time.Now()
		body := adapter.BuildFilePrompt(modelName, parseResult.Text, message)
		result, usage, err := adapter.Forward(c.Context(), body)
		if err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": fiber.Map{
				"message": "upstream error: " + err.Error(), "type": "upstream_error",
			}})
		}

		responseMS := int(time.Since(start).Milliseconds())
		usageRecorder.Record(&storage.UsageRecord{
			TenantID:         user.TenantID,
			UserID:           user.UserID,
			ModelConfigID:    modelCfg.ID,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			SCUCost:          tokenizer.ToSCU(usage.PromptTokens, usage.CompletionTokens, modelCfg.SCURate),
			USDCost:          0,
			ResponseMS:       responseMS,
		})

		for k, vs := range result.Headers {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}
```

- [ ] **Step 4: Run tests to confirm PASS**

```bash
cd /path/to/worktree/gateway && go test ./handlers/ -run "TestFileChatHandler" -v
```
Expected: 4 tests PASS.

- [ ] **Step 5: Build entire gateway**

```bash
cd /path/to/worktree/gateway && go build ./...
```
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add gateway/handlers/file_chat.go gateway/handlers/file_chat_test.go
git commit -m "feat(gateway): FileChatHandler — upload → parse → PII scan → forward to LLM"
```

---

## Task 10: Parser Microservice + Wire Everything

**Files:**
- Create: `parser/main.py`
- Create: `parser/requirements.txt`
- Create: `parser/Dockerfile`
- Modify: `gateway/main.go` (add `/v1/files/chat` route)
- Modify: `docker-compose.yml`
- Modify: `.env`

- [ ] **Step 1: Create parser/requirements.txt**

```
fastapi==0.115.5
uvicorn==0.34.0
pdfplumber==0.11.4
python-docx==1.1.2
python-pptx==1.0.2
```

- [ ] **Step 2: Create parser/main.py**

```python
# parser/main.py
import io

from fastapi import FastAPI, File, HTTPException, UploadFile

app = FastAPI()


@app.post("/parse")
async def parse_file(file: UploadFile = File(...)):
    filename = (file.filename or "").lower()
    data = await file.read()

    if filename.endswith(".pdf"):
        text, page_count = _parse_pdf(data)
    elif filename.endswith(".docx"):
        text, page_count = _parse_docx(data)
    elif filename.endswith(".pptx"):
        text, page_count = _parse_pptx(data)
    else:
        raise HTTPException(status_code=400, detail="unsupported format")

    return {"text": text, "page_count": page_count}


def _parse_pdf(data: bytes) -> tuple[str, int]:
    import pdfplumber

    parts = []
    page_count = 0
    with pdfplumber.open(io.BytesIO(data)) as pdf:
        page_count = len(pdf.pages)
        for page in pdf.pages:
            text = page.extract_text()
            if text:
                parts.append(text)
    return "\n".join(parts), page_count


def _parse_docx(data: bytes) -> tuple[str, int]:
    from docx import Document

    doc = Document(io.BytesIO(data))
    text = "\n".join(p.text for p in doc.paragraphs if p.text.strip())
    return text, 1


def _parse_pptx(data: bytes) -> tuple[str, int]:
    from pptx import Presentation

    prs = Presentation(io.BytesIO(data))
    slides = []
    for slide in prs.slides:
        texts = [
            shape.text
            for shape in slide.shapes
            if hasattr(shape, "text") and shape.text.strip()
        ]
        slides.append("\n".join(texts))
    return "\n\n".join(slides), len(prs.slides)
```

- [ ] **Step 3: Create parser/Dockerfile**

```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY main.py .
EXPOSE 8090
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8090"]
```

- [ ] **Step 4: Add parser service to docker-compose.yml**

Add after the `redis` service block (before `gateway`):

```yaml
  parser:
    build: ./parser
    ports:
      - "${PARSER_PORT:-8090}:8090"
    profiles: ["app"]
```

- [ ] **Step 5: Add PARSER_PORT to .env**

Add to `.env`:
```
PARSER_PORT=8090
PARSER_URL=http://parser:8090
```

- [ ] **Step 6: Wire /v1/files/chat into gateway/main.go**

Read `gateway/main.go`. Add to the imports if not present:
```go
"github.com/yourorg/totra/gateway/handlers"
```

In `main()`, after the `policyRuleStore` initialization and before `app := fiber.New(...)`, or after `proxyHandler` setup, add:

```go
parserClient := storage.NewParserClient(cfg.ParserURL)
fileLookup := storage.NewPGModelLookup(pool)
```

After the `v1` group and `v1.Post("/messages", ...)` lines, add:

```go
app.Post("/v1/files/chat",
    middleware.NewAuthMiddleware(pgUserLookup),
    middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
    handlers.NewFileChatHandler(fileLookup, parserClient, piiStore, usageStore),
)
```

- [ ] **Step 7: Build gateway**

```bash
cd /path/to/worktree/gateway && go build ./...
```
Expected: clean.

- [ ] **Step 8: Run all gateway tests**

```bash
cd /path/to/worktree/gateway && go test ./... -count=1 2>&1 | tail -15
```
Expected: all PASS.

- [ ] **Step 9: Commit**

```bash
git add parser/main.py parser/requirements.txt parser/Dockerfile \
        docker-compose.yml .env gateway/main.go
git commit -m "feat: parser microservice + wire /v1/files/chat into gateway"
```

---

## Self-Review

**Spec coverage:**
- ✅ PDF/DOCX/PPTX parsing via Python microservice — Task 10
- ✅ PII detection on extracted text, 422 block — Tasks 8, 9
- ✅ `providers.Adapter` interface + `BuildFilePrompt` — Tasks 1–5
- ✅ Gemini adapter (new provider) — Task 5
- ✅ Registry replaces hard-coded switch — Task 6
- ✅ OpenAI, Anthropic, DeepSeek (via openai), Gemini all covered — Tasks 2–5
- ✅ Local model returns 400 — Tasks 4, 9
- ✅ File > 20MB → 413 — Task 9 handler
- ✅ Unsupported format → 400 — Tasks 7, 9
- ✅ Parser timeout (30s) built into `ParserClient` — Task 7
- ✅ Parser error → 502 — Task 9
- ✅ Usage recorded after successful forward — Task 9
- ✅ docker-compose parser service + PARSER_URL — Task 10

**Placeholder scan:** None found.

**Type consistency:**
- `handlers.ModelLookup.GetByName` signature matches `storage.PGModelLookup.GetByName` — ✅
- `handlers.FileParser.Parse` returns `*storage.ParseResult` — matches `storage.ParserClient.Parse` — ✅
- `handlers.UsageRecorder.Record` matches `storage.UsageStore.Record` — ✅
- `providers.Adapter.BuildFilePrompt(model, docText, userMessage string)` — consistent across Tasks 1–5 and used in Task 9 — ✅
- `middleware.ViolationRecorder` interface defined in `middleware/pii.go`, implemented by `*storage.PIIStore` — unchanged — ✅
