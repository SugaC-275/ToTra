# File Upload + PII Scanning Design Spec

**Date:** 2026-05-15  
**Feature:** 文件上传合规扫描 — 支持 PDF/DOCX/PPT，PII 检测，适配所有主流 LLM

---

## Goal

员工上传文件（PDF/DOCX/PPT）并附带问题，网关提取文件文本，扫描 PII，发现敏感内容直接拦截；文件干净则将文本注入 prompt，转发给 AI。支持所有主流云端 LLM provider，本地模型不参与（数据不出公司，无需扫描）。

---

## Architecture

```
Client → POST /v1/files/chat (multipart: file + message + model)
           ↓
      [ Gateway :8082 ]
           ↓
      1. Auth + Quota middleware（现有）
           ↓
      2. FileChatHandler
           ├─ provider == "local" → 400
           ├─ file > 20MB → 413
           ├─ 调用 Parser 微服务提取文本（超时 30s）
           │     ├─ 不支持格式 → 400
           │     └─ 超时/错误 → 502
           ├─ PII 扫描提取文本（复用现有正则引擎）
           │     └─ 有 PII → 422 + 写 pii_violations
           ├─ adapter.BuildFilePrompt(docText, userMessage)
           └─ 转发 AI + 记录 usage
           ↓
      [ Parser :8090 ] ← FastAPI + pdfplumber + python-docx + python-pptx
```

---

## Phase 1 — Provider Registry Refactor

### Problem

当前 `gateway/main.go` 用 `switch modelCfg.Provider` 硬编码三种 provider。每加新 LLM 必须改代码。

### Solution

引入 `Adapter` 接口 + 注册表，所有 provider 统一实现：

```go
// gateway/providers/registry.go
type Adapter interface {
    Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error)
    BuildFilePrompt(model, docText, userMessage string) []byte
}

type AdapterFactory func(baseURL, apiKey string) Adapter

var registry = map[string]AdapterFactory{}

func Register(providerType string, factory AdapterFactory) {
    registry[providerType] = factory
}

func New(providerType, baseURL, apiKey string) (Adapter, error) {
    factory, ok := registry[providerType]
    if !ok {
        return nil, fmt.Errorf("unknown provider: %q", providerType)
    }
    return factory(baseURL, apiKey), nil
}
```

注册在各 adapter 文件的 `init()` 中：
```go
// gateway/providers/openai.go
func init() { Register("openai", func(u, k string) Adapter { return NewOpenAIAdapter(u, k) }) }
```

### Provider Coverage

| provider 字段 | Adapter | 覆盖模型 |
|--------------|---------|---------|
| `openai` | OpenAIAdapter | GPT-4/3.5, DeepSeek, Mistral, Groq, Together AI, Qwen, Yi, 任何 OpenAI 兼容 API |
| `anthropic` | AnthropicAdapter | Claude 全系列 |
| `gemini` | GeminiAdapter（新增）| Gemini 全系列 |
| `local` | LocalAdapter | Ollama 等本地模型 |

DeepSeek / Mistral / Groq 等：数据库 `provider` 字段填 `openai`，`base_url` 指向各自端点，零代码改动。

### BuildFilePrompt Implementations

**OpenAI（含 DeepSeek / Mistral 等兼容）：**
```go
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

**Anthropic：**
```go
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

**Gemini：**
```go
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
```

**Local：**
```go
func (a *LocalAdapter) BuildFilePrompt(_, _, _ string) []byte { return nil }
// 返回 nil 表示不支持，FileChatHandler 检查此值返回 400
```

---

## Phase 2 — File Upload + PII Scanning

### Parser Microservice

新增 Docker 容器，职责单一：接收文件，返回提取文本。

```
parser/
├── main.py          # FastAPI app
├── requirements.txt # fastapi uvicorn pdfplumber python-docx python-pptx
└── Dockerfile       # python:3.12-slim
```

**API：**
```
POST http://parser:8090/parse
Content-Type: multipart/form-data
Body: file=<binary>

Response 200: { "text": "...", "page_count": 12 }
Response 400: { "error": "unsupported format" }
Response 422: { "error": "failed to parse file" }
```

