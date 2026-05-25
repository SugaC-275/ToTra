# ToTra vs LiteLLM — Gap Analysis & Implementation Roadmap

> 基于代码精确分析，持续更新至 2026-05-25。✅ 已实现，❌ 缺失，🚫 暂不计划。

---

## 完整功能对比（当前状态）

| 能力 | ToTra | LiteLLM | 备注 |
|------|-------|---------|------|
| Chat completions | ✅ | ✅ | |
| Streaming | ✅ | ✅ | |
| Embeddings `/v1/embeddings` | ✅ | ✅ | |
| Audio transcriptions `/v1/audio/transcriptions` | ✅ | ✅ | Whisper-compatible |
| Image generation `/v1/images/generations` | ✅ | ✅ | DALL-E/Stability |
| Reranking `/v1/rerank` | ✅ | ✅ | Cohere/Jina compatible |
| Moderation `/v1/moderations` | ✅ | ✅ | OpenAI moderation proxy |
| Batch API `/v1/batches` | ✅ | ✅ | async worker + PostgreSQL queue |
| MCP Gateway `/v1/mcp/chat` | ✅ | ❌ | ToTra独有 |
| Provider: OpenAI | ✅ | ✅ | |
| Provider: Anthropic | ✅ | ✅ | |
| Provider: Gemini | ✅ | ✅ | |
| Provider: Azure OpenAI | ✅ | ✅ | |
| Provider: AWS Bedrock | ✅ | ✅ | SigV4 auto-auth |
| Provider: Mistral/Groq/Ollama | ✅ | ✅ | via openai adapter + baseURL |
| Cost tracking & analytics | ✅ | ✅ | ToTra有更多维度 |
| Rate limiting | ✅ | ✅ | Redis-based |
| Per-user quotas | ✅ | ✅ | |
| Exact cache | ✅ | ✅ | Redis |
| Semantic cache | ✅ | ✅ | SimHash LSH (ToTra独有) |
| Prompt compression | ✅ | ❌ | ToTra独有 |
| Fallback models | ✅ | ✅ | |
| Auto-router (cost/complexity) | ✅ | ✅ | |
| Circuit breaker / cooldown | ✅ | ✅ | Redis TTL-based |
| Retry with backoff | ✅ | ✅ | |
| Context window fallback | ✅ | ✅ | pre-call token estimation |
| PII detection (regex, 18 language groups) | ✅ | ❌ | ToTra独有 |
| PII detection (semantic via Presidio) | ✅ | ❌ | ToTra独有，可选sidecar |
| Post-call PII response scan | ✅ | ❌ | ToTra独有 |
| Prompt injection detection | ✅ | ❌ | ToTra独有 |
| Policy rules | ✅ | ❌ | ToTra独有 |
| SIEM integration | ✅ | ❌ | ToTra独有 |
| GDPR / data retention | ✅ | ❌ | ToTra独有 |
| Audit log (hash-chained) | ✅ | ❌ | ToTra独有 |
| RBAC | ✅ | ✅ (Enterprise) | |
| SSO/OIDC | ✅ | ✅ (Enterprise) | |
| IP allowlist | ✅ | ✅ (Enterprise) | |
| OpenTelemetry tracing | ✅ | ✅ | |
| Langfuse callbacks | ✅ | ✅ | async post-request |
| Datadog metrics callbacks | ✅ | ✅ | async post-request |
| Prometheus metrics | ✅ | ✅ | |
| Python SDK | ✅ | ✅ | sync + async |
| Agent loop (tool calling) | ✅ | ✅ | |
| HR sync | ✅ | ❌ | ToTra独有 |
| Budget alerts & forecasting | ✅ | ✅ (Enterprise) | |
| File chat | ✅ | ❌ | ToTra独有 |
| A2A protocol | 🚫 | 🚫 | LiteLLM也未完整实现 |

