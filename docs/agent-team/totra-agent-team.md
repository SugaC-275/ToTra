# ToTra Agent Team 配置文档

**日期：** 2026-05-09  
**框架：** Claude Code (Superpowers) + Claude Flow v3  
**项目：** ToTra — 企业 AI 效能管理平台

---

## 一、编排模型

```
Claude Flow v3（任务编排 + 内存管理 + 并行调度）
         +
Superpowers Skills（每个 Agent 的专业能力包）
```

- **Claude Flow v3** 负责：Agent 间通信、任务队列、并行/串行调度、共享内存 (Memory Bank)
- **Superpowers Skills** 负责：每个 Agent 的工作方法论（TDD、安全审查、UI 设计等）
- **PUA Skills** 强制配置给所有 Agent：保持高执行力、不妥协、持续推进

---

## 二、Skills 全量一览表

| Agent | Superpowers Skills | Claude Flow Skills | 通用 Skills |
|---|---|---|---|
| 📋 Product Manager（总指挥）| `dispatching-parallel-agents` `writing-plans` `executing-plans` `verification-before-completion` `brainstorming` | `hive-mind-advanced` `swarm-orchestration` `v3-swarm-coordination` `v3-core-implementation` | `to-issues` `to-prd` `github-project-management` `github-multi-repo` `triage` `pua` `pua-loop` |
| 🏗️ Architect | `brainstorming` `writing-plans` `systematic-debugging` | `v3-core-implementation` `v3-ddd-architecture` `improve-codebase-architecture` `sparc-methodology` | `pua` |
| 💼 BD Manager | `brainstorming` | — | `web-access` `grill-with-docs` `internal-comms` `pua` |
| ✍️ Doc Writer | `writing-skills` | — | `document-skills-docx` `document-skills-pdf` `document-skills-pptx` `doc-coauthoring` `writing-rules` `grill-with-docs` `pua` |
| ⚡ Gateway Engineer | `test-driven-development` `subagent-driven-development` `finishing-a-development-branch` `verification-before-completion` | `stream-chain` `v3-performance-optimization` `worker-integration` `performance-analysis` | `tdd` `security-review` `pua` |
| 🗄️ Admin Engineer | `test-driven-development` `subagent-driven-development` `finishing-a-development-branch` `verification-before-completion` | `v3-core-implementation` `v3-memory-unification` `worker-benchmarks` | `tdd` `pua` |
| 🎨 Frontend Engineer | `test-driven-development` `finishing-a-development-branch` `verification-before-completion` | `v3-integration-deep` | `ui-ux-pro-max` `frontend-design` `design-system` `webapp-testing` `pua` |
| 🔒 Security Engineer | `receiving-code-review` `requesting-code-review` `systematic-debugging` | `v3-security-overhaul` `v3-mcp-optimization` | `security-review` `pua` |
| 🛠️ DevOps Engineer | `using-git-worktrees` `finishing-a-development-branch` `verification-before-completion` | `v3-mcp-optimization` `v3-cli-modernization` | `setup-pre-commit` `github-workflow-automation` `hooks-automation` `github-project-management` `pua` |

> 所有 `superpowers:*` skills 通过 Skill tool 调用，前缀 `superpowers:` 已省略以便阅读。

---

## 三、Agent 详细配置

### 📋 Agent 0：Product Manager（产品经理 / 总指挥）

**职责：** 统筹全局、任务拆解与分发、调度并行/串行工作流、追踪进度、协调冲突、质量把关  
**工具：** Claude Flow v3 Queen 模式

| 类别 | Skills |
|---|---|
| Superpowers | `dispatching-parallel-agents` / `writing-plans` / `executing-plans` / `verification-before-completion` / `brainstorming` |
| Claude Flow | `hive-mind-advanced` / `swarm-orchestration` / `v3-swarm-coordination` / `v3-core-implementation` |
| 通用 | `to-issues` / `to-prd` / `github-project-management` / `github-multi-repo` / `triage` / `pua` / `pua-loop` |

**核心工作流：**
```
读取设计文档 + Architect 输出的 API 契约
      │
      ▼
用 to-prd 生成各模块 PRD
      │
      ▼
用 to-issues 拆解为 GitHub Issues（含优先级、Assignee、依赖关系）
      │
      ▼
用 dispatching-parallel-agents 将 Issues 分发给对应 Engineer Agent
      │
      ▼
用 github-project-management 持续追踪，阻塞时用 pua-loop 推进
```

---

### 🏗️ Agent 1：Architect（系统架构师）

**职责：** 整体架构设计、模块接口定义、技术决策、API 契约  
**工作时机：** **串行**，所有编码工作开始前必须完成

