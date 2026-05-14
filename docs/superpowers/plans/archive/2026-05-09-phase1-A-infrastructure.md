# ToTra Phase 1-A: Infrastructure Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Set up Docker Compose environment with PostgreSQL, Redis, and all DB migrations so every other agent can start work immediately.

**Architecture:** Single `docker-compose.yml` at project root spins up PostgreSQL 16, Redis 7, and placeholder containers for Gateway/Admin/Dashboard. DB migrations run as an init script. All services share one Docker network.

**Tech Stack:** Docker Compose v2, PostgreSQL 16, Redis 7, golang-migrate (for future migration management)

**Assigned Agent:** 🛠️ DevOps Engineer  
**Must complete before:** Plan B (Gateway), Plan C (Admin), Plan D (Dashboard) can all start

---

## File Map

```
ToTra/
├── docker-compose.yml              CREATE — all services
├── .env.example                    CREATE — all env vars documented
├── .env                            CREATE — local dev values (gitignored)
├── .gitignore                      CREATE
├── infra/
│   └── postgres/
│       └── 001_init.sql            CREATE — complete schema
└── gateway/                        CREATE empty dir (placeholder)
└── admin/                          CREATE empty dir (placeholder)
└── dashboard/                      CREATE empty dir (placeholder)
```

---

## Task 1: Project Root Scaffold

**Files:**
- Create: `.gitignore`
- Create: `.env.example`
- Create: `.env`

- [ ] **Step 1: Create .gitignore**

```
.env
*.local
node_modules/
dist/
vendor/
*.exe
*.test
.DS_Store
```

- [ ] **Step 2: Create .env.example**

```env
# PostgreSQL
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=totra
POSTGRES_USER=totra
POSTGRES_PASSWORD=totra_secret

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=

# Gateway
GATEWAY_PORT=8080
GATEWAY_ENCRYPTION_KEY=32-byte-hex-key-replace-in-prod

# Admin Service
ADMIN_PORT=8081
JWT_SECRET=replace-with-secure-random-secret
JWT_EXPIRY_HOURS=24

# Dashboard
DASHBOARD_PORT=3000
VITE_API_URL=http://localhost:8081
```

- [ ] **Step 3: Create .env (local dev values)**

```env
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_DB=totra
POSTGRES_USER=totra
POSTGRES_PASSWORD=totra_secret
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
GATEWAY_PORT=8080
GATEWAY_ENCRYPTION_KEY=0123456789abcdef0123456789abcdef
ADMIN_PORT=8081
JWT_SECRET=dev-jwt-secret-not-for-production
JWT_EXPIRY_HOURS=24
DASHBOARD_PORT=3000
VITE_API_URL=http://localhost:8081
```

- [ ] **Step 4: Create placeholder directories**

```bash
mkdir -p gateway admin dashboard infra/postgres
```

- [ ] **Step 5: Commit**

```bash
git init
git add .gitignore .env.example
git commit -m "chore: project scaffold with env config"
```

---

## Task 2: Database Schema

**Files:**
- Create: `infra/postgres/001_init.sql`

- [ ] **Step 1: Write complete schema**

```sql
-- infra/postgres/001_init.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Tenants (one per enterprise customer)
CREATE TABLE tenants (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    plan        TEXT NOT NULL CHECK (plan IN ('api_key', 'sso')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Users (employees)
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    email           TEXT NOT NULL,
    employee_id     TEXT,
    auth_type       TEXT NOT NULL CHECK (auth_type IN ('api_key', 'sso')),
    api_key_hash    TEXT,           -- SHA-256 of the employee key
    quota_scu       INTEGER NOT NULL DEFAULT 100000,
    quota_reset_day INTEGER NOT NULL DEFAULT 1 CHECK (quota_reset_day BETWEEN 1 AND 28),
    is_active       BOOLEAN NOT NULL DEFAULT TRUE,
    role            TEXT NOT NULL DEFAULT 'standard' CHECK (role IN ('standard', 'senior', 'researcher', 'admin')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, email)
);

-- Model configurations (per tenant)
CREATE TABLE model_configs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,   -- e.g. "gpt-4o", "claude-3-5-sonnet"
    provider             TEXT NOT NULL CHECK (provider IN ('openai', 'anthropic', 'gemini', 'local')),
    api_key_encrypted    TEXT,            -- AES-256 encrypted, NULL for local
    base_url             TEXT NOT NULL,   -- provider endpoint
    scu_rate             FLOAT NOT NULL DEFAULT 1.0,  -- 1 token = N SCU
    allowed_roles        TEXT[] NOT NULL DEFAULT ARRAY['standard','senior','researcher','admin'],
    is_active            BOOLEAN NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

-- Usage records (immutable, one row per request)
CREATE TABLE usage_records (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id),
    user_id          UUID NOT NULL REFERENCES users(id),
    model_config_id  UUID NOT NULL REFERENCES model_configs(id),
    prompt_tokens    INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    scu_cost         FLOAT NOT NULL,
    usd_cost         FLOAT NOT NULL,
    response_ms      INTEGER NOT NULL DEFAULT 0,
    request_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_usage_tenant_user ON usage_records (tenant_id, user_id, request_at DESC);
CREATE INDEX idx_usage_tenant_month ON usage_records (tenant_id, date_trunc('month', request_at));

-- Monthly summaries (pre-aggregated for dashboard performance)
CREATE TABLE monthly_summaries (
    tenant_id      UUID NOT NULL REFERENCES tenants(id),
    user_id        UUID NOT NULL REFERENCES users(id),
    year_month     CHAR(7) NOT NULL,   -- e.g. "2026-05"
    total_scu      FLOAT NOT NULL DEFAULT 0,
    total_usd      FLOAT NOT NULL DEFAULT 0,
    request_count  INTEGER NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id, year_month)
);

-- Quota change requests (approval workflow)
CREATE TABLE quota_requests (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id),
    user_id      UUID NOT NULL REFERENCES users(id),
    requested_by UUID NOT NULL REFERENCES users(id),
    new_quota    INTEGER NOT NULL,
    reason       TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reviewed_by  UUID REFERENCES users(id),
    reviewed_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- IP allowlist per tenant
CREATE TABLE ip_allowlist (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cidr       TEXT NOT NULL,    -- e.g. "192.168.1.0/24"
    label      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

- [ ] **Step 2: Verify SQL is valid**

```bash
docker run --rm -e POSTGRES_PASSWORD=test postgres:16 \
  psql -U postgres -c "\i /dev/stdin" < infra/postgres/001_init.sql 2>&1 | head -30
