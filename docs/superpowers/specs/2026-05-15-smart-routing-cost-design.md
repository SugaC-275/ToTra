# Smart Routing + Cost Precision Design Spec

**Date:** 2026-05-15
**Feature:** 成本优化 Phase 2 — 多信号复杂度路由升级 + 美元节省精算

---

## Goal

将现有基于 body 字节数的粗糙路由升级为多维度复杂度打分，同时在 `model_configs` 引入模型单价，实现逐次请求的美元节省精算，为 CFO 提供可信的 AI 成本优化数据。不做 Prompt 压缩（侵入性风险高，边际收益低）。

---

## Architecture

```
Gateway: POST /v1/chat/completions
    ↓
NewAutoRouterMiddleware
    ↓
complexityScore(body) → 0-100
    ↓
score < ROUTER_COMPLEXITY_THRESHOLD(默认50)
    ↓ YES                          ↓ NO
降级 model                      保留原 model
记录 complexity_score           继续
    ↓
响应返回后回填 prompt_tokens + completion_tokens + usd_saved
→ gateway_routing_events

Admin: GET /api/admin/cost/savings?month=
    ← SUM(usd_saved), AVG(complexity_score), routing_event_count
```

---

## Database Changes

### Migration 023

```sql
-- 模型单价（可选，NULL 表示不计算美元节省）
ALTER TABLE model_configs
  ADD COLUMN price_per_m_input  NUMERIC(10,4),
  ADD COLUMN price_per_m_output NUMERIC(10,4);

-- 路由事件补充字段
ALTER TABLE gateway_routing_events
  ADD COLUMN complexity_score   INT,
  ADD COLUMN prompt_tokens      INT,
  ADD COLUMN completion_tokens  INT,
  ADD COLUMN usd_saved          NUMERIC(12,6);
```

`price_per_m_input` / `price_per_m_output` 为 NULL 时，`usd_saved` 记为 NULL（不影响路由功能）。

---

## Gateway Changes

### `gateway/middleware/router.go`

**新增 `complexityScore(body []byte) int`：**

```
复杂度分（0–100）=
  40 × min(len(body) / 4000, 1.0)         // 请求体长度
+ 20 × min(messageCount / 10, 1.0)        // 对话轮数（messages 数组长度）
+ 20 × (hasSystemPrompt ? 1 : 0)          // 存在 system role 消息
+ 10 × (hasToolCalls ? 1 : 0)             // 存在 tool_calls 字段
+ 10 × (hasComplexKeyword ? 1 : 0)        // 含关键词：分析/推理/审查/对比/架构/重构/evaluate/analyze/compare/refactor
```

**路由判断替换：**

```go
// 旧
if len(body) >= complexityThreshold { return c.Next() }

// 新
score := complexityScore(body)
threshold := getEnvInt("ROUTER_COMPLEXITY_THRESHOLD", 50)
if score >= threshold { return c.Next() }
```

**`RoutingEvent` struct 新增字段：**

```go
type RoutingEvent struct {
    TenantID, UserID, OriginalModel, RoutedModel string
    BodyLen         int
    ComplexityScore int   // 新增
}
```

**响应后回填 token 数和节省金额：**

路由降级发生后，`makeProxyHandler` 在收到上游响应时，异步更新该次路由事件：

```go
go routingStore.UpdateTokensAndSavings(ctx, routingEventID,
    usage.PromptTokens, usage.CompletionTokens,
    originalModelCfg, routedModelCfg)
```

`UpdateTokensAndSavings` 计算：

```
usd_saved = NULL  // 若任一模型价格为 NULL
usd_saved = prompt_tokens/1_000_000 × (orig.price_per_m_input - routed.price_per_m_input)
          + completion_tokens/1_000_000 × (orig.price_per_m_output - routed.price_per_m_output)
```

### `gateway/storage/routing_store.go`

新增方法：

```go
func (s *RoutingStore) Record(ctx context.Context, e middleware.RoutingEvent) (int64, error)
// 返回新插入行的 ID，供后续回填用

func (s *RoutingStore) UpdateTokensAndSavings(ctx context.Context,
    id int64, promptTokens, completionTokens int,
    origPrice, routedPrice *ModelPrice) error
// ModelPrice = {PricePerMInput, PricePerMOutput float64}
```

---

## Admin Services

### `admin/services/cost_savings_report.go`（升级）

```go
type MonthlySavingsReport struct {
    YearMonth         string            `json:"year_month"`
    RoutingEventCount int64             `json:"routing_event_count"`
    TotalUSDSaved     *float64          `json:"total_usd_saved"`      // nil 表示无价格数据
    AvgComplexityScore *float64         `json:"avg_complexity_score"` // nil 表示无数据
    RoutedModels      []RoutedModelStat `json:"routed_models"`
    GeneratedAt       string            `json:"generated_at"`
}
```

SQL 升级：

```sql
SELECT
  COUNT(*) AS routing_event_count,
  SUM(usd_saved) AS total_usd_saved,
  AVG(complexity_score) AS avg_complexity_score,
  ...
FROM gateway_routing_events
WHERE tenant_id = $1
  AND to_char(routed_at AT TIME ZONE 'UTC','YYYY-MM') = $2
```

---

## Admin API

### 新增端点（`admin/api/cost_reports.go`）

```
PUT /api/admin/models/:id/pricing
Body: { "price_per_m_input": 5.00, "price_per_m_output": 15.00 }
Response: 200 { "id": "...", "name": "gpt-4o", "price_per_m_input": 5.00, ... }
```

```
GET /api/admin/cost/savings?month=2026-05
Response: MonthlySavingsReport（含 total_usd_saved, avg_complexity_score）
```

---

## Dashboard Changes

### `CostCenterPage.tsx`（扩展）

**统计卡新增（顶部 4 列扩为 6 列，或替换现有低价值卡）：**

- 「本月节省」：`$342.18`（`total_usd_saved` 非 null 时显示，否则显示 `—`，tooltip 说明"请在模型配置中填写价格"）
- 「平均复杂度」：`38 / 100`（`avg_complexity_score` 非 null 时显示）

**路由事件明细表新增列：**
- `Complexity` — 显示分数（如 `32`）+ 颜色条（绿=低复杂，红=高复杂）
- `Saved` — 显示美元节省（如 `$0.005`，null 时显示 `—`）

### Models 配置页（`ModelsPage.tsx`，已有）

每个模型编辑表单新增两个可选输入：
- **Input Price** `$_____ / M tokens`
- **Output Price** `$_____ / M tokens`

提交后调 `PUT /api/admin/models/:id/pricing`。

---

## Configuration

| 环境变量 | 默认值 | 说明 |
|---------|--------|------|
| `ROUTER_COMPLEXITY_THRESHOLD` | `50` | 0–100，低于此分值触发路由降级 |

---

## Error Handling

| 情况 | 行为 |
|------|------|
| `complexityScore` JSON 解析失败 | 返回默认分 0（触发路由）|
| `UpdateTokensAndSavings` 失败 | 记录 error log，不影响响应 |
| 模型无价格数据 | `usd_saved = NULL`，报告显示 `—` |
| 负节省值（路由到更贵的模型）| 记录实际值，报告如实显示 |

---

## Out of Scope

- Prompt 压缩（侵入性风险高）
- 语义模型分类（小模型推理，额外延迟）
- 动态阈值（按部门/用户设置不同阈值）
- 历史路由事件补录 USD 节省（只计算新事件）
