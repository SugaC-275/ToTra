# ToTra Phase 2 — 智能层设计文档

**日期：** 2026-05-10  
**状态：** 已批准，待实施

---

## 一、目标

在 Phase 1（数据采集层）基础上，增加**产出数据接入**和**KPI效率评分**，让 SCU 消耗数据真正变得有意义，并通过 AI-Fuel 自动配额奖励形成激励闭环。

**Phase 2 包含三个子系统：**
1. 产出关联引擎（Webhook 接入）
2. KPI 效率评分系统
3. AI-Fuel 自动配额奖励

模型推荐引擎推迟到 Phase 3（仅对多模型场景有价值，覆盖率低）。

---

## 二、架构

**不新增独立服务**，全部扩展到现有服务中：

```
admin service (Go :8081) 新增：
  POST /webhooks/github          接收 GitHub 事件
  POST /webhooks/jira            接收 Jira 事件
  POST /webhooks/feishu          接收 飞书/钉钉 事件
  GET  /api/integrations         查询集成配置
  POST /api/integrations         管理员创建集成（填写 Webhook 密钥）
  GET  /api/kpi/snapshots        查询月度 KPI 快照
  GET  /api/me/integrations      员工查看自己的第三方账号绑定
  POST /api/me/integrations      员工绑定第三方账号
  POST /api/admin/kpi/run        手动触发月度快照（测试用）

dashboard (React :3000) 新增：
  /admin/kpi                     KPI 排行榜页
  /admin/integrations            Webhook 集成配置页
  /me（扩展）                    新增效率分 + 账号绑定

数据库 Phase 2 新建全部6张表（Phase 1 未建）：
  user_integrations              用户第三方账号映射
  output_events                  产出事件记录
  efficiency_snapshots           月度效率快照
  fuel_transactions              AI-Fuel 奖励流水
  webhook_configs                Webhook 密钥与平台配置
  fuel_settings                  租户级奖励比例配置
```

---

## 三、子系统1：产出关联引擎

### 3.1 支持的平台与事件

| 平台 | 事件 | 默认权重 | 说明 |
|---|---|---|---|
| GitHub | pull_request.closed (merged=true) | 5 | PR 合并 |
| GitHub | push (含 commit) | 1 | 有关联 PR 的 commit |
| Jira | issue_updated (status→Done) | story_points × 1 | 默认 story_points=3 |
| 飞书 | task.completed | 2 | 飞书任务完成 |
| 飞书 | docs.created | 2 | 飞书文档创建 |
| 钉钉 | task.completed | 2 | 钉钉任务完成 |

权重存储在 `webhook_configs.event_weights` (JSONB)，管理员可按租户自定义。

### 3.2 签名验证

每个平台用 HMAC-SHA256 验证请求合法性，拒绝无效签名（返回 401）：

| 平台 | Header | 算法 |
|---|---|---|
| GitHub | `X-Hub-Signature-256` | HMAC-SHA256(secret, body) |
| Jira | `X-Hub-Signature` | HMAC-SHA256(secret, body) |
| 飞书 | `X-Lark-Signature` | HMAC-SHA256(secret, timestamp+body) |

密钥由管理员在 ToTra 后台配置，不对外暴露。

### 3.3 用户身份匹配（降级链）

```
步骤1：邮箱自动匹配
  GitHub event.pusher.email == users.email → 匹配成功

步骤2：员工自助绑定
  user_integrations WHERE platform=github AND external_id=event.sender.login → 匹配成功

步骤3：管理员手动映射
  user_integrations WHERE platform=github AND external_id=event.sender.login
  AND created_by=admin → 匹配成功

步骤4：全部失败
  记录 output_events WHERE user_id=NULL, raw_payload=...
  供管理员在后台排查（Integrations 页显示 unmatched 事件列表）
```

### 3.4 幂等性

`output_events` 表有唯一索引 `(platform, external_event_id)`。  
重复投递的 Webhook 在 INSERT 时触发 `ON CONFLICT DO NOTHING`，安全忽略。

### 3.5 新增数据库表

```sql
-- Webhook 集成配置
CREATE TABLE webhook_configs (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id),
  platform     TEXT NOT NULL,  -- github | jira | feishu | dingtalk
  webhook_secret_encrypted TEXT NOT NULL,  -- AES-256 加密，与 model_configs.api_key_encrypted 一致
  event_weights JSONB NOT NULL DEFAULT '{}',
  is_active    BOOLEAN NOT NULL DEFAULT TRUE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 用户第三方账号映射（Phase 2 新建）
CREATE TABLE user_integrations (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id    UUID NOT NULL REFERENCES tenants(id),
  user_id      UUID REFERENCES users(id),  -- NULL = 管理员映射待关联
  platform     TEXT NOT NULL,  -- github | jira | feishu | dingtalk
  external_id  TEXT NOT NULL,  -- GitHub login / Jira accountId / 飞书 open_id
  created_by   TEXT NOT NULL DEFAULT 'employee',  -- employee | admin
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, platform, external_id)
);

-- 产出事件（Phase 2 新建）
CREATE TABLE output_events (
  id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          UUID NOT NULL REFERENCES tenants(id),
  user_id            UUID REFERENCES users(id),  -- NULL = 未匹配
  platform           TEXT NOT NULL,
  event_type         TEXT NOT NULL,
  external_event_id  TEXT NOT NULL,
  title              TEXT,
  weight             FLOAT NOT NULL DEFAULT 0,
  occurred_at        TIMESTAMPTZ NOT NULL,
  raw_payload        JSONB,
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (platform, external_event_id)
);
```

---

## 四、子系统2：KPI 效率评分系统

### 4.1 效率分公式

```
efficiency_score = total_output_weight / log(total_scu + 1)
```

