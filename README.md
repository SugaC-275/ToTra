# ToTra

ToTra is a multi-service platform for managing LLM/cloud spend, enforcing compliance,
and orchestrating AI agents. Requests flow through a Go **gateway** that proxies to LLM
providers while applying quota, PII, and policy controls; an **admin** service holds the
business logic (auth, cost analytics, compliance, budgets); a Python **parser** extracts
text from uploaded documents; and a React **dashboard** provides the UI.

## Architecture

| Service     | Stack                 | Port | Purpose |
|-------------|-----------------------|------|---------|
| `gateway`   | Go 1.26 / Fiber       | 8080 | LLM API proxy — routing, caching, quota / PII / policy enforcement |
| `admin`     | Go 1.26 / Fiber       | 8081 | Business logic API — auth, cost, compliance, budgets, agents |
| `parser`    | Python 3.12 / FastAPI | 8090 | Document parsing (PDF / DOCX / PPTX → text) |
| `dashboard` | React 19 / Vite       | 3000 | Web UI |
| `postgres`  | PostgreSQL 16         | 5432 | Primary datastore |
| `redis`     | Redis 7               | 6379 | Quota cache, agent/session state |

Request flow: client → `gateway` → LLM provider (or `parser` for file uploads). The
`dashboard` talks to `admin`; in the Docker image nginx proxies `/api/` to `admin:8081`.

## Prerequisites

- Docker + Docker Compose — for the one-command setup.
- For local (non-Docker) development: Go 1.26.3, Node 20, Python 3.12.

## Quick start

```bash
cp .env.example .env
docker-compose --profile app up -d --wait
bash scripts/healthcheck.sh
```

`healthcheck.sh` starts the stack, waits for every service to report healthy, and runs an
admin-login smoke test. Once up:

- Dashboard — http://localhost:3000
- Admin API — http://localhost:8081
- Gateway API — http://localhost:8080

Stop / reset:

```bash
docker-compose --profile app down        # stop
docker-compose --profile app down -v     # stop + wipe the database
```

## Local development

Run the databases in Docker and each service on the host:

```bash
docker-compose up -d postgres redis          # databases only

cd gateway   && go run .                     # :8080
cd admin     && go run .                     # :8081
cd parser    && uvicorn main:app --port 8090 # :8090
cd dashboard && npm install && npm run dev   # :3000
```

## Testing

```bash
make test                       # all services

# or per service:
cd gateway   && go test ./...
cd admin     && go test ./...
cd dashboard && npm run test:run
cd parser    && pytest
```

## Default dev credentials

The seed migration (`infra/postgres/002_seed_dev.sql`) creates a demo tenant; dev
passwords are set by `scripts/set-dev-passwords/`. The default admin login is:

- Email: `admin@acme.com`
- Password: `totra-emp-dev-admin-key`

These are for local development only — never use them anywhere else.

## Project layout

```
gateway/    Go — LLM API proxy
admin/      Go — business logic API
parser/     Python — document parsing
dashboard/  React — web UI
infra/      PostgreSQL schema + migrations (001–032)
scripts/    healthcheck, smoke tests, dev utilities
docs/       additional documentation
```

## More documentation

- Per-service docs: [`gateway/README.md`](gateway/README.md),
  [`admin/README.md`](admin/README.md), [`parser/README.md`](parser/README.md),
  [`dashboard/README.md`](dashboard/README.md)
- [`infra/README.md`](infra/README.md) — database operations
- [`docs/feishu-webhook-setup.md`](docs/feishu-webhook-setup.md) — Feishu bot integration
