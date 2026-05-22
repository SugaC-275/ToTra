# Gateway

The gateway is ToTra's LLM API proxy. It authenticates callers, enforces quota / PII /
policy controls, applies prompt compression and model auto-routing, proxies the request
to the configured LLM provider, and records usage. Built with Go and
[Fiber](https://gofiber.io/).

- **Port:** 8080 (`GATEWAY_PORT`)
- **Entry point:** `main.go`

## Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET  | `/health` | none | Liveness probe → `{"status":"ok"}` |
| GET  | `/metrics` | none | Prometheus metrics |
| POST | `/v1/chat/completions` | API key | Proxied chat completion (OpenAI-style) |
| POST | `/v1/messages` | API key | Proxied messages (Anthropic-style) |
| POST | `/v1/chat/completions/stream` | API key | Streaming chat completion |
| POST | `/v1/files/chat` | API key | File upload → parser → chat |

## Middleware chain

`/v1` requests pass through, in order (`main.go:71-81`):
metrics → auth → rate limiter → quota → PII → policy → compression → auto-router → agent.

## Configuration

Env vars (loaded in `config/`):

| Var | Required | Default | Purpose |
|-----|----------|---------|---------|
| `GATEWAY_PORT` | no | `8080` | Listen port |
| `POSTGRES_HOST` / `POSTGRES_PORT` / `POSTGRES_DB` / `POSTGRES_USER` / `POSTGRES_PASSWORD` | yes | — | PostgreSQL connection |
| `REDIS_HOST` / `REDIS_PORT` | no | `localhost` / `6379` | Redis connection |
| `REDIS_PASSWORD` | no | — | Redis auth |
| `GATEWAY_ENCRYPTION_KEY` | yes | — | 32-byte hex key for credential encryption |
| `JWT_SECRET` | yes | — | Shared JWT secret |
| `PARSER_URL` | no | `http://localhost:8090` | Parser service base URL |
| `AGENT_LOOP_LIMIT` | no | `20` | Max agent tool-call loops |

## Run & test

```bash
go run .          # start (requires postgres + redis)
go test ./...     # run tests
go vet ./...      # static checks
```

## Package layout

```
config/      env loading
handlers/    HTTP handlers (metrics, file-chat, stream proxy)
middleware/  auth, rate limit, quota, PII, policy, compression, routing, agent
providers/   LLM provider adapters (OpenAI, Anthropic, …)
storage/     PostgreSQL + Redis data access
tokenizer/   token counting / SCU cost
```