**ToTra独有功能（LiteLLM没有）**: MCP Gateway、多语言PII、Presidio语义PII、Post-call PII、Prompt注入检测、SIEM、GDPR合规、Hash-chain审计、语义缓存(SimHash)、Prompt压缩、HR同步、文件聊天

---

## 原始差距分析（8项，已全部完成）

| Gap | 状态 |
|-----|------|
| 1. Provider覆盖 (Azure, Bedrock) | ✅ 2026-05-24 |
| 2. 可观测性 (OTEL + Langfuse/Datadog) | ✅ 2026-05-25 |
| 3. 路由可靠性 (circuit breaker, context fallback) | ✅ 2026-05-24 |
| 4. Guardrails (injection detection, Presidio) | ✅ 2026-05-25 |
| 5. Python SDK | ✅ 2026-05-24 |
| 6. `/v1/embeddings` | ✅ 2026-05-24 |
| 7. MCP Gateway | ✅ 2026-05-24 |
| 8. 负载测试 (k6) | ✅ 2026-05-24 |

## 补充新增端点（LiteLLM parity）

| Endpoint | 状态 |
|----------|------|
| `/v1/audio/transcriptions` | ✅ 2026-05-25 |
| `/v1/images/generations` | ✅ 2026-05-25 |
| `/v1/rerank` | ✅ 2026-05-25 |
| `/v1/moderations` | ✅ 2026-05-25 |
| `/v1/batches` (async batch) | ✅ 2026-05-25 |

---

## 存档：原始差距说明

---

## 现有能力盘点（比想象中强）

| 能力 | 现状 |
|------|------|
| Provider 接口 | ✅ `Adapter` interface + `Register()` 插件注册 + `RetryAdapter` 指数退避 |
| Prometheus `/metrics` | ✅ 已有 5 个指标（requests/duration/tokens/cache/errors），Bearer Token 保护 |
| 路由 | ✅ 复杂度评分自动路由（0-100分，复杂→贵模型，简单→便宜模型） |
| Fallback | ✅ 502/503 自动切换备用模型 |
| Retry | ✅ 指数退避（500ms→5s，最多 N 次） |
| PII 扫描 | ✅ 18 语言组，pre-call，SIEM 事件，`ScanForPII` 函数导出 |
| 语义缓存 | ✅ SimHash LSH |
| Prompt 压缩 | ✅ `compress.go` 中间件 |
| 代理 Endpoint | ✅ `/v1/chat/completions`, `/v1/messages`, stream, `/v1/files/chat` |

---

## 8 个真实差距 & 实现方案

---

### Gap 1 — Provider 覆盖

**差距**: 只有 openai / anthropic / gemini / local。LiteLLM 有 100+。  
**注**: Mistral / Groq / Ollama / LM Studio 都是 OpenAI API 兼容的——只要在模型配置里设置正确的 `baseURL`，直接复用 `openai` adapter 即可，**这些 0 工作量**。

真正需要新写 adapter 的：

#### 1a. Azure OpenAI Adapter

Azure 的 URL 格式与标准 OpenAI 不同：
```
https://{resource}.openai.azure.com/openai/deployments/{deployment}/chat/completions?api-version=2024-05-01-preview
```

```go
// gateway/providers/azure.go
type AzureAdapter struct {
    resourceName   string
    deploymentName string
    apiKey         string
    apiVersion     string
    client         *http.Client
}

func NewAzureAdapter(baseURL, apiKey string) Adapter {
    // baseURL 约定格式: "azure://{resource}/{deployment}"
    // 解析 resource + deployment，组装完整 Azure URL
    parts := parseAzureURL(baseURL) // resource, deployment
    return &AzureAdapter{
        resourceName:   parts.resource,
        deploymentName: parts.deployment,
        apiKey:         apiKey,
        apiVersion:     "2024-05-01-preview",
        client:         &http.Client{},
    }
}

func (a *AzureAdapter) endpoint() string {
    return fmt.Sprintf(
        "https://%s.openai.azure.com/openai/deployments/%s/chat/completions?api-version=%s",
        a.resourceName, a.deploymentName, a.apiVersion,
    )
}

// Forward: 同 OpenAI，但用 api-key header 而非 Authorization: Bearer
func (a *AzureAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
    req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint(), bytes.NewReader(body))
    req.Header.Set("api-key", a.apiKey)
    req.Header.Set("Content-Type", "application/json")
    // ... 其余与 OpenAI adapter 相同
}

func init() {
    Register("azure", func(baseURL, apiKey string) Adapter {
        return NewAzureAdapter(baseURL, apiKey)
    })
}
```

