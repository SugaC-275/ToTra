# ToTra

**ToTra** is an open-source LLM gateway and AI spend-management platform. It sits between your applications and LLM providers (OpenAI, Anthropic, Gemini, local models), enforcing quota, PII, and policy controls while giving you deep cost analytics, compliance reporting, budget management, and an employee self-service portal — all in one self-hosted stack.

---

## Features

**Gateway / Proxy**
- OpenAI-compatible and Anthropic-compatible API endpoints
- Multi-provider routing — OpenAI, Anthropic, Gemini, local models
- Per-tenant token quota with Redis-backed enforcement
- Real-time multilingual PII detection and blocking before requests reach any LLM
- Policy rule engine (allow/deny/transform based on prompt content)
- Prompt compression to reduce token spend
- Intelligent model auto-routing and fallback
- Rate limiting, API-key auth, IP allowlist
- Streaming (`text/event-stream`) proxy
- File upload → parse → chat pipeline (PDF / DOCX / PPTX)
- SIEM event emission per request

**Cost & Analytics**
- Per-tenant, per-model, per-user cost tracking
- Cost optimization recommendations and savings reports
- Budget planner with alert thresholds
- ROI reports and procurement analytics
- Smart-routing cost analysis
- Anomaly detection on spend

**Compliance**
- GDPR compliance workflows and data-subject request handling
- Industry-specific compliance frameworks
- PII violation logging
- Data retention policies with automated cleanup
- Full audit log

**Administration**
- JWT authentication + OIDC / SSO integration
- Role-based access control (RBAC)
- User and team management
- Model catalogue management
- Bot and integration configurations (Slack, Feishu, GitLab, Confluence, …)
- HR sync connector
- Quota request / approval workflow
- Alert configuration with webhook and SIEM targets

**Observability**
- Prometheus metrics on every service (`/metrics`)
- Per-request routing and usage events stored in PostgreSQL

**PII Detection — Language Coverage**

The gateway scans every request body before it reaches any LLM provider. Blocked requests return `422` and emit a SIEM event. Covered languages and data types:

