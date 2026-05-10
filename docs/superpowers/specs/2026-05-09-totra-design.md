# ToTra — 企业 AI 效能管理平台 设计文档

**日期：** 2026-05-09  
**状态：** 已批准，待实施

---

## 一、项目定位

ToTra 是一个**私有化部署的企业 AI API 管理与员工效率 KPI 衡量平台**。

企业将公司级 AI 供应商 API Key 录入 ToTra，员工统一通过 ToTra Gateway 调用各类 AI 模型（包括 LLM 和 AI Agent）。系统自动记录每个员工的 Token 消耗，结合产出数据（代码提交、任务关闭等）计算 AI 使用效率分，作为 KPI 评估依据。

**核心价值一句话：** 让企业知道每个员工用 AI 花了多少钱、产出了多少价值，并对高效员工形成激励。

### 目标客户

- **小客户**：使用企业订阅的云端大模型（OpenAI、Anthropic、Gemini 等），以 ToTra Employee Key 认证
- **大客户**：有自有本地部署模型（Ollama、vLLM 等），以企业 SSO 认证（LDAP / Active Directory / 飞书 / 钉钉）

所有客户均**私有化部署** ToTra，数据不出域。

---

## 二、整体架构

### 部署形态

```
┌─────────────────────────────────────────────────────────┐
│                     客户内网环境                           │
│                                                         │
│  员工设备（统一使用 OpenAI 兼容格式调用）                    │
│  [API Client / IDE / Cursor / App]                      │
│         │ Employee Key 或 SSO Token                     │
│         ▼                                               │
│  ┌──────────────────────────────────┐                   │
│  │         Gateway Service (Go)     │◄── Redis           │
│  │  Auth & Quota Middleware         │    (实时配额计数)    │
│  │  Provider Router                 │                   │
│  │  OpenAI│Anthropic│Gemini│Local   │                   │
│  │  Adapter Adapater Adapter Adapter│                   │
│  └──────────────┬───────────────────┘                   │
│                 │ 转发                                   │
│     OpenAI  Anthropic  Gemini  Ollama/vLLM              │
│                                                         │
│  ┌──────────────────┐   ┌──────────────┐                │
│  │  Admin Service   │   │  Dashboard   │                │
│  │  (Go)            │◄──│  (React SPA) │                │
│  └────────┬─────────┘   └──────────────┘                │
│           │                                             │
│       PostgreSQL                                        │
│                                                         │
│  一键部署：docker-compose up                              │
└─────────────────────────────────────────────────────────┘
```

### 关键设计原则

- Gateway 与 Admin Service **完全解耦**，共享 PostgreSQL + Redis，不直接通信
- Gateway 是无状态服务，可水平扩展，保持极低延迟
- 员工使用 OpenAI 兼容格式，无需关心背后是哪个供应商
- 所有 Token 换算为**标准算力单位（SCU）**，实现跨模型成本比较

---

## 三、身份认证与 Key 管理

### 两种 Key 的区分

| Key 类型 | 持有人 | 用途 |
|---|---|---|
| **供应商 API Key**（OpenAI/Anthropic 真实 Key） | 管理员 | 后台配置，员工不可见，Gateway 用于转发请求 |
| **Employee Key**（ToTra 内部颁发） | 员工 | ToTra 自动生成，员工用于调用 Gateway |

### 员工接入流程

1. 管理员录入员工信息（或通过 HR 系统自动同步）
2. ToTra 为每位员工生成唯一 Employee Key（格式：`totra-emp-{uuid}`）
3. 员工将工具中的 `api_key` 替换为 Employee Key，`base_url` 指向 Gateway 地址
4. 员工正常使用，ToTra 在后台完成追踪

### 认证方式

- **小客户（API Key 模式）**：Employee Key → Redis 查缓存确认 user_id + tenant_id
- **大客户（SSO 模式）**：JWT Token → 验签提取 user_id + tenant_id，支持 LDAP / AD / 飞书 / 钉钉

---

## 四、Gateway 请求处理流程