#### 1b. AWS Bedrock Adapter

Bedrock 用 AWS SigV4 签名，不用 API Key。需要 `github.com/aws/aws-sdk-go-v2`。

```go
// gateway/providers/bedrock.go
import (
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

type BedrockAdapter struct {
    client    *bedrockruntime.Client
    modelID   string // e.g. "anthropic.claude-3-5-sonnet-20241022-v2:0"
}

func NewBedrockAdapter(baseURL, _ string) Adapter {
    // baseURL 约定: "bedrock://{region}/{modelID}"
    region, modelID := parseBedrockURL(baseURL)
    cfg, _ := config.LoadDefaultConfig(context.Background(), config.WithRegion(region))
    return &BedrockAdapter{
        client:  bedrockruntime.NewFromConfig(cfg),
        modelID: modelID,
    }
}

func (a *BedrockAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
    // 1. 将 OpenAI 格式 body 转换为 Bedrock InvokeModel 格式
    bedrockBody := openAIToBedrockMessages(body, a.modelID)
    // 2. 调用 bedrockruntime.InvokeModel
    out, err := a.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
        ModelId: &a.modelID,
        Body:    bedrockBody,
    })
    if err != nil { return nil, nil, err }
    // 3. 将 Bedrock 响应转换回 OpenAI 格式
    return bedrockToOpenAIResponse(out.Body)
}

func init() {
    Register("bedrock", func(baseURL, apiKey string) Adapter {
        return NewBedrockAdapter(baseURL, apiKey)
    })
}
```

**已覆盖（0 工作量）**: Mistral, Groq, Together AI, Fireworks, Anyscale, LM Studio, Ollama —— 全部 OpenAI 兼容，设置 baseURL 即可。

---

### Gap 2 — 可观测性

**差距**: `/metrics` 已有！真正缺的是 **OpenTelemetry 分布式 trace**（让 Langfuse/Datadog/Honeycomb 能看到每次 LLM 调用的链路）。

```go
// gateway/middleware/otel.go
import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/attribute"
    "go.opentelemetry.io/otel/trace"
)

func NewOTELMiddleware() fiber.Handler {
    tracer := otel.Tracer("totra.gateway")
    return func(c *fiber.Ctx) error {
        ctx, span := tracer.Start(c.Context(), "llm.request")
        defer span.End()

        err := c.Next()

        // 从已有 locals 提取数据，不重复计算
        if user, ok := c.Locals("user").(*UserInfo); ok && user != nil {
            span.SetAttributes(attribute.String("tenant.id", user.TenantID))
        }
        if model, ok := c.Locals("routed_model").(string); ok {
            span.SetAttributes(attribute.String("llm.model", model))
        }
        span.SetAttributes(
            attribute.Int("http.status_code", c.Response().StatusCode()),
        )
        return err
    }
}
```

通过环境变量 `OTEL_EXPORTER_OTLP_ENDPOINT` 控制导出目标，一行配置对接 Langfuse / Datadog / Honeycomb / Jaeger。

---

### Gap 3 — 路由可靠性

**差距**: 现有 502/503 fallback + 复杂度路由。缺：

#### 3a. Context Window Fallback（最高价值）