- `total_output_weight`：当月所有 output_events.weight 之和
- `log(total_scu + 1)`：自然对数，+1 防分母为零
- 无产出事件时 efficiency_score = 0（不参与排名）

### 4.2 Peer Group

按 `users.role` 字段分组：standard / senior / researcher / admin。  
排名仅在同 role 内进行，不做全公司横向比较。

### 4.3 月度快照（Cron Job）

**触发时机：** 每月 1 日 00:05（admin service 内置 goroutine，启动时注册）

**计算步骤：**
```
1. 查询上月所有活跃用户（有 usage_records 或 output_events）
2. 对每个用户：
   a. SUM(usage_records.scu_cost) → total_scu
   b. SUM(output_events.weight)   → total_output_weight
   c. efficiency_score = total_output_weight / log(total_scu + 1)
3. 按 role 分组，在组内排名（RANK() OVER PARTITION BY role）
4. 写入 efficiency_snapshots
5. 触发 AI-Fuel 奖励计算（见子系统3）
```

**新员工规则（入职 ≤ 90 天）：**

入职日期以 `users.created_at` 为准，90 天窗口期内属于"新员工期"。

- **新员工期（入职 ≤ 90 天）：**
  - 按**同批次**（同月入职）组成独立 Cohort Peer Group，在组内横向排名
  - 同时展示自我成长曲线（效率分月度趋势 ↑↓）
  - 不触发 AI-Fuel 奖励（Cohort 排名不产生配额奖励）
  - `efficiency_snapshots.peer_group` 字段记录为 `cohort_{YYYY-MM}`（入职月份）

- **第 91 天起（正式员工期）：**
  - 自动并入 `users.role` 对应的正式 Peer Group
  - 同时继续展示自我成长曲线（含新员工期历史数据，完整趋势）
  - 触发正常 AI-Fuel 奖励规则

### 4.4 Dashboard：KPI 排行榜（`/admin/kpi`）

管理员视图，按月份筛选，**分两个 Tab**：

**Tab 1：正式员工**（入职 > 90 天）  
按 role 分区（standard / senior / researcher），每个分区内显示：

| 列 | 说明 |
|---|---|
| 员工姓名 | — |
| 角色 | Peer Group 标识 |
| 效率分 | 本月计算值，2位小数 |
| 产出权重 | 各平台事件汇总 |
| SCU 消耗 | — |
| Peer Group 排名 | 第 X / 共 Y 人 |
| 环比趋势 | ↑↓ 与上月对比 |

**Tab 2：新员工**（入职 ≤ 90 天，按入职月份分 Cohort）  
每个 Cohort 一个分区，展示同批次横向对比 + 成长曲线趋势。

**员工 My Usage 页：**
- 新员工期：展示 Cohort 排名 + 成长曲线，标注"新员工期（还有 X 天进入正式评估）"
- 正式员工期：展示 Peer Group 排名 + 完整历史成长曲线（含新员工期数据）

---

## 五、子系统3：AI-Fuel 自动配额奖励

### 5.1 奖励规则

月度快照计算完成后立即执行，按 Peer Group 内排名百分位：

| 排名百分位 | 默认额外配额 |
|---|---|
| 前 10% | +20% |
| 前 25% | +10% |
| 前 50% | +5%  |
| 后 50% | 无奖励 |

奖励比例存储在 `fuel_settings` 表，**管理员可按租户自定义**。

### 5.2 奖励计算

```
awarded_scu = FLOOR(user.quota_scu * bonus_rate)
```

奖励 SCU 直接加到用户当月可用配额（UPDATE users SET quota_scu = quota_scu + awarded_scu）。  
同时写一条 `fuel_transactions` 流水记录。

**新员工（入职 ≤ 90 天）：** 不参与奖励计算，跳过。

### 5.3 fuel_settings 表

```sql
CREATE TABLE fuel_settings (
  tenant_id         UUID PRIMARY KEY REFERENCES tenants(id),
  top_10pct_bonus   FLOAT NOT NULL DEFAULT 0.20,
  top_25pct_bonus   FLOAT NOT NULL DEFAULT 0.10,
  top_50pct_bonus   FLOAT NOT NULL DEFAULT 0.05,
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

### 5.4 员工感知

My Usage 页新增奖励说明：
> "本月效率奖励：+1,000 SCU（Peer Group 排名：前 25%）"

---

## 六、安全与边界情况

| 场景 | 处理方式 |
|---|---|
| Webhook 签名验证失败 | 返回 401，记录告警日志 |
| 同一事件重复投递 | ON CONFLICT DO NOTHING，幂等处理 |
| 用户未绑定第三方账号 | 记录 unmatched event，管理员可排查 |
| 租户禁用 AI-Fuel | fuel_settings 所有奖励比例设为 0，快照照常生成 |
| 月度快照执行失败 | 重试3次，失败后写错误日志，支持管理员手动触发 `/api/admin/kpi/run` |
| 新员工无产出数据 | efficiency_score=0，不参与排名，不受奖惩 |

---

## 七、实施顺序

按依赖关系顺序实施（不并行）：

1. **E — 数据库迁移** — 新建全部6张 Phase 2 表（002_phase2.sql）
2. **F — Webhook 接收层** — 三平台签名验证 + 事件解析 + 用户匹配 + output_events 写入
3. **G — 集成配置 API + 用户绑定** — 管理员配置 Webhook 密钥，员工绑定第三方账号
4. **H — KPI 评分引擎** — 月度快照 cron + efficiency_score 计算 + Peer Group 排名
5. **I — AI-Fuel 奖励** — 奖励计算 + quota 更新 + fuel_transactions 流水
6. **J — Dashboard 扩展** — KPI 排行榜页、Integrations 配置页、My Usage 扩展
