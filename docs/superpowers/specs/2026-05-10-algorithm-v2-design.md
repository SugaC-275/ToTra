# Algorithm v2.0 — AI Efficiency Score Design

> **For agentic workers:** Implementation plan is at `docs/superpowers/plans/2026-05-10-algorithm-v2.md`

**Goal:** Replace the current `output_weight / ln(scu+1)` formula with a multi-pillar algorithm that covers all employees (including those without ecosystem integrations), is anti-gaming, and is legally defensible for performance reviews.

**Architecture:** Three-pillar composite score (AIQ + OSS + GTS) with adaptive weights based on integration level. All pillars normalize within job-role peer groups.

**Tech Stack:** Go (admin service), PostgreSQL, React dashboard

---

## Final Formula

```
final_score = α·AIQ + β·OSS + γ·GTS
```

| Integration Level | α (AIQ) | β (OSS) | γ (GTS) |
|-------------------|---------|---------|---------|
| Level 3 (GitHub + Jira + 飞书/钉钉) | 0.35 | 0.50 | 0.15 |
| Level 2 (any one collaboration tool) | 0.45 | 0.40 | 0.15 |
| Level 1 (no integration) | 0.60 | 0.25 | 0.15 |

Integration level is auto-detected from the count of active `webhook_configs` per tenant.

---

## Pillar 1: AIQ — AI Utilization Quality

All sub-metrics derived from `usage_records`. Available for every employee regardless of ecosystem.

### 1.1 Output Density (OD)
```
OD = median( completion_tokens / prompt_tokens )  per conversation, this month
```
- Group requests by `conversation_id`, compute ratio per conversation, take median across all conversations
- Measures prompt quality: well-crafted prompts produce longer, more substantive outputs
- Cap at historical 95th percentile (anti-gaming)

### 1.2 Usage Consistency (UC)
```
UC = active_days / working_days_in_month
```
- `active_day` = any day with ≥ 1 request
- `working_days` = weekdays (Mon–Fri) in month
- Measures: AI integrated into daily workflow vs. month-end spikes

### 1.3 Task Depth (TD)
```
TD = median(turns_per_conversation) × log( avg_tokens_per_conversation + 1 )
```
- `turns_per_conversation` = COUNT of requests sharing same `conversation_id`
- Rewards multi-turn problem-solving over one-off queries
- Exclude conversations in top 99th percentile of turns (bot/loop detection)

### 1.4 Cost Efficiency (CE)
```
CE = total_completion_tokens / total_scu_cost
```
- Rewards choosing cheaper models for appropriate tasks
- Normalized within peer group

### AIQ Composite
```
AIQ = 0.30·Z(OD) + 0.30·Z(UC) + 0.25·Z(TD) + 0.15·Z(CE)
```
All sub-metrics Z-scored within peer group before weighting. Final AIQ scaled 0–100.

---

## Pillar 2: OSS — Output Signal Score

### Level 3 (full integration) — quality-weighted events

| Platform | Event | Base Weight | Quality Multiplier |
|----------|-------|-------------|-------------------|
| GitHub | PR merged | 5 | `× (1 + min(lines_changed/500, 1))` — larger PRs score more |
| GitHub | PR merged | | `× 1/(1 + reopened_count)` — reopened PRs penalized |
| Jira | issue closed | story_points (default 3) | `× 1/(1 + max(cycle_time_z, 0))` — faster closure = bonus |
| 飞书/钉钉 | task completed | 2 | none (v1) |
| 飞书 | doc created | 2 | none (v1) |

### Level 2 (partial) — base weights only
Use existing `event_weights` from `webhook_configs`, no quality multipliers.

### Level 1 (no integration) — token-based fallback
```
OSS_fallback = log(total_completion_tokens + 1) / log(peer_median_completion_tokens + 1)
```
Completion tokens are a proxy for substantive output volume. Normalized against peer group median.

---

## Pillar 3: GTS — Growth Trajectory Score

```
GTS = 0.50 × MoM(t-1) + 0.33 × MoM(t-2) + 0.17 × MoM(t-3)
```
`MoM(t-n)` = month-over-month growth rate of `final_score` at month t-n.

- Recent months weighted heavier
- Requires ≥ 2 months of history; if unavailable, GTS weight redistributes to AIQ
- Prevents "one month spike then drop" from looking good in reviews

---

## Peer Group Logic

Peer groups determine Z-score normalization and rank calculation.

| Condition | Peer Group |
|-----------|-----------|
| `job_role` set | group by `job_role` |
| `job_role` not set | group by `role` (standard/senior/researcher) |
| New employee (< 90 days) | `cohort_YYYY-MM` (existing logic preserved) |

**Peer group population source:**
- With 飞书/钉钉 integration: sync `department` + `job_role` from org API
- Without integration: admin sets `job_role` on user record, or employee self-inputs on first login

---

## Anti-Gaming Rules

| Rule | Mechanism |
|------|-----------|
| Minimum activity | < 8 active working days → `score = NULL`, excluded from rankings |
| OD cap | completion/prompt ratio capped at 95th percentile of all-time tenant data |
| Anomaly detection | Monthly SCU > personal 3-month mean × 3σ → flag for review, score withheld |
| Loop detection | Conversations with turns > 99th percentile → excluded from TD calculation |
| Review smoothing | Performance review uses 3-month weighted average, not single month snapshot |

---

## Schema Changes Required

| Table | Change | Type |
|-------|--------|------|
| `usage_records` | Add `conversation_id UUID` | New column |
| `users` | Add `job_role VARCHAR(100)` | New column |
| `users` | Add `department VARCHAR(100)` | New column |
| `output_events` | Add `lines_changed INTEGER` | New column (GitHub) |
| `output_events` | Add `cycle_time_hours FLOAT` | New column (Jira) |
| `output_events` | Add `reopened_count INTEGER DEFAULT 0` | New column (GitHub) |
| `efficiency_snapshots` | Add `aiq_score FLOAT` | Score component |
| `efficiency_snapshots` | Add `oss_score FLOAT` | Score component |
| `efficiency_snapshots` | Add `gts_score FLOAT` | Score component |
| `efficiency_snapshots` | Add `integration_level INTEGER` | 1/2/3 at snapshot time |

---

## API Changes

### Admin
- `PATCH /api/users/:id` — support `job_role`, `department`
- `GET /api/admin/integration-level` — returns current auto-detected level

### Employee
- `GET /api/me/profile` — returns `job_role`, `department`
- `PATCH /api/me/profile` — employee self-inputs `job_role` on first login

### KPI
- `GET /api/me/kpi` — response now includes `aiq_score`, `oss_score`, `gts_score` breakdown
- `GET /api/kpi/snapshots` — same enrichment

---

## Gateway Changes

- Store `conversation_id` from request header/body into `usage_records`
- Clients (Claude Code, OpenAI SDK, etc.) pass conversation context; gateway extracts and persists it

---

## What Changes vs v1

| Dimension | v1 | v2 |
|-----------|----|----|
| No integration → score | 0 (broken) | AIQ + OSS_fallback (always works) |
| Measures AI quality | ❌ only consumption | ✅ 4 sub-metrics |
| Anti-gaming | ❌ none | ✅ 5 rules |
| Growth trajectory | ❌ none | ✅ GTS pillar |
| Role fairness | ❌ all in one group | ✅ job_role peer groups |
| PR/issue quality | ❌ count only | ✅ size, cycle time, reopen rate |
| Score explainability | ❌ one number | ✅ AIQ/OSS/GTS breakdown visible |