```go
// gateway/middleware/context_window_fallback.go

// modelContextLimits 每个模型的 token 上限
var modelContextLimits = map[string]int{
    "gpt-4o":                128_000,
    "gpt-4o-mini":           128_000,
    "gpt-4-turbo":           128_000,
    "claude-3-5-sonnet":     200_000,
    "claude-3-opus":         200_000,
    "claude-3-haiku":        200_000,
    "gemini-1.5-pro":      1_048_576,
    "gemini-1.5-flash":    1_048_576,
}

// contextWindowFallbackChain 按上限从小到大排列，用于找"更大上限"的备选
var contextWindowFallbackChain = []struct{ model string; limit int }{
    {"gpt-4o-mini",       128_000},
    {"gpt-4o",            128_000},
    {"claude-3-5-sonnet", 200_000},
    {"gemini-1.5-pro",  1_048_576},
}

// NewContextWindowFallbackMiddleware 在请求进入 proxy 前检查 token 估算。
// 若超过当前模型上限的 90%，自动切换到 context 更大的模型。
func NewContextWindowFallbackMiddleware(lookup ModelLookup) fiber.Handler {
    return func(c *fiber.Ctx) error {
        body := c.Body()
        estimated := tokenizer.EstimateTokensFromBody(body)

        var reqBody struct{ Model string `json:"model"` }
        if json.Unmarshal(body, &reqBody) != nil || reqBody.Model == "" {
            return c.Next()
        }

        limit, known := modelContextLimits[reqBody.Model]
        if !known || estimated <= limit*90/100 {
            return c.Next()
        }

        // 找到比当前 limit 更大的模型
        for _, candidate := range contextWindowFallbackChain {
            if candidate.limit > limit {
                if patched, err := patchModel(body, candidate.model); err == nil {
                    c.Request().SetBody(patched)
                    c.Set("X-Context-Fallback-From", reqBody.Model)
                    c.Set("X-Context-Fallback-To", candidate.model)
                }
                break
            }
        }
        return c.Next()
    }
}
```

#### 3b. Provider Cooldown（熔断器）

```go
// gateway/storage/cooldown_store.go
// 当某 provider 连续 3 次 5xx，Redis TTL 60s 标记为 "cooling"
// proxy handler 在 GetByName 后检查 cooldown，自动跳过冷却中的 provider

type CooldownStore struct { rdb *redis.Client }

func (s *CooldownStore) MarkFailure(ctx context.Context, provider string) {
    key := "cooldown:failure:" + provider
    count, _ := s.rdb.Incr(ctx, key).Result()
    s.rdb.Expire(ctx, key, 2*time.Minute)
    if count >= 3 {
        s.rdb.Set(ctx, "cooldown:active:"+provider, "1", 60*time.Second)
    }
}

func (s *CooldownStore) IsCooling(ctx context.Context, provider string) bool {
    v, _ := s.rdb.Get(ctx, "cooldown:active:"+provider).Result()
    return v == "1"
}
```

---

### Gap 4 — Guardrails 框架

**差距**: 只有 pre-call PII scan。缺 post-call（扫描 LLM 返回内容）和可插拔接口。

#### 4a. Post-call PII Scan

最小实现：在 `makeProxyHandler` 拿到 `result.Body` 后，调用已有的 `middleware.ScanForPII`：

```go
// 在 makeProxyHandler 的 result 拿到后：
if piiType, found := middleware.ScanForPII(string(result.Body)); found {
    // mask 响应中的 PII 而非 block（LLM 已经处理了，block 没意义）
    result.Body = middleware.MaskPII(result.Body, piiType)
    // 记录 SIEM 事件
    siemChan <- middleware.SIEMEvent{...EventType: "pii_in_response"...}
}
```

需要新增 `MaskPII(body []byte, piiType string) []byte`（将匹配内容替换为 `[REDACTED:pii_type]`）。

#### 4b. Guardrail 接口（为未来 Presidio / Lakera 对接准备）

