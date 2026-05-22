# ToTra Gap Closure Design

**Date:** 2026-05-22  
**Status:** Approved — full autonomous implementation

---

## Scope

Nine parallel workstreams closing all identified gaps across three layers.

---

## Layer 1 — Reliability / Ops

### R1: SSE Streaming (gateway-stream)

- Detect `"stream": true` in request body at proxy handler
- Add `ForwardStream(ctx, body, onChunk func([]byte))` to each provider adapter
- New handler `gateway/handlers/stream_proxy.go` — writes SSE chunks as they arrive
- Integration: wire into `gateway/main.go` alongside existing `makeProxyHandler`

### R2: Retry + Fallback Model (gateway-reliability)

- `gateway/providers/retry.go` — exponential backoff wrapper (max 3 attempts, 500ms base)
- `infra/postgres/026_fallback_model.sql` — add `fallback_model_config_id` FK to `model_configs`
- `gateway/middleware/fallback.go` — on 502, look up fallback model and re-route
- Integration: wrap provider in retry, add fallback middleware to chain

### R3: Prometheus Metrics (observability)

- `gateway/middleware/metrics.go` — counters + histograms: req count, latency (by model/tenant), error rate, token usage, cache hit rate
- `admin/middleware/metrics.go` — admin API req count + latency
- `/metrics` endpoint on both services
- No distributed tracing in this phase

### R4: Request-rate Limiting (rate-limiter)

- `gateway/storage/rate_limit_store.go` — Redis sliding window (per-user per-minute)
- `gateway/middleware/rate_limiter.go` — 429 + `Retry-After` header on breach
- `infra/postgres/027_rate_limits.sql` — per-tenant rate limit config table

---

## Layer 2 — Enterprise Features

### E1: SSO / OIDC (sso-agent)

- `infra/postgres/028_oidc_config.sql` — `oidc_configs` (tenant_id, issuer, client_id, client_secret_enc, redirect_uri, enabled)
- `admin/services/oidc.go` — GetConfig, SetConfig, ExchangeCode, GetUserInfo
- `admin/api/oidc.go` — GET/PUT `/api/admin/sso/config`, GET `/api/auth/oidc/login`, GET `/api/auth/oidc/callback`
- `dashboard/src/pages/admin/SSOPage.tsx` — OIDC config form + test connection button

### E2: Fine-grained RBAC (rbac-agent)

- New roles: `dept_admin`, `auditor`, `compliance_officer` (in addition to `admin`, `user`)
- `infra/postgres/029_rbac_roles.sql` — `user_roles` table
- `admin/services/rbac.go` — GetUserRoles, AssignRole, RevokeRole, HasPermission
- `admin/api/rbac.go` — GET/POST/DELETE `/api/admin/users/:id/roles`
- Extend `admin/api/middleware.go` `RequireRole` to check `user_roles` table

### E3: Real-time Alert Push (alerting-agent)

- `infra/postgres/030_alert_configs.sql` — `alert_delivery_configs` (channel_type: slack/email/webhook, destination, event_types[])
- `admin/services/alert_push.go` — DeliverToSlack, DeliverToWebhook, DeliverToEmail
- `admin/api/alert_config.go` — CRUD `/api/admin/alert-configs`
- Extend `admin/services/budget_alert.go` to call AlertPushService after DB write
- `dashboard/src/pages/admin/AlertConfigPage.tsx`

### E4: Employee Self-Service (self-service-agent)

- `infra/postgres/031_quota_requests.sql` — `quota_requests` table
- `admin/services/employee_self_service.go` — PII violations (own), quota request, CSV export
- `admin/api/employee_self_service.go` — GET `/api/employee/pii-violations`, POST `/api/employee/quota-requests`, GET `/api/employee/usage-export`
- `dashboard/src/pages/employee/SelfServicePage.tsx`

### E5: Data Retention Dashboard (retention-ui-agent)

- `DataRetentionService` already exists in `admin/services/data_retention.go`
- `admin/api/data_retention.go` — GET/PUT `/api/admin/data-retention`, POST `/api/admin/data-retention/run-cleanup`
- `dashboard/src/pages/admin/DataRetentionPage.tsx` — retention setting, last cleanup stats, manual trigger

---

## Integration Pass (post-parallel)

After all 9 agents complete, a single integration pass wires:
- `gateway/main.go`: new middleware chain entries (metrics, rate-limiter, fallback), streaming route
- `admin/main.go`: new route registrations (oidc, rbac, alert-configs, employee-self-service, data-retention)
- `dashboard/src/App.tsx`: new page routes (SSO, RBAC, AlertConfig, SelfService, DataRetention)

---

## Migration Numbering

| Migration | Owner |
|-----------|-------|
| 026 | R2 (fallback model) |
| 027 | R4 (rate limits) |
| 028 | E1 (oidc config) |
| 029 | E2 (rbac roles) |
| 030 | E3 (alert configs) |
| 031 | E4 (quota requests) |