| 类别 | Skills |
|---|---|
| Superpowers | `brainstorming` / `writing-plans` / `systematic-debugging` |
| Claude Flow | `v3-core-implementation` / `v3-ddd-architecture` / `improve-codebase-architecture` / `sparc-methodology` |
| 通用 | `pua` |

---


### 💼 Agent 3：BD Manager（商务拓展经理）

**职责：** 市场调研、竞品分析、目标客户画像、定价策略、销售材料准备  
**工作时机：** 全程**并行**，与技术团队独立运作，定期向 Orchestrator 汇报

| 类别 | Skills |
|---|---|
| Superpowers | `brainstorming` |
| Claude Flow | — |
| 通用 | `web-access` / `grill-with-docs` / `internal-comms` / `pua` |

**核心工作内容：**
- 用 `web-access` 调研 LiteLLM、PortKey、Langfuse 等竞品的定价和功能
- 用 `grill-with-docs` 分析客户合同模板和行业报告
- 用 `brainstorming` 设计分层定价方案（按员工数 / 按 Token 量 / 私有化授权）
- 用 `internal-comms` 起草客户 Pitch Deck 和产品介绍文档

---

### ✍️ Agent 4：Doc Writer（文档工程师）

**职责：** 技术文档、API 文档、用户手册、部署指南、产品更新日志  
**工作时机：** 各 Engineer Agent 完成功能后**串行**触发，持续输出文档

| 类别 | Skills |
|---|---|
| Superpowers | `writing-skills` |
| Claude Flow | — |
| 通用 | `document-skills-docx` / `document-skills-pdf` / `document-skills-pptx` / `doc-coauthoring` / `writing-rules` / `grill-with-docs` / `pua` |

**输出文档类型：**

| 文档 | 格式 | 触发时机 |
|---|---|---|
| API 参考文档 | Markdown / PDF | Gateway + Admin 完工后 |
| 私有化部署指南 | PDF / Docx | DevOps 完工后 |
| 用户操作手册 | PDF / Docx | Frontend 完工后 |
| 管理员配置手册 | PDF / Docx | Phase 1 完工后 |
| 产品功能介绍（PPT） | PPTX | BD Manager 需要时 |
| CHANGELOG | Markdown | 每个 Sprint 结束后 |

---

### ⚡ Agent 5：Gateway Engineer（网关工程师）

**职责：** Go Gateway Service：多供应商 Adapter、Token 计数、配额中间件、流式代理  
**工作时机：** PM 分发 Issues 后与 Admin Engineer、Frontend Engineer **并行**

| 类别 | Skills |
|---|---|
| Superpowers | `test-driven-development` / `subagent-driven-development` / `finishing-a-development-branch` / `verification-before-completion` |
| Claude Flow | `stream-chain` / `v3-performance-optimization` / `worker-integration` / `performance-analysis` |
| 通用 | `tdd` / `security-review` / `pua` |

---

### 🗄️ Agent 6：Admin Service Engineer（后台服务工程师）

**职责：** Go Admin Service：用户管理、配额配置、KPI 评分引擎、报表 API  
**工作时机：** PM 分发 Issues 后与 Gateway Engineer、Frontend Engineer **并行**

| 类别 | Skills |
|---|---|
| Superpowers | `test-driven-development` / `subagent-driven-development` / `finishing-a-development-branch` / `verification-before-completion` |
| Claude Flow | `v3-core-implementation` / `v3-memory-unification` / `worker-benchmarks` |
| 通用 | `tdd` / `pua` |

---

### 🎨 Agent 7：Frontend Engineer（前端工程师）

**职责：** React Dashboard：管理员看板、员工自助页、KPI 可视化  
**工作时机：** PM 分发 Issues 后与 Gateway、Admin Engineer **并行**

| 类别 | Skills |
|---|---|
| Superpowers | `test-driven-development` / `finishing-a-development-branch` / `verification-before-completion` |
| Claude Flow | `v3-integration-deep` |
| 通用 | `ui-ux-pro-max` / `frontend-design` / `design-system` / `webapp-testing` / `pua` |

---

### 🔒 Agent 8：Security Engineer（安全工程师）

**职责：** PII 检测、API Key 加密、SSO 集成、跨切面安全审查  
**工作时机：** 每个 Engineer 完工前**并行触发**安全审查

| 类别 | Skills |
|---|---|
| Superpowers | `receiving-code-review` / `requesting-code-review` / `systematic-debugging` |
| Claude Flow | `v3-security-overhaul` / `v3-mcp-optimization` |
| 通用 | `security-review` / `pua` |