```
员工请求进入
      │
      ▼
1. 解析 Authorization Header → 确认 user_id + tenant_id
      │
      ▼
2. 配额检查（Redis 原子操作）
   ├── 已用 SCU < 配额上限 → 放行
   └── 超额 → 返回 429（OpenAI 兼容格式），提示剩余配额
      │
      ▼
3. 敏感信息扫描（PII 检测）
   ├── 发现手机号/身份证/客户数据 → 拦截并告警
   └── 通过 → 继续
      │
      ▼
4. Provider Router → 根据 model 字段查 model_configs 表
      │
      ▼
5. Provider Adapter 转换请求格式 → 转发到上游 API
   流式响应（SSE）直接透传给员工
      │
      ▼
6. 响应完成后【异步】写入：
   ├── Redis：原子增加已用 SCU
   └── PostgreSQL：写 usage_records 明细
```

**要点：** 步骤 6 异步执行，不影响员工拿到 AI 响应的速度。

---

## 五、数据模型

### PostgreSQL 核心表

```sql
-- 租户（每家企业一个）
tenants(id, name, plan, created_at)

-- 员工
users(id, tenant_id, name, email, employee_id,
      auth_type, api_key_hash,
      quota_scu INT, quota_reset_day INT)

-- 模型配置（管理员配置可用模型）
model_configs(id, tenant_id, name, provider,
              api_key_encrypted, base_url,
              scu_rate FLOAT, is_active BOOL)

-- 用量明细（每次请求一条，只写不改）
usage_records(id, tenant_id, user_id, model_config_id,
              prompt_tokens, completion_tokens,
              scu_cost FLOAT, usd_cost FLOAT,
              request_at TIMESTAMPTZ, response_ms INT)

-- 月度汇总（定时聚合，加速看板查询）
monthly_summaries(tenant_id, user_id, year_month,
                  total_scu, total_usd, request_count)

-- 集成配置（Webhook 工具）
integrations(id, tenant_id, type, webhook_secret,
             access_token_encrypted, is_active BOOL)

-- 产出事件（从 Webhook 收到）
output_events(id, tenant_id, user_id, source, event_type,
              event_id, title, weight FLOAT, occurred_at TIMESTAMPTZ)

-- 效率快照（周期性计算）
efficiency_snapshots(id, tenant_id, user_id,
                     period_start, period_end,
                     total_scu, total_output, efficiency_score,
                     rank INT, snapshot_at TIMESTAMPTZ)

-- AI-Fuel 余额与流水
fuel_balances(tenant_id, user_id, balance, updated_at)
fuel_transactions(id, tenant_id, user_id, amount,
                  reason, ref_snapshot_id, created_at)
```

### Redis 结构

```
quota:{tenant_id}:{user_id}:{year_month}  → 当月已消耗 SCU（原子自增）
config:{tenant_id}:{user_id}              → 用户配额上限缓存（TTL 5min）
empkey:{employee_key_hash}                → user_id + tenant_id 缓存（TTL 10min）
```

---

## 六、KPI 效率评分

### 核心公式

```
efficiency_score = total_output_weight / log(total_scu + 1)
```

- `total_output_weight`：周期内所有产出事件的加权总分
- `log(scu + 1)`：避免极低 token 用量被过度奖励（未使用 AI 的员工不应排名靠前）

### 产出事件权重（可配置）

| 事件类型 | 默认权重 |
|---|---|
| PR merged | 5 |
| Jira ticket closed (story point × 1) | 1–8 |
| Commit (有关联 PR) | 1 |
| Notion / Lark 文档创建 | 2 |

### 公平性修正

- 任务复杂度由 AI 自动评分（分析 PR 描述、Jira 详情），纳入 weight 计算
- 对比维度为**同职级同部门 Peer Group**，不做全公司横向排名
- 追踪月度效率分趋势，识别成长型员工

---

## 七、功能路线图

### Phase 1：基础层（Month 1-2）
*目标：数据准确流动，管理员有基本可见性*

**Gateway 核心**
- 多供应商 Adapter（OpenAI、Anthropic、Gemini、本地模型）
- 员工身份绑定（Employee Key + SSO）
- 实时 Token 计数 → SCU 换算
- 配额管理 + 超额自动熔断（返回 429）
- 按角色限制模型权限（初级 / 高级 / 研究员）

**安全合规**
- 敏感信息检测（PII 拦截）
- IP 白名单