```go
// gateway/guardrail/interface.go
type Mode string
const (
    PreCall    Mode = "pre_call"
    PostCall   Mode = "post_call"
)

type Payload struct {
    Request  []byte
    Response []byte // only populated for PostCall
    TenantID string
    UserID   string
}

type Result struct {
    Blocked bool
    Reason  string
    Masked  []byte // if Blocked==false but content was modified
}

type Guardrail interface {
    Name() string
    Mode() Mode
    Check(ctx context.Context, p *Payload) (*Result, error)
}

// Chain 按 mode 执行所有 guardrail，现有 PII middleware 迁移为第一个实现
type Chain struct {
    pre  []Guardrail
    post []Guardrail
}
```

---

### Gap 5 — Python SDK

新建 `sdk/python/` 目录，发布 `pip install totra-sdk`。

```
sdk/python/
├── pyproject.toml
├── totra/
│   ├── __init__.py
│   ├── client.py       # ToTra class
│   └── types.py        # ChatRequest, ChatResponse, etc.
```

```python
# sdk/python/totra/client.py
import httpx
from typing import Iterator

class ToTra:
    def __init__(self, api_key: str, base_url: str):
        self._headers = {"Authorization": f"Bearer {api_key}", "Content-Type": "application/json"}
        self._base = base_url.rstrip("/")
        self._http = httpx.Client(timeout=120)

    def chat(self, model: str, messages: list[dict], **kwargs) -> dict:
        r = self._http.post(f"{self._base}/v1/chat/completions",
                            headers=self._headers,
                            json={"model": model, "messages": messages, **kwargs})
        r.raise_for_status()
        return r.json()

    def stream(self, model: str, messages: list[dict], **kwargs) -> Iterator[str]:
        with self._http.stream("POST", f"{self._base}/v1/chat/completions",
                               headers=self._headers,
                               json={"model": model, "messages": messages, "stream": True, **kwargs}) as r:
            for line in r.iter_lines():
                if line.startswith("data: ") and line != "data: [DONE]":
                    yield line[6:]

    def embed(self, model: str, input: list[str], **kwargs) -> dict:
        r = self._http.post(f"{self._base}/v1/embeddings",
                            headers=self._headers,
                            json={"model": model, "input": input, **kwargs})
        r.raise_for_status()
        return r.json()
```

---

### Gap 6 — `/v1/embeddings` Endpoint

```go
// gateway/handlers/embeddings.go

// POST /v1/embeddings — 代理到 OpenAI/Azure/Bedrock embeddings
// 复用现有 auth + quota + metrics 中间件链
func NewEmbeddingsHandler(modelLookup ModelLookup, usageStore *storage.UsageStore) fiber.Handler {
    return func(c *fiber.Ctx) error {
        user := c.Locals("user").(*middleware.UserInfo)

        var req struct {
            Model string   `json:"model"`
            Input any      `json:"input"` // string or []string
        }
        if err := c.BodyParser(&req); err != nil || req.Model == "" {
            return c.Status(400).JSON(fiber.Map{"error": "model required"})
        }

        modelCfg, err := modelLookup.GetByName(c.Context(), user.TenantID, req.Model)
        if err != nil || modelCfg == nil {
            return c.Status(400).JSON(fiber.Map{"error": "model not configured"})
        }

        adapter, err := providers.New(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
        if err != nil {
            return c.Status(400).JSON(fiber.Map{"error": "unsupported provider"})
        }

        result, usage, err := adapter.ForwardEmbeddings(c.Context(), c.Body())
        if err != nil {
            return c.Status(502).JSON(fiber.Map{"error": "upstream unavailable"})
        }

        // 按 prompt token 记录费用（embeddings 只有 input token）
        usageStore.Record(&storage.UsageRecord{
            TenantID:     user.TenantID,
            PromptTokens: usage.PromptTokens,
            // ...
        })

        return c.Status(result.StatusCode).Send(result.Body)
    }
}
```

需要在 `Adapter` 接口增加 `ForwardEmbeddings` 方法，或（更简单）直接在 OpenAI adapter 里转发到 `/embeddings` endpoint，不改接口。

在 `main.go` 注册：
```go
v1.Post("/embeddings", embeddingsHandler)
```

