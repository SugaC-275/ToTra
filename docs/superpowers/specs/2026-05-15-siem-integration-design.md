# SIEM Integration Design Spec

**Date:** 2026-05-15
**Feature:** 合规事件导出至 SIEM — 支持 Push（Webhook + 重试队列）+ Pull（REST API），可配置事件类型，API Key 认证

---

## Goal

企业 IT/合规团队能将 ToTra 产生的合规事件（PII 违规、策略拦截、审计日志、配额超限、路由事件）实时或定期同步到企业 SIEM 系统（Splunk、Microsoft Sentinel、任意支持 HTTP 接收的系统），满足监管审计和安全运营需求。

---

## Architecture

```
Gateway (PII block / Policy block / Quota exceeded)
    ↓ buffered channel (非阻塞)
SIEMDeliveryService.Enqueue()
    ↓
siem_delivery_queue (DB)
    ↓ Worker (每30s)
POST {endpoint_url}
Authorization: Bearer {api_key}

Enterprise SIEM (Splunk HEC / Azure Sentinel / custom)

Pull path:
Admin/SIEM → GET /api/admin/siem/events?since=&types=&limit=
              ← 联查 pii_violations + audit_log + gateway_routing_events
```

---

## Database — Migration 022

```sql
CREATE TABLE siem_configs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  endpoint_url TEXT NOT NULL,
  api_key_encrypted TEXT NOT NULL,
  event_types TEXT[] NOT NULL,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON siem_configs (tenant_id);

CREATE TABLE siem_delivery_queue (
  id BIGSERIAL PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  siem_config_id UUID NOT NULL REFERENCES siem_configs(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending','delivered','failed')),
  attempts INT NOT NULL DEFAULT 0,
  next_retry_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX ON siem_delivery_queue (tenant_id, status, next_retry_at);
```

**支持的 event_types：**
- `pii_violation` — PII 检测拦截
- `policy_block` — 策略规则拦截
- `audit_log` — 月度 KPI 快照审计条目
- `quota_exceeded` — 配额超限（429）
- `routing_event` — 自动路由降级

---

## Admin Services

### `admin/services/siem.go`

**SIEMConfigService：**

```go
type SIEMConfig struct {
    ID               string    `json:"id"`
    TenantID         string    `json:"tenant_id"`
    Name             string    `json:"name"`
    EndpointURL      string    `json:"endpoint_url"`
    EventTypes       []string  `json:"event_types"`
    IsActive         bool      `json:"is_active"`
    CreatedAt        time.Time `json:"created_at"`
}

func (s *SIEMConfigService) Create(ctx context.Context, tenantID, name, endpointURL, apiKey string, eventTypes []string) (*SIEMConfig, error)
func (s *SIEMConfigService) List(ctx context.Context, tenantID string) ([]*SIEMConfig, error)
func (s *SIEMConfigService) Delete(ctx context.Context, tenantID, id string) error
func (s *SIEMConfigService) GetActiveForTenant(ctx context.Context, tenantID string) ([]*siemConfigRow, error)
```

API key 用 AES-256-GCM 加密存储（与 `bot_configs` 一致）。`List` 返回时不解密（不暴露 key）。

**SIEMDeliveryService：**

```go
func (s *SIEMDeliveryService) Enqueue(ctx context.Context, tenantID string, eventType string, payload map[string]any) error
func (s *SIEMDeliveryService) RunWorker(ctx context.Context)
func (s *SIEMDeliveryService) SendTest(ctx context.Context, tenantID, configID string) error
func (s *SIEMDeliveryService) GetDeliveryLog(ctx context.Context, tenantID string, limit int) ([]*DeliveryLogRow, error)
```

**Enqueue 逻辑：**
1. 查询该租户所有 `is_active=TRUE` 的 siem_configs，过滤包含该 event_type 的配置
2. 每个匹配配置插入一条 `siem_delivery_queue` 记录