支持格式：`.pdf`（pdfplumber）、`.docx`（python-docx）、`.pptx`（python-pptx）。

### Gateway: ParserClient

```go
// gateway/storage/parser_client.go
type ParserClient struct{ baseURL string; client *http.Client }

type ParseResult struct {
    Text      string `json:"text"`
    PageCount int    `json:"page_count"`
}

func NewParserClient(baseURL string) *ParserClient {
    return &ParserClient{
        baseURL: baseURL,
        client:  &http.Client{Timeout: 30 * time.Second},
    }
}

func (c *ParserClient) Parse(ctx context.Context, filename string, data []byte) (*ParseResult, error)
```

### Gateway: FileChatHandler

```go
// gateway/handlers/file_chat.go
// POST /v1/files/chat (multipart: file, message, model)
func NewFileChatHandler(
    modelLookup ModelLookup,
    parser *storage.ParserClient,
    piiRecorder middleware.ViolationRecorder,
    usageStore *storage.UsageStore,
) fiber.Handler
```

**处理步骤：**
1. 读取 multipart：`file`、`message`、`model`
2. 文件大小 > 20MB → 413
3. 查 model 配置 → 获取 provider 类型
4. provider == `local` → 400
5. 用注册表创建 adapter
6. `adapter.BuildFilePrompt` 返回 nil → 400（local 路径）
7. 调用 `parser.Parse()` 提取文本
8. 用现有 PII 正则扫描提取文本
9. 有 PII → 422 + `piiRecorder.RecordViolation()`
10. `adapter.BuildFilePrompt(docText, message)` 构建请求体（注入 model 字段）
11. `adapter.Forward(ctx, body)` 转发 AI
12. 记录 usage

### New Route in main.go

```go
// 注册在认证中间件之后，独立于 /v1 group
app.Post("/v1/files/chat",
    middleware.NewAuthMiddleware(pgUserLookup),
    middleware.NewQuotaMiddleware(quotaStore, pgUserQuota),
    handlers.NewFileChatHandler(modelLookup, parserClient, piiStore, usageStore),
)
```

### docker-compose.yml 新增

```yaml
parser:
  build: ./parser
  ports:
    - "${PARSER_PORT:-8090}:8090"
  profiles: ["app"]
```

Gateway 环境变量新增：`PARSER_URL=http://parser:8090`

---

## Error Responses

| 情况 | HTTP | error.type |
|------|------|------------|
| 文件超 20MB | 413 | `file_too_large` |
| 不支持格式 | 400 | `unsupported_format` |
| 本地模型 | 400 | `local_model_not_supported` |
| PII 检测到 | 422 | `pii_blocked` |
| Parser 超时/失败 | 502 | `parser_error` |
| Model 未配置 | 400 | `model_not_found` |

---

## Files Created / Modified

| 操作 | 文件 |
|------|------|
| 新建 | `gateway/providers/registry.go` |
| 新建 | `gateway/providers/gemini.go` |
| 新建 | `gateway/handlers/file_chat.go` |
| 新建 | `gateway/storage/parser_client.go` |
| 新建 | `parser/main.py` |
| 新建 | `parser/requirements.txt` |
| 新建 | `parser/Dockerfile` |
| 修改 | `gateway/providers/openai.go` — 加 `init()` 注册 + `BuildFilePrompt` |
| 修改 | `gateway/providers/anthropic.go` — 加 `init()` 注册 + `BuildFilePrompt` |
| 修改 | `gateway/providers/local.go` — 加 `init()` 注册 + `BuildFilePrompt` nil |
| 修改 | `gateway/main.go` — 用注册表替换 switch，注册 `/v1/files/chat` |
| 修改 | `gateway/config/config.go` — 新增 `ParserURL` 字段 |
| 修改 | `docker-compose.yml` — 新增 parser 服务 |
| 修改 | `.env` — 新增 `PARSER_PORT` |

---

## Out of Scope

- 文件存储（不落盘）
- 脱敏放行（只拦截）
- 扫描 AI 返回内容
- Dashboard 文件上传 UI（本 spec 只覆盖 API 层）