| Language | Detected PII types |
|----------|--------------------|
| Universal | Email, credit card (grouped/bare), IBAN, SWIFT/BIC, ICD medical codes |
| Chinese | ID card, phone, bank account, unified credit code, contract amount, transaction ID, loan account, securities account, insurance policy, patient ID |
| English | US SSN, US/UK phone, UK NI number, US passport, driver's license, date of birth, medical record number |
| Japanese | My Number (個人番号), phone, postal code (〒), passport, bank account, health insurance number |
| Korean | RRN (주민등록번호), phone, passport, business registration number, driver's license |
| French | NIR (numéro de sécurité sociale), phone |
| German | Steueridentifikationsnummer, phone, passport |
| Spanish | DNI / NIE, phone |
| Italian | Codice Fiscale, Partita IVA, phone |
| Dutch | BSN (Burgerservicenummer), phone, passport |
| Polish | PESEL, NIP (tax number), phone |
| Swedish | Personnummer, phone |
| Portuguese | NIF (tax number), phone |
| Belgian | National register number, phone |
| Swiss | AHV / AVS number, phone |
| Danish | CPR number, phone |
| Finnish | HETU (henkilötunnus), phone |
| Norwegian | Fødselsnummer, phone |
| Arabic (SA) | National ID, Iqama (residency number), phone |
| Arabic (UAE) | Emirates ID, phone |
| Arabic (EG) | National ID (الرقم القومي), phone |
| Arabic (KW) | Civil ID (الرقم المدني), phone |
| Arabic (QA) | QID, phone |
| Arabic (MA) | CIN (carte nationale), phone |
| Arabic (DZ) | NIN (Numéro d'Identification National) |
| Arabic (LB) | Phone |
| Arabic (JO) | Phone |
| Pan-Arab | Passport, bank account / IBAN keyword |

---

## Architecture

```
┌──────────────┐         ┌───────────────────────────────────────────────┐
│   Clients    │──────→  │              Gateway  :8080                    │
│ (apps / curl)│         │  auth → rate-limit → quota → PII → policy     │
└──────────────┘         │  → compress → auto-router → agent → provider  │
                         └──────────┬──────────────────────────────┬──────┘
                                    │ usage events                  │ proxied
                                    ▼                               ▼
                         ┌──────────────────┐         ┌──────────────────┐
                         │  Admin  :8081    │         │  LLM Providers   │
                         │  auth, cost,     │         │  OpenAI / Anthropic│
                         │  compliance,     │         │  Gemini / local  │
                         │  budgets, agents │         └──────────────────┘
                         └──────────────────┘
                                    │ /api/
                         ┌──────────────────┐
                         │ Dashboard :3000  │
                         │ React admin UI   │
                         │ + employee portal│
                         └──────────────────┘
                                    │ file uploads
                         ┌──────────────────┐
                         │  Parser  :8090   │
                         │  PDF/DOCX/PPTX   │
                         │  → plain text    │
                         └──────────────────┘
```

| Service     | Stack                  | Port | Purpose                                               |
|-------------|------------------------|------|-------------------------------------------------------|
| `gateway`   | Go 1.26 / Fiber        | 8080 | LLM proxy — routing, caching, quota / PII / policy   |
| `admin`     | Go 1.26 / Fiber        | 8081 | Business logic — auth, cost, compliance, budgets      |
| `parser`    | Python 3.12 / FastAPI  | 8090 | Document parsing (PDF / DOCX / PPTX → text)           |
| `dashboard` | React 19 / Vite        | 3000 | Web UI (admin console + employee self-service)        |
| `postgres`  | PostgreSQL 16          | 5432 | Primary datastore                                     |
| `redis`     | Redis 7                | 6379 | Quota cache, agent/session state                      |

---

## Quick Start

### Prerequisites

- [Docker](https://docs.docker.com/get-docker/) + Docker Compose

### Run with Docker

```bash
git clone https://github.com/<your-org>/totra.git
cd totra
cp .env.example .env          # fill in secrets (see Configuration below)
docker-compose --profile app up -d --wait
bash scripts/healthcheck.sh   # waits for all services, runs a smoke test
```

Services will be available at:

| URL | Service |
|-----|---------|
| http://localhost:3000 | Dashboard (web UI) |
| http://localhost:8081 | Admin API |
| http://localhost:8080 | Gateway (LLM proxy) |

Stop / reset:

```bash
docker-compose --profile app down        # stop, keep data
docker-compose --profile app down -v     # stop + wipe database
```

### Default dev credentials

> For local development only — never use these in production.

| Field    | Value                       |
|----------|-----------------------------|
| Email    | `admin@acme.com`            |
| Password | `totra-emp-dev-admin-key`   |

---

## Local Development

Run databases in Docker; run each service on the host:

```bash
# 1. Start databases
docker-compose up -d postgres redis

# 2. Start services (each in its own terminal)
cd gateway   && go run .
cd admin     && go run .
cd parser    && uvicorn main:app --port 8090
cd dashboard && npm install && npm run dev
```

### Prerequisites for local dev

| Tool          | Version  |
|---------------|----------|
| Go            | 1.26+    |
| Node.js       | 20+      |
| Python        | 3.12+    |
| Docker        | any recent |

---

## Configuration

Copy `.env.example` to `.env` and set the following variables:

### Required

| Variable             | Services            | Description                                                    |
|----------------------|---------------------|----------------------------------------------------------------|
| `POSTGRES_HOST`      | gateway, admin      | PostgreSQL host (default: `localhost`)                         |
| `POSTGRES_PORT`      | gateway, admin      | PostgreSQL port (default: `5432`)                              |
| `POSTGRES_DB`        | gateway, admin      | Database name                                                  |
| `POSTGRES_USER`      | gateway, admin      | Database user                                                  |
| `POSTGRES_PASSWORD`  | gateway, admin      | Database password                                              |
| `JWT_SECRET`         | gateway, admin      | Secret used to sign/verify JWTs (shared)                       |
| `ENCRYPTION_KEY`     | admin               | 32-byte hex key for encrypting stored credentials              |
| `GATEWAY_ENCRYPTION_KEY` | gateway         | 32-byte hex key for gateway credential store                   |

### Optional

| Variable             | Service   | Default               | Description                          |
|----------------------|-----------|-----------------------|--------------------------------------|
| `GATEWAY_PORT`       | gateway   | `8080`                | Gateway listen port                  |
| `ADMIN_PORT`         | admin     | `8081`                | Admin API listen port                |
| `PARSER_PORT`        | parser    | `8090`                | Parser listen port                   |
| `DASHBOARD_PORT`     | dashboard | `3000`                | Dashboard listen port                |
| `REDIS_HOST`         | gateway   | `localhost`           | Redis host                           |
| `REDIS_PORT`         | gateway   | `6379`                | Redis port                           |
| `REDIS_PASSWORD`     | gateway   | —                     | Redis auth password                  |
| `PARSER_URL`         | gateway   | `http://localhost:8090` | Parser service base URL            |
| `JWT_EXPIRY_HOURS`   | admin     | `24`                  | JWT token lifetime in hours          |
| `AGENT_LOOP_LIMIT`   | gateway   | `20`                  | Max tool-call loops per agent request|
| `INTERNAL_SECRET`    | admin     | —                     | Shared secret for internal compliance routes |
| `MAX_UPLOAD_BYTES`   | parser    | `10485760` (10 MiB)   | Maximum accepted file upload size    |
| `VITE_API_URL`       | dashboard | `http://localhost:8081` | Admin API base URL (dev)           |

---

## API Overview

### Gateway — LLM Proxy (`localhost:8080`)

Authenticate with `Authorization: Bearer <api-key>` or `x-api-key: <api-key>`.

| Method | Path                              | Description                              |
|--------|-----------------------------------|------------------------------------------|
| GET    | `/health`                         | Liveness probe                           |
| GET    | `/metrics`                        | Prometheus metrics                       |
| POST   | `/v1/chat/completions`            | Chat completion (OpenAI-compatible)      |
| POST   | `/v1/messages`                    | Messages (Anthropic-compatible)          |
| POST   | `/v1/chat/completions/stream`     | Streaming chat completion                |
| POST   | `/v1/files/chat`                  | File upload → parse → chat completion   |

### Admin API (`localhost:8081`)

Authenticate with `Authorization: Bearer <jwt>` (obtain from `POST /api/auth/login`).

| Group         | Endpoints |
|---------------|-----------|
| Auth          | `POST /api/auth/login`, OIDC login/callback |
| Users         | CRUD users, password management             |
| Models        | Model catalogue — enable/disable/configure  |
| Usage         | Token usage by user, model, date range      |
| Quota         | Quota limits, request/approval workflow     |
| Cost          | Cost analytics, optimization, savings, ROI  |
| Compliance    | GDPR, industry frameworks, checklists, PII  |
| Budgets       | Budget plans, alert thresholds              |
| Agents        | Agent session tracking                      |
| Audit         | Immutable audit log                         |
| SIEM          | SIEM target configuration                   |
| RBAC          | Roles, permissions                          |
| IP Allowlist  | Per-tenant IP allowlists                    |
| Integrations  | Bots, webhooks, HR sync, SSO                |
| Data Retention| Retention policies, cleanup log             |
| Alerts        | Alert configurations                        |
| Procurement   | Procurement reports                         |
| Employee      | Self-service usage view                     |

### Parser (`localhost:8090`)

| Method | Path      | Description                                       |
|--------|-----------|---------------------------------------------------|
| GET    | `/health` | Liveness probe                                    |
| POST   | `/parse`  | Multipart upload → extract text (PDF/DOCX/PPTX)  |

---

## Testing

```bash
make test                     # run all services

# per service:
cd gateway   && go test ./...
cd admin     && go test ./...
cd dashboard && npm run test:run
cd parser    && pytest
```

---

## Project Layout

```
gateway/     Go — LLM proxy (auth, quota, PII, policy, compression, routing, providers)
admin/       Go — business logic (auth, cost, compliance, budgets, agents, RBAC, …)
parser/      Python — document parsing (PDF / DOCX / PPTX → text)
dashboard/   React — admin console and employee self-service portal
infra/       PostgreSQL schema + 32 migration files
scripts/     healthcheck, smoke tests, dev utilities
docs/        additional documentation
```

---

## Integrations

- **LLM Providers**: OpenAI, Anthropic (Claude), Google Gemini, local/self-hosted models
- **SSO**: OIDC-compatible identity providers
- **SIEM**: configurable webhook targets
- **Chat / Bots**: Feishu, Slack, GitLab, Confluence
- **HR Systems**: HR sync connector
- **Observability**: Prometheus-compatible metrics on every service

---

## Documentation

- [Gateway](gateway/README.md) — middleware chain, endpoints, env vars
- [Admin](admin/README.md) — route groups, configuration, package layout
- [Parser](parser/README.md) — endpoints, supported formats, configuration
- [Dashboard](dashboard/README.md) — pages, authentication, scripts
- [Infrastructure](infra/README.md) — database operations, migrations
- [Feishu Webhook Setup](docs/feishu-webhook-setup.md) — Feishu bot integration

---

## Tech Stack

| Layer       | Technology |
|-------------|------------|
| Gateway     | Go 1.26, [Fiber](https://gofiber.io/), Redis 7 |
| Admin API   | Go 1.26, [Fiber](https://gofiber.io/), PostgreSQL 16 |
| Parser      | Python 3.12, [FastAPI](https://fastapi.tiangolo.com/), PyMuPDF, python-docx, python-pptx |
| Dashboard   | React 19, Vite, Tailwind CSS 4, TanStack Query, Recharts |
| Infra       | Docker Compose, PostgreSQL 16, Redis 7 |
| Observability | Prometheus metrics |

---

## License

[MIT](LICENSE)