**Worker 逻辑（每 30s 运行）：**
```
SELECT * FROM siem_delivery_queue
WHERE status='pending' AND next_retry_at <= NOW()
ORDER BY created_at LIMIT 100
```
对每条记录：
- 解密 api_key，POST payload 到 endpoint_url（Header: `Authorization: Bearer {key}`，`Content-Type: application/json`，超时 10s）
- 成功（2xx）→ `status='delivered'`, `delivered_at=NOW()`
- 失败 → `attempts++`，`next_retry_at = NOW() + interval '2^attempts minutes'`（1/2/4 分钟）
- `attempts >= 3` → `status='failed'`

**Push payload 格式（统一）：**
```json
{
  "source": "totra",
  "tenant_id": "acme-corp",
  "event_type": "pii_violation",
  "occurred_at": "2026-05-15T10:30:00Z",
  "detail": {
    "user_id": "...",
    "pii_type": "china_phone",
    "action": "blocked",
    "path": "/v1/chat/completions"
  }
}
```

### `admin/services/siem_pull.go`

```go
type SIEMEvent struct {
    ID         string          `json:"id"`
    Type       string          `json:"type"`
    TenantID   string          `json:"tenant_id"`
    OccurredAt time.Time       `json:"occurred_at"`
    Detail     json.RawMessage `json:"detail"`
}

type SIEMEventsResult struct {
    Events    []*SIEMEvent `json:"events"`
    NextSince time.Time    `json:"next_since"`
}

func (s *SIEMPullService) GetEvents(ctx context.Context, tenantID string, since time.Time, types []string, limit int) (*SIEMEventsResult, error)
```

联查三张表（`pii_violations`、`audit_log`、`gateway_routing_events`），按 `occurred_at DESC` 排序，返回统一格式。`next_since` = 最新事件时间戳，供下次拉取用。

---

## API Routes — `admin/api/siem.go`

| Method | Path | 说明 |
|--------|------|------|
| GET | `/api/admin/siem/configs` | 列出 SIEM 配置（不含 api_key）|
| POST | `/api/admin/siem/configs` | 新建配置 |
| DELETE | `/api/admin/siem/configs/:id` | 删除配置 |
| POST | `/api/admin/siem/configs/:id/test` | 发送测试事件 |
| GET | `/api/admin/siem/events` | Pull API（?since=&types=&limit=）|
| GET | `/api/admin/siem/delivery-log` | 推送日志（最近 N 条）|

所有端点 admin only（`c.Locals("role") == "admin"`）。

---

## Gateway 修改

### `gateway/main.go`

启动时初始化并传入中间件：

```go
siemChan := make(chan siemEvent, 1000)
siemWorker := startSIEMEnqueuer(siemChan, siemConfigStore, deliveryQueue)
go siemWorker.Run(ctx)

// 传给 PII 和 Policy 中间件
middleware.NewPIIMiddleware(piiStore, "", siemChan)
middleware.NewPolicyMiddleware(policyRuleStore, siemChan)
```

中间件拦截时：
```go
select {
case siemChan <- siemEvent{TenantID: tid, EventType: "pii_violation", Payload: detail}:
default: // channel 满则丢弃，不阻塞请求
}
```

`startSIEMEnqueuer` 是一个 goroutine，消费 channel 并调用 `SIEMDeliveryService.Enqueue()`。

---

## Dashboard — `SIEMPage.tsx`

**管理员专区，侧边栏新增入口。**

布局：
- **配置列表卡片**：每条显示名称、URL（截断 50 字符）、事件类型标签组、状态徽章（Active/Inactive）、删除按钮、测试按钮
- **新建表单**（展开式）：名称、Endpoint URL、API Key（password input）、事件类型多选（勾选框）
- **推送日志表**：最近 50 条，列：事件类型 | 状态 | 重试次数 | 时间

---

## Error Handling

| 情况 | 行为 |
|------|------|
| Enqueue 时无匹配 siem_config | 静默跳过，不报错 |
| Push 超时（10s）| 同失败处理，attempts++ |
| Push 3 次失败 | status='failed'，不再重试，日志可查 |
| Pull API since 参数缺失 | 默认返回最近 24h |
| API key 解密失败 | 跳过该配置，记录 error log |

---

## Out of Scope

- CEF（Common Event Format）格式支持（JSON 已覆盖 Splunk/Sentinel）
- 事件去重（幂等 key）
- SIEM 配置数量限制（暂无）
- Pull API 分页（用 since cursor 替代）
- Dashboard Pull API 测试界面
