# Admin Service

The admin service holds ToTra's business logic: authentication, cost analytics,
compliance reporting, budget planning, agent session tracking, audit logging, and
SIEM / webhook integrations. Built with Go and [Fiber](https://gofiber.io/).

- **Port:** 8081 (`ADMIN_PORT`)
- **Entry point:** `main.go`

## Authentication

- `POST /api/auth/login` — exchange email + password for a JWT.
- Routes under the protected group require `Authorization: Bearer <token>` (JWT
  middleware) and pass an IP-allowlist check.
- OIDC login / callback routes are public (no JWT); see `RegisterOIDCPublicRoutes`.
- Internal compliance routes are guarded by the `INTERNAL_SECRET` shared secret.

## Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET  | `/health` | none | Liveness probe → `{"status":"ok"}` |
| GET  | `/metrics` | none | Prometheus metrics |
| POST | `/api/auth/login` | none | Issue JWT |
| *    | `/api/...` | JWT | Users, models, usage, quota, cost, compliance, budgets, agents, audit, SIEM, RBAC, … |

Route groups are registered in `main.go`; each group's handlers live in `api/`.

## Configuration

| Var | Required | Default | Purpose |
|-----|----------|---------|---------|
| `ADMIN_PORT` | no | `8081` | Listen port |
| `POSTGRES_HOST` / `POSTGRES_PORT` / `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | yes | — | PostgreSQL connection |
| `JWT_SECRET` | yes | — | JWT signing secret |
| `JWT_EXPIRY_HOURS` | no | `24` | Token lifetime (hours) |
| `ENCRYPTION_KEY` | yes | — | 32-byte hex key for credential encryption |
| `INTERNAL_SECRET` | no | — | Shared secret for internal compliance routes |

## Run & test

```bash
go run .          # start (requires postgres)
go test ./...     # run tests
go vet ./...      # static checks
```

## Package layout

```
api/         HTTP route groups + handlers
services/    business logic
handlers/    shared handlers (metrics)
middleware/  metrics, JWT
crypto/      encryption helpers
cron/        scheduled jobs
db/          connection pool
```