---

### Gap 7 — MCP Gateway

MCP（Model Context Protocol）是工具调用协议。ToTra 作为 MCP Gateway，负责：接收 agent 的工具调用请求 → 转发给 MCP server → 把结果注入 LLM 上下文 → 返回最终响应。

所有现有中间件（PII / 配额 / 审计）自动覆盖，这是 LiteLLM 没有的 governance 层。

```go
// gateway/handlers/mcp.go
// POST /v1/mcp/chat
// Body:
// {
//   "model": "gpt-4o",
//   "messages": [...],
//   "mcp_servers": [{"name": "web_search", "url": "http://mcp-server:3000"}],
//   "tools": [{"type": "mcp", "server": "web_search", "tool": "search"}]
// }

func NewMCPHandler(modelLookup ModelLookup, usageStore *storage.UsageStore) fiber.Handler {
    return func(c *fiber.Ctx) error {
        // 1. 解析请求，提取 MCP server 列表
        // 2. 调用 LLM，若响应包含 tool_calls，转发给对应 MCP server
        // 3. 将 tool 结果注入 messages，再次调用 LLM（agentic loop）
        // 4. 返回最终响应
        // 所有 token 费用汇总记录
    }
}
```

---

### Gap 8 — 性能基准 & 负载测试

```javascript
// scripts/load-test/k6.js
import http from "k6/http";
import { check, sleep } from "k6";

export const options = {
    scenarios: {
        baseline: { executor: "constant-vus",  vus: 10,  duration: "2m" },
        stress:   { executor: "ramping-vus",   stages: [
            { duration: "1m", target: 100 },
            { duration: "3m", target: 100 },
            { duration: "1m", target: 0 },
        ]},
    },
    thresholds: {
        // Gateway 自身（不含 LLM 延迟）目标 P95 < 50ms
        "http_req_duration{scenario:baseline}": ["p(95)<50"],
        "http_req_failed": ["rate<0.01"],
    },
};

const GATEWAY = __ENV.GATEWAY_URL || "http://localhost:8080";
const API_KEY  = __ENV.API_KEY;

export default function () {
    const res = http.post(`${GATEWAY}/v1/chat/completions`,
        JSON.stringify({
            model: "gpt-4o-mini",
            messages: [{ role: "user", content: "ping" }],
        }),
        { headers: { "Authorization": `Bearer ${API_KEY}`, "Content-Type": "application/json" } },
    );
    check(res, { "status 200": (r) => r.status === 200 });
}
```

GitHub Actions 在 release tag 时自动跑，结果写入 release notes。

---

## 实现顺序（技术依赖驱动）

```
Week 1-2: 地基
  [1a] Azure OpenAI adapter         → 企业客户高频，1 天
  [1b] Bedrock adapter              → 最大差距，3 天
  [3a] Context window fallback      → 复用现有 tokenizer，1 天
  [3b] Provider cooldown store      → 复用现有 Redis，1 天

Week 3-4: 端点扩展
  [6]  /v1/embeddings               → 复用现有 proxy 链，2 天
  [4a] Post-call PII mask           → 复用已有 ScanForPII，1 天
  [4b] Guardrail interface          → 重构，不改外部行为，2 天

Week 5-6: 开发者生态
  [5]  Python SDK                   → 独立，3 天
  [2]  OpenTelemetry traces         → 加中间件，2 天
  [8]  k6 load tests + CI           → 新脚本，2 天

Week 7-8: Agent 生态
  [7]  MCP Gateway                  → 最复杂，1 周
```

---

## 说明

- **Mistral / Groq / Ollama / Together AI**: 已经支持！设置 `provider=openai` + 对应的 `baseURL`。
- **`/metrics`**: 已经有，不需要做。
- **自动路由 + 502 fallback + 重试**: 已经有，不需要做。
- **核心护城河（PII 18语言、GDPR、Chargeback、EU AI Act）**: LiteLLM 没有，继续深化。