```

Expected: No ERROR lines.

- [ ] **Step 3: Commit**

```bash
git add infra/
git commit -m "feat: complete database schema for Phase 1"
```

---

## Task 3: Docker Compose

**Files:**
- Create: `docker-compose.yml`

- [ ] **Step 1: Write docker-compose.yml**

```yaml
# docker-compose.yml
version: "3.9"

services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: ${POSTGRES_DB}
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
    ports:
      - "${POSTGRES_PORT}:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./infra/postgres/001_init.sql:/docker-entrypoint-initdb.d/001_init.sql:ro
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 5s
      timeout: 5s
      retries: 10

  redis:
    image: redis:7-alpine
    command: redis-server --requirepass "${REDIS_PASSWORD:-}"
    ports:
      - "${REDIS_PORT}:6379"
    volumes:
      - redis_data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 10

  gateway:
    build: ./gateway
    ports:
      - "${GATEWAY_PORT}:8080"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    profiles: ["app"]   # only start with: docker compose --profile app up

  admin:
    build: ./admin
    ports:
      - "${ADMIN_PORT}:8081"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    profiles: ["app"]

  dashboard:
    build: ./dashboard
    ports:
      - "${DASHBOARD_PORT}:3000"
    env_file: .env
    profiles: ["app"]

volumes:
  postgres_data:
  redis_data:
```

- [ ] **Step 2: Start infrastructure services**

```bash
docker compose up -d postgres redis
```

Expected output:
```
✔ Container totra-postgres-1  Started
✔ Container totra-redis-1     Started
```

- [ ] **Step 3: Verify PostgreSQL has all tables**

```bash
docker compose exec postgres psql -U totra -d totra -c "\dt"
```

Expected: 7 tables listed (tenants, users, model_configs, usage_records, monthly_summaries, quota_requests, ip_allowlist)

- [ ] **Step 4: Verify Redis is reachable**

```bash
docker compose exec redis redis-cli ping
```

Expected: `PONG`

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml
git commit -m "feat: docker compose with postgres and redis"
```

---

## Task 4: Seed Data for Development

**Files:**
- Create: `infra/postgres/002_seed_dev.sql`

- [ ] **Step 1: Write seed script**

```sql
-- infra/postgres/002_seed_dev.sql
-- Development seed data only — never run in production

-- Tenant: Acme Corp (API Key plan)
INSERT INTO tenants (id, name, plan) VALUES
  ('00000000-0000-0000-0000-000000000001', 'Acme Corp', 'api_key');

-- Admin user
INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role)
VALUES (
  '00000000-0000-0000-0000-000000000010',
  '00000000-0000-0000-0000-000000000001',
  'Admin User', 'admin@acme.com', 'api_key',
  encode(sha256('totra-emp-dev-admin-key'), 'hex'),
  999999, 'admin'
);

-- Standard employee
INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, quota_scu, role)
VALUES (
  '00000000-0000-0000-0000-000000000011',
  '00000000-0000-0000-0000-000000000001',
  'Alice Engineer', 'alice@acme.com', 'api_key',
  encode(sha256('totra-emp-dev-alice-key'), 'hex'),
  50000, 'standard'
);

-- OpenAI model config (api_key_encrypted is plaintext here for dev ease)
INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
VALUES (
  '00000000-0000-0000-0000-000000000020',
  '00000000-0000-0000-0000-000000000001',
  'gpt-4o', 'openai', 'dev-openai-key', 'https://api.openai.com/v1', 2.0
);
```

- [ ] **Step 2: Apply seed**

```bash
docker compose exec -T postgres psql -U totra -d totra < infra/postgres/002_seed_dev.sql
```

Expected: `INSERT 0 1` lines, no errors.

- [ ] **Step 3: Verify**

```bash
docker compose exec postgres psql -U totra -d totra -c "SELECT name, plan FROM tenants;"
```

Expected:
```
   name    |  plan   
-----------+---------
 Acme Corp | api_key
```

- [ ] **Step 4: Commit**

```bash
git add infra/postgres/002_seed_dev.sql
git commit -m "feat: dev seed data for local testing"
```

---

## Task 5: Write Infrastructure README

**Files:**
- Create: `infra/README.md`

- [ ] **Step 1: Write README**

```markdown
# Infrastructure

## Quick Start

```bash
cp .env.example .env
docker compose up -d postgres redis
```

## Services

| Service | Port | Purpose |
|---|---|---|
| PostgreSQL | 5432 | Primary database |
| Redis | 6379 | Real-time quota cache |

## Start Everything

```bash
docker compose --profile app up -d
```

## Reset Database

```bash
docker compose down -v && docker compose up -d postgres redis
```

## Connect to PostgreSQL

```bash
docker compose exec postgres psql -U totra -d totra
```

## Connect to Redis

```bash
docker compose exec redis redis-cli
```
```

- [ ] **Step 2: Commit**

```bash
git add infra/README.md
git commit -m "docs: infrastructure setup guide"
```