**管理后台**
- 员工用量看板（模型 / SCU / 美元成本）
- 部门成本分摊报告（Chargeback，可导出）
- 配额申请审批工作流
- 预算预测（按趋势预测月底支出）
- AI 采用率追踪（未使用员工识别）

**员工自助**
- 个人用量仪表盘（配额余量、消耗趋势）
- Slack / 飞书 / 钉钉 Bot（查余额、接收告警）

**集成**
- HR 系统同步（新员工开通 / 离职封禁）

---

### Phase 2：智能层（Month 3-4）
*目标：Token 数据有意义，KPI 可量化，激励系统上线*

**产出关联引擎**
- Webhook 接入（GitHub、GitLab、Jira、飞书、Notion）
- 时间轴自动关联（Token 消耗 ↔ Commit / PR / 任务关闭）
- 任务复杂度 AI 自动评分
- 无效对话识别（abandoned session 标记）

**KPI 评分系统**
- 效率分计算与周期快照
- Peer Group 同职级对比
- 员工效率成长曲线
- 异常检测（用量突增告警）
- 使用场景自动分类（代码 / 文案 / 分析 / 翻译）

**激励系统**
- AI-Fuel 代币（内部算力积分）
- 效率高的员工自动获得额外配额奖励
- AI-Fuel 余额与流水

**优化建议**
- 模型推荐引擎（根据使用模式建议更低成本模型）
- 公司级提示词模板库
- 提示词效率评分

---

### Phase 3：信任层（Month 5-6）
*目标：不可篡改审计、高端市场、生态扩展*

**区块链审计**
- 每日快照上链（Polygon / Arbitrum 或 Hyperledger Fabric）
- IPFS 存储脱敏审计报告（链上存 Hash）
- 批量提交（降低 Gas Fee）
- 智能合约自动分配 AI-Fuel

**AI Agent 专项**
- Agent 任务效率追踪（循环次数、工具调用次数）
- Agent 死循环检测与自动熔断
- 多层 Agent 工作流追踪

**高管与商业**
- ROI 高管季度报告（AI 投入 vs 可量化生产力提升）
- 行业基准匿名对比（处于同规模公司前 X%）
- 部门效率挑战赛（赢得额外配额）

**生态**
- 白标 / API 开放给第三方 HR SaaS
- GDPR 合规工具（员工数据删除申请）
- 数据留存策略配置

---

## 八、技术栈

| 层次 | 技术选型 |
|---|---|
| Gateway | Go + Fiber |
| Admin Service | Go |
| Dashboard | React + TypeScript |
| 缓存 | Redis |
| 数据库 | PostgreSQL |
| 部署 | Docker Compose（私有化），K8s（大客户可选）|
| Token 计数 | tiktoken（OpenAI）/ Anthropic tokenizer / Gemini tokenizer |
| 区块链（Phase 3） | Polygon / Arbitrum 或 Hyperledger Fabric |
| 去中心化存储（Phase 3）| IPFS |
| 智能合约（Phase 3）| Solidity |

---

## 九、关键挑战与对策

| 挑战 | 对策 |
|---|---|
| 员工担心 Prompt 被监控 | Gateway 只记录 Token 数量和时间戳，不存储 Prompt 原文；PII 检测在本地完成后丢弃内容 |
| 不同模型 Token 定义不统一 | 统一换算为 SCU（Standard Compute Unit），各模型按价格和等级加权 |
| 区块链 Gas Fee | 批量提交策略（每 1000 次请求或每小时合并为一条链上记录） |
| KPI 公平性（任务难度不同）| AI 自动评估任务复杂度，按 Peer Group 对比，不做全公司排名 |
| 员工 KPI 篡改 | Token 数据由 Gateway 控制不可篡改；产出数据来自 GitHub/Jira 官方 Webhook，员工无法修改历史记录 |

---

## 十、商业模式

- **私有化授权**：按员工人数收取年费 + 实施费
- **SaaS 订阅**（Phase 2 后）：按月度处理 Token 量分档计费，小客户低门槛入场
- **白标授权**（Phase 3 后）：HR SaaS / ERP 厂商集成，B2B2B
- **算力转售**（可选）：作为 API 经销商，通过分层计费赚取差价