---

### 🛠️ Agent 9：DevOps Engineer（基础设施工程师）

**职责：** Docker Compose 编排、CI/CD 流水线、监控告警、Pre-commit hooks  
**工作时机：** 架构确认后**立即并行启动**，全程运行

| 类别 | Skills |
|---|---|
| Superpowers | `using-git-worktrees` / `finishing-a-development-branch` / `verification-before-completion` |
| Claude Flow | `v3-mcp-optimization` / `v3-cli-modernization` |
| 通用 | `setup-pre-commit` / `github-workflow-automation` / `hooks-automation` / `github-project-management` / `pua` |

---

## 四、工作流调度

### 串行阶段

```
[Product Manager（总指挥）] 初始化 + 全局规划
      │
      ▼
[Architect] 系统设计 + API 契约
      │
      ▼
[Product Manager] 拆解 Issues，分发任务
      │
      ▼
各 Engineer Agent 解锁
```

### 并行阶段

```
[Gateway Engineer] ──┐
[Admin Engineer]  ───┤── 并行开发（PM 持续监控）
[Frontend Engineer]──┤
[DevOps Engineer] ───┘
      │
[Security Engineer] — 每个 Agent 完工时并行触发
[BD Manager]        — 全程独立并行
[Doc Writer]        — 每个模块完工后并行触发
```

### 集成阶段（串行）

```
[Product Manager] 触发集成 Sprint
      │
      ▼
[Gateway + Admin] 集成验证
      │
      ▼
[Frontend + Backend] E2E 测试
      │
      ▼
[Security] 最终安全审查
      │
      ▼
[DevOps] 部署
      │
      ▼
[Doc Writer] 输出部署指南 + 用户手册
```

---

## 五、Claude Flow v3 Memory Bank

| Key | 内容 | 写入方 | 读取方 |
|---|---|---|---|
| `arch/api-contracts` | 服务间 API 接口定义 | Architect | Gateway, Admin, Frontend |
| `arch/db-schema` | 数据库表结构 | Architect | Admin, Gateway |
| `arch/scu-rates` | SCU 换算规则 | Architect | Gateway |
| `pm/issues` | 当前 Sprint Issue 清单 | PM | 所有 Engineer |
| `pm/blockers` | 阻塞项列表 | PM | Orchestrator |
| `status/gateway` | Gateway 完工状态 | Gateway | PM, Frontend |
| `status/admin` | Admin 完工状态 | Admin | PM, Frontend |
| `status/frontend` | Frontend 完工状态 | Frontend | PM |
| `security/findings` | 安全审查发现项 | Security | 所有 Agent |
| `deploy/env-vars` | 环境变量清单 | DevOps | 所有 Agent |
| `bd/pricing` | 定价方案草稿 | BD Manager | Orchestrator, PM |
| `docs/changelog` | 功能更新记录 | Doc Writer | BD Manager |

---

## 六、Phase 分工总览

| Agent | Phase 1 | Phase 2 | Phase 3 |
|---|---|---|---|
| 📋 PM（总指挥） | ✅ 全程 | ✅ 全程 | ✅ 全程 |
| 🏗️ Architect | ✅ 架构设计 | ✅ KPI 引擎设计 | ✅ 链上架构设计 |
| 📋 PM | ✅ Issue 分发 + 追踪 | ✅ Issue 分发 + 追踪 | ✅ Issue 分发 + 追踪 |
| 💼 BD Manager | ✅ 竞品调研 + 定价 | ✅ 客户案例收集 | ✅ 合规客户开拓 |
| ✍️ Doc Writer | ✅ 部署指南 + API 文档 | ✅ 用户手册更新 | ✅ 审计报告模板 |
| ⚡ Gateway Engineer | ✅ 核心开发 | ✅ Agent 流量追踪 | 🔸 轻度扩展 |
| 🗄️ Admin Engineer | ✅ 核心开发 | ✅ KPI 评分引擎 | ✅ AI-Fuel 合约集成 |
| 🎨 Frontend Engineer | ✅ 基础看板 | ✅ KPI 可视化 | ✅ 区块链审计界面 |
| 🔒 Security Engineer | ✅ 跨切面 | ✅ Webhook 安全 | ✅ 链上安全审计 |
| 🛠️ DevOps Engineer | ✅ 环境搭建 | ✅ CI/CD 优化 | ✅ 链节点部署 |

---

## 七、PUA Skills 强制配置

所有 Agent 必须配置 `pua`，Product Manager 额外配置 `pua-loop`。

**原则：** 遇到阻塞时主动上报 Orchestrator，不等待，不停滞。
