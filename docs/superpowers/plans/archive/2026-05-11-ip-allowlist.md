# IP Allowlist Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow tenant admins to configure CIDR-based IP allowlists so that, when any entries exist, requests from non-matching IPs are rejected with 403 on all protected routes.

**Architecture:** A new `tenant_ip_allowlists` table stores per-tenant CIDRs. `IPAllowlistService` in `admin/services/ip_allowlist.go` provides CRUD and a `LoadTenantCIDRs` method used by `IPAllowlistMiddleware`, which is registered on the `protected` Fiber group immediately after the JWT middleware. Three REST endpoints (GET/POST/DELETE `/api/admin/ip-allowlist`) are exposed from `admin/api/ip_allowlist.go` and wired in `admin/main.go`. The React page at `/admin/ip-allowlist` lists entries, adds new ones via a CIDR + label form, and deletes entries; it is linked from the admin sidebar.

**Tech Stack:** Go 1.26 + Fiber v2, pgx/v5, `net` stdlib for CIDR matching; React 19 + TypeScript, TanStack Query v5, React Router v6, Tailwind v4, existing `Card`/`Button`/`Input` components in `dashboard/src/components/ui/`.

---

## File Map

| Task | Files |
|------|-------|
| 1 | `infra/postgres/006_ip_allowlist.sql` (create) |
| 2 | `admin/services/ip_allowlist.go` (create) |
| 3 | `admin/api/ip_allowlist.go` (create), `admin/main.go` (modify) |
| 4 | `dashboard/src/api/client.ts` (modify) |
| 5 | `dashboard/src/pages/admin/IPAllowlistPage.tsx` (create) |
| 6 | `dashboard/src/App.tsx` (modify), `dashboard/src/components/Layout.tsx` (modify) |

---

## Task 1: DB migration — create `tenant_ip_allowlists` table

**Files:**
- Create: `infra/postgres/006_ip_allowlist.sql`

- [ ] **Step 1: Create `infra/postgres/006_ip_allowlist.sql`**

```sql
CREATE TABLE IF NOT EXISTS tenant_ip_allowlists (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    cidr       TEXT        NOT NULL,
    label      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, cidr)
);

CREATE INDEX IF NOT EXISTS idx_tenant_ip_allowlists_tenant_id
    ON tenant_ip_allowlists (tenant_id);
```

- [ ] **Step 2: Apply the migration**

```bash
psql "$POSTGRES_DSN" -f /Users/sugac.275/ToTra/infra/postgres/006_ip_allowlist.sql
```

Expected output:
```
CREATE TABLE
CREATE INDEX
```

If `$POSTGRES_DSN` is not set, use the DSN from your local `.env` or `config.Load()` source. For Docker Compose: `psql "postgres://totra:totra@localhost:5432/totra" -f infra/postgres/006_ip_allowlist.sql`

- [ ] **Step 3: Verify table exists**

```bash
psql "$POSTGRES_DSN" -c "\d tenant_ip_allowlists"
```

Expected: table description showing `id`, `tenant_id`, `cidr`, `label`, `created_at` columns, primary key constraint, unique constraint on `(tenant_id, cidr)`, and the index.

- [ ] **Step 4: Commit**

```bash
git add infra/postgres/006_ip_allowlist.sql
git commit -m "feat(db): add tenant_ip_allowlists migration"
```

---

## Task 2: Service layer — `admin/services/ip_allowlist.go`

**Files:**
- Create: `admin/services/ip_allowlist.go`

This file contains the `IPAllowlistEntry` struct, `IPAllowlistService` with three CRUD methods (`List`, `Add`, `Delete`) and a `LoadTenantCIDRs` helper used by the middleware, plus the `IPAllowlistMiddleware` function.

The middleware is co-located in the services package because it depends only on `IPAllowlistService` and `Claims` — both already in `services`. It follows the same closure-over-service pattern as `NewJWTMiddleware` in `admin/api/middleware.go`.

- [ ] **Step 1: Write the failing tests for the service in `admin/services/ip_allowlist_test.go`**

```go
package services_test

import (
	"context"
	"testing"

	"github.com/pashagolub/pgxmock/v3"
	"github.com/yourorg/totra/admin/services"
)

func newAllowlistPool(t *testing.T) (pgxmock.PgxPoolIface, *services.IPAllowlistService) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatal(err)
	}
	svc := services.NewIPAllowlistService(mock)
	return mock, svc
}

func TestIPAllowlistList(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "cidr", "label", "created_at"}).
		AddRow("id-1", "10.0.0.0/8", "office", "2026-05-11T00:00:00Z").
		AddRow("id-2", "203.0.113.5/32", "vpn", "2026-05-11T00:00:00Z")
	mock.ExpectQuery(`SELECT id, cidr, label, created_at FROM tenant_ip_allowlists`).
		WithArgs("tenant-1").
		WillReturnRows(rows)

	entries, err := svc.List(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].CIDR != "10.0.0.0/8" {
		t.Errorf("unexpected CIDR: %s", entries[0].CIDR)
	}
}

func TestIPAllowlistAdd(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"id", "cidr", "label", "created_at"}).
		AddRow("new-id", "192.168.1.0/24", "branch", "2026-05-11T00:00:00Z")
	mock.ExpectQuery(`INSERT INTO tenant_ip_allowlists`).
		WithArgs("tenant-1", "192.168.1.0/24", "branch").
		WillReturnRows(rows)

	entry, err := svc.Add(context.Background(), "tenant-1", "192.168.1.0/24", "branch")
	if err != nil {
		t.Fatal(err)
	}
	if entry.ID != "new-id" {
		t.Errorf("unexpected ID: %s", entry.ID)
	}
}

func TestIPAllowlistDelete(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	mock.ExpectExec(`DELETE FROM tenant_ip_allowlists`).
		WithArgs("entry-id", "tenant-1").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := svc.Delete(context.Background(), "tenant-1", "entry-id")
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadTenantCIDRs(t *testing.T) {
	mock, svc := newAllowlistPool(t)
	defer mock.Close()

	rows := pgxmock.NewRows([]string{"cidr"}).
		AddRow("10.0.0.0/8").
		AddRow("203.0.113.5/32")
	mock.ExpectQuery(`SELECT cidr FROM tenant_ip_allowlists`).
		WithArgs("tenant-1").
		WillReturnRows(rows)

	cidrs, err := svc.LoadTenantCIDRs(context.Background(), "tenant-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cidrs) != 2 {
		t.Fatalf("expected 2, got %d", len(cidrs))
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run TestIPAllowlist -v
```

Expected: compilation error — `services.IPAllowlistService` undefined.

Note: `pgxmock/v3` accepts a `pgxpool.Pool`-compatible interface. If it is not yet in `go.mod`, run `go get github.com/pashagolub/pgxmock/v3` first.

- [ ] **Step 3: Create `admin/services/ip_allowlist.go`**

```go
package services

import (
	"context"
	"fmt"
	"net"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IPAllowlistEntry is a single CIDR entry for a tenant.
type IPAllowlistEntry struct {
	ID        string `json:"id"`
	CIDR      string `json:"cidr"`
	Label     string `json:"label"`
	CreatedAt string `json:"created_at"`
}

// IPAllowlistService manages per-tenant IP allowlist CRUD and middleware.
type IPAllowlistService struct {
	pool *pgxpool.Pool
}

// NewIPAllowlistService constructs the service.
func NewIPAllowlistService(pool *pgxpool.Pool) *IPAllowlistService {
	return &IPAllowlistService{pool: pool}
}

// List returns all allowlist entries for the given tenant, ordered by created_at.
func (s *IPAllowlistService) List(ctx context.Context, tenantID string) ([]*IPAllowlistEntry, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, cidr, label, created_at
		FROM tenant_ip_allowlists
		WHERE tenant_id = $1
		ORDER BY created_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*IPAllowlistEntry
	for rows.Next() {
		e := &IPAllowlistEntry{}
		if err := rows.Scan(&e.ID, &e.CIDR, &e.Label, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Add inserts a new CIDR entry for the tenant. Returns the created entry.
// Returns an error if the CIDR is invalid or already exists for the tenant.
func (s *IPAllowlistService) Add(ctx context.Context, tenantID, cidr, label string) (*IPAllowlistEntry, error) {
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return nil, fmt.Errorf("invalid CIDR %q: %w", cidr, err)
	}
	e := &IPAllowlistEntry{}
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenant_ip_allowlists (tenant_id, cidr, label)
		VALUES ($1, $2, $3)
		RETURNING id, cidr, label, created_at`,
		tenantID, cidr, label,
	).Scan(&e.ID, &e.CIDR, &e.Label, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// Delete removes a specific allowlist entry by id, scoped to the tenant.
func (s *IPAllowlistService) Delete(ctx context.Context, tenantID, id string) error {
	_, err := s.pool.Exec(ctx, `
		DELETE FROM tenant_ip_allowlists
		WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	return err
}

// LoadTenantCIDRs returns the raw CIDR strings for a tenant. Used by the middleware.
func (s *IPAllowlistService) LoadTenantCIDRs(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT cidr FROM tenant_ip_allowlists
		WHERE tenant_id = $1`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cidrs []string
	for rows.Next() {
		var cidr string
		if err := rows.Scan(&cidr); err != nil {
			return nil, err
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs, rows.Err()
}

// IPAllowlistMiddleware gates protected routes by tenant IP allowlist.
// If the tenant has zero entries, all IPs are allowed (opt-in feature).
// If the tenant has one or more entries, only matching IPs pass; others get 403.
// Must be registered AFTER JWT middleware so that claims are available.
func IPAllowlistMiddleware(svc *IPAllowlistService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims, ok := c.Locals("claims").(*Claims)
		if !ok || claims == nil {
			// Not yet authenticated — let JWT middleware handle the rejection.
			return c.Next()
		}

		cidrs, err := svc.LoadTenantCIDRs(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "allowlist check failed"})
		}

		// Zero entries → feature is opt-out; allow all.
		if len(cidrs) == 0 {
			return c.Next()
		}

		clientIP := net.ParseIP(c.IP())
		if clientIP == nil {
			return c.Status(403).JSON(fiber.Map{"error": "forbidden: unresolvable client IP"})
		}

		for _, cidrStr := range cidrs {
			_, network, err := net.ParseCIDR(cidrStr)
			if err != nil {
				// Malformed entry in DB — skip it rather than blocking all traffic.
				continue
			}
			if network.Contains(clientIP) {
				return c.Next()
			}
		}

		return c.Status(403).JSON(fiber.Map{"error": "forbidden: IP not in allowlist"})
	}
}
```

- [ ] **Step 4: Run the tests to confirm they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/ -run TestIPAllowlist -v
```

Expected output:
```
--- PASS: TestIPAllowlistList
--- PASS: TestIPAllowlistAdd
--- PASS: TestIPAllowlistDelete
--- PASS: TestLoadTenantCIDRs
PASS
```

- [ ] **Step 5: Build to verify no compilation errors**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add admin/services/ip_allowlist.go admin/services/ip_allowlist_test.go
git commit -m "feat(services): add IPAllowlistService and IPAllowlistMiddleware"
```

---

## Task 3: API layer — `admin/api/ip_allowlist.go` + wire into `admin/main.go`

**Files:**
- Create: `admin/api/ip_allowlist.go`
- Modify: `admin/main.go`

Three endpoints, all admin-only:
- `GET  /api/admin/ip-allowlist`         — list entries
- `POST /api/admin/ip-allowlist`         — add entry (`{"cidr":"...","label":"..."}`)
- `DELETE /api/admin/ip-allowlist/:id`   — delete entry by UUID

The middleware is applied to the existing `protected` group immediately after the JWT middleware, before any route registrations on that group.

- [ ] **Step 1: Write the failing handler tests in `admin/api/ip_allowlist_test.go`**

```go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

// stubAllowlistService satisfies the interface used by the handlers.
type stubAllowlistService struct {
	entries []*services.IPAllowlistEntry
	addErr  error
	delErr  error
}

func (s *stubAllowlistService) List(_ context.Context, _ string) ([]*services.IPAllowlistEntry, error) {
	return s.entries, nil
}
func (s *stubAllowlistService) Add(_ context.Context, _, cidr, label string) (*services.IPAllowlistEntry, error) {
	if s.addErr != nil {
		return nil, s.addErr
	}
	return &services.IPAllowlistEntry{ID: "new-id", CIDR: cidr, Label: label, CreatedAt: "2026-05-11T00:00:00Z"}, nil
}
func (s *stubAllowlistService) Delete(_ context.Context, _, _ string) error {
	return s.delErr
}

func claimsCtx(app *fiber.App) {
	// inject admin claims before the handler under test
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{
			UserID: "u1", TenantID: "t1", Role: "admin",
		})
		return c.Next()
	})
}

func TestListIPAllowlist(t *testing.T) {
	app := fiber.New()
	claimsCtx(app)
	stub := &stubAllowlistService{
		entries: []*services.IPAllowlistEntry{
			{ID: "id-1", CIDR: "10.0.0.0/8", Label: "office", CreatedAt: "2026-05-11T00:00:00Z"},
		},
	}
	api.RegisterIPAllowlistRoutes(app, stub)

	req := httptest.NewRequest("GET", "/api/admin/ip-allowlist", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	entries := body["entries"].([]interface{})
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestAddIPAllowlist(t *testing.T) {
	app := fiber.New()
	claimsCtx(app)
	stub := &stubAllowlistService{}
	api.RegisterIPAllowlistRoutes(app, stub)

	payload, _ := json.Marshal(map[string]string{"cidr": "192.168.0.0/16", "label": "hq"})
	req := httptest.NewRequest("POST", "/api/admin/ip-allowlist", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 201 {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}
}

func TestAddIPAllowlist_NonAdmin(t *testing.T) {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "u2", TenantID: "t1", Role: "user"})
		return c.Next()
	})
	stub := &stubAllowlistService{}
	api.RegisterIPAllowlistRoutes(app, stub)

	payload, _ := json.Marshal(map[string]string{"cidr": "1.2.3.4/32", "label": ""})
	req := httptest.NewRequest("POST", "/api/admin/ip-allowlist", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != 403 {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}
}

func TestDeleteIPAllowlist(t *testing.T) {
	app := fiber.New()
	claimsCtx(app)
	stub := &stubAllowlistService{}
	api.RegisterIPAllowlistRoutes(app, stub)

	req := httptest.NewRequest("DELETE", "/api/admin/ip-allowlist/entry-id", nil)
	resp, _ := app.Test(req)
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run TestIPAllowlist -v 2>&1 | head -20
```

Expected: compilation error — `api.RegisterIPAllowlistRoutes` undefined.

- [ ] **Step 3: Create `admin/api/ip_allowlist.go`**

```go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// IPAllowlistServiceIface is the narrow interface the handlers need.
// The concrete *services.IPAllowlistService satisfies it.
type IPAllowlistServiceIface interface {
	List(ctx context.Context, tenantID string) ([]*services.IPAllowlistEntry, error)
	Add(ctx context.Context, tenantID, cidr, label string) (*services.IPAllowlistEntry, error)
	Delete(ctx context.Context, tenantID, id string) error
}

// RegisterIPAllowlistRoutes wires GET/POST/DELETE for /api/admin/ip-allowlist.
func RegisterIPAllowlistRoutes(app fiber.Router, svc IPAllowlistServiceIface) {
	app.Get("/api/admin/ip-allowlist", listIPAllowlist(svc))
	app.Post("/api/admin/ip-allowlist", addIPAllowlist(svc))
	app.Delete("/api/admin/ip-allowlist/:id", deleteIPAllowlist(svc))
}

func listIPAllowlist(svc IPAllowlistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		entries, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		if entries == nil {
			entries = []*services.IPAllowlistEntry{}
		}
		return c.JSON(fiber.Map{"entries": entries})
	}
}

func addIPAllowlist(svc IPAllowlistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			CIDR  string `json:"cidr"`
			Label string `json:"label"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if body.CIDR == "" {
			return c.Status(400).JSON(fiber.Map{"error": "cidr is required"})
		}
		entry, err := svc.Add(c.Context(), claims.TenantID, body.CIDR, body.Label)
		if err != nil {
			// Surface CIDR validation errors and unique-constraint violations as 400.
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(entry)
	}
}

func deleteIPAllowlist(svc IPAllowlistServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id := c.Params("id")
		if id == "" {
			return c.Status(400).JSON(fiber.Map{"error": "id param required"})
		}
		if err := svc.Delete(c.Context(), claims.TenantID, id); err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "deleted"})
	}
}
```

- [ ] **Step 4: Run the handler tests to confirm they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./api/ -run TestIPAllowlist -v
```

Expected output:
```
--- PASS: TestListIPAllowlist
--- PASS: TestAddIPAllowlist
--- PASS: TestAddIPAllowlist_NonAdmin
--- PASS: TestDeleteIPAllowlist
PASS
```

- [ ] **Step 5: Wire into `admin/main.go`**

`admin/main.go` currently ends the `protected` group registrations at line 51. Make two additions:

**5a.** After the `jwtMiddleware` declaration (after line 29), add:

```go
allowlistSvc := services.NewIPAllowlistService(pool)
```

**5b.** After `protected := app.Group("/", jwtMiddleware)` (line 43), add the middleware on the next line:

```go
protected.Use(services.IPAllowlistMiddleware(allowlistSvc))
```

**5c.** After `api.RegisterFuelRoutes(protected, fuelSvc)` (line 51), add:

```go
api.RegisterIPAllowlistRoutes(protected, allowlistSvc)
```

The relevant section of `admin/main.go` after your edits should look like this:

```go
jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
jwtMiddleware := api.NewJWTMiddleware(jwtSvc)
allowlistSvc := services.NewIPAllowlistService(pool)

app := fiber.New()
app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

app.Get("/health", func(c *fiber.Ctx) error {
    return c.JSON(fiber.Map{"status": "ok"})
})

app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

webhookSvc := services.NewWebhookService(pool)
api.RegisterWebhookRoutes(app, webhookSvc, cfg.EncryptionKey)

protected := app.Group("/", jwtMiddleware)
protected.Use(services.IPAllowlistMiddleware(allowlistSvc))
api.RegisterUserRoutes(protected, services.NewUserService(pool))
api.RegisterModelRoutes(protected, services.NewModelService(pool))
usageSvc := services.NewUsageService(pool)
api.RegisterUsageRoutes(protected, usageSvc)
api.RegisterUsageAdminRoutes(protected, usageSvc)
api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), cfg.EncryptionKey)
api.RegisterKPIRoutes(protected, kpiSvc)
api.RegisterFuelRoutes(protected, fuelSvc)
api.RegisterIPAllowlistRoutes(protected, allowlistSvc)
```

- [ ] **Step 6: Build to verify**

```bash
cd /Users/sugac.275/ToTra/admin && go build ./...
```

Expected: no output.

- [ ] **Step 7: Run all existing tests to confirm nothing regressed**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./...
```

Expected: all tests pass.

- [ ] **Step 8: Commit**

```bash
git add admin/api/ip_allowlist.go admin/api/ip_allowlist_test.go admin/main.go
git commit -m "feat(api): add IP allowlist endpoints and middleware wiring"
```

---

## Task 4: Frontend API client — types + 3 functions in `dashboard/src/api/client.ts`

**Files:**
- Modify: `dashboard/src/api/client.ts`

Append after the final `getInactiveUsers` function (currently line 287 of `client.ts`).

- [ ] **Step 1: Append to `dashboard/src/api/client.ts`**

Add the following block at the very end of the file:

```typescript
// ---- IP Allowlist ----

export interface IPAllowlistEntry {
  id: string;
  cidr: string;
  label: string;
  created_at: string;
}

export const listIPAllowlist = () =>
  apiClient.get<{ entries: IPAllowlistEntry[] }>("/api/admin/ip-allowlist");

export const addIPAllowlistEntry = (cidr: string, label: string) =>
  apiClient.post<IPAllowlistEntry>("/api/admin/ip-allowlist", { cidr, label });

export const deleteIPAllowlistEntry = (id: string) =>
  apiClient.delete<{ status: string }>(`/api/admin/ip-allowlist/${id}`);
```

- [ ] **Step 2: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/api/client.ts
git commit -m "feat(client): add IP allowlist API types and functions"
```

---

## Task 5: Frontend page — `dashboard/src/pages/admin/IPAllowlistPage.tsx`

**Files:**
- Create: `dashboard/src/pages/admin/IPAllowlistPage.tsx`

The page shows:
1. A table listing current CIDR entries with label and creation date, plus a Delete button per row.
2. An "Add Entry" form with a CIDR text field and an optional Label field.
3. Inline error messages for add failures (e.g. invalid CIDR, duplicate).

All mutations use `useMutation` from TanStack Query v5 and invalidate the `["ip-allowlist"]` query key on success.

- [ ] **Step 1: Create `dashboard/src/pages/admin/IPAllowlistPage.tsx`**

```tsx
import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listIPAllowlist,
  addIPAllowlistEntry,
  deleteIPAllowlistEntry,
} from "../../api/client";
import type { IPAllowlistEntry } from "../../api/client";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "../../components/ui/card";
import { Button } from "../../components/ui/button";
import { Input } from "../../components/ui/input";

export function IPAllowlistPage() {
  const queryClient = useQueryClient();
  const [cidr, setCidr] = useState("");
  const [label, setLabel] = useState("");
  const [addError, setAddError] = useState<string | null>(null);

  const { data, isLoading } = useQuery({
    queryKey: ["ip-allowlist"],
    queryFn: () => listIPAllowlist().then((r) => r.data),
  });

  const entries: IPAllowlistEntry[] = data?.entries ?? [];

  const addMutation = useMutation({
    mutationFn: () => addIPAllowlistEntry(cidr.trim(), label.trim()),
    onSuccess: () => {
      setCidr("");
      setLabel("");
      setAddError(null);
      queryClient.invalidateQueries({ queryKey: ["ip-allowlist"] });
    },
    onError: (err: unknown) => {
      const message =
        (err as { response?: { data?: { error?: string } } })?.response?.data
          ?.error ?? "Failed to add entry";
      setAddError(message);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => deleteIPAllowlistEntry(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["ip-allowlist"] });
    },
  });

  function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    if (!cidr.trim()) return;
    setAddError(null);
    addMutation.mutate();
  }

  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-bold">IP Allowlist</h1>
      <p className="text-sm text-zinc-400">
        When at least one entry exists, only requests from matching IP ranges
        are permitted. If no entries exist, all IPs are allowed (opt-in).
      </p>

      {/* Add entry form */}
      <Card>
        <CardHeader>
          <CardTitle>Add CIDR Entry</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleAdd} className="flex items-end gap-3 flex-wrap">
            <div className="flex flex-col gap-1">
              <label className="text-xs text-zinc-400 font-medium">
                CIDR <span className="text-zinc-500">(e.g. 10.0.0.0/8)</span>
              </label>
              <Input
                type="text"
                placeholder="203.0.113.0/24"
                value={cidr}
                onChange={(e) => setCidr(e.target.value)}
                className="w-52"
                required
              />
            </div>
            <div className="flex flex-col gap-1">
              <label className="text-xs text-zinc-400 font-medium">
                Label <span className="text-zinc-500">(optional)</span>
              </label>
              <Input
                type="text"
                placeholder="office-vpn"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                className="w-40"
              />
            </div>
            <Button
              type="submit"
              disabled={addMutation.isPending || !cidr.trim()}
            >
              {addMutation.isPending ? "Adding..." : "Add Entry"}
            </Button>
          </form>
          {addError && (
            <p className="mt-2 text-sm text-red-400">{addError}</p>
          )}
        </CardContent>
      </Card>

      {/* Entries table */}
      <Card>
        <CardHeader>
          <CardTitle>
            Current Allowlist
            {entries.length > 0 && (
              <span className="ml-2 text-sm font-normal text-zinc-400">
                ({entries.length} {entries.length === 1 ? "entry" : "entries"})
              </span>
            )}
          </CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          {isLoading ? (
            <p className="text-zinc-500 text-sm p-4">Loading...</p>
          ) : entries.length === 0 ? (
            <p className="text-zinc-500 text-sm p-4 text-center">
              No entries — all IPs are currently allowed.
            </p>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-zinc-800 text-zinc-400">
                  <th className="text-left py-2 px-4 font-medium">CIDR</th>
                  <th className="text-left py-2 font-medium">Label</th>
                  <th className="text-left py-2 font-medium">Added</th>
                  <th className="py-2 px-4" />
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr
                    key={entry.id}
                    className="border-b border-zinc-800/50 hover:bg-zinc-800/30"
                  >
                    <td className="py-2 px-4 font-mono text-indigo-300">
                      {entry.cidr}
                    </td>
                    <td className="py-2 text-zinc-400">
                      {entry.label || <span className="text-zinc-600">—</span>}
                    </td>
                    <td className="py-2 text-zinc-500">
                      {new Date(entry.created_at).toLocaleDateString()}
                    </td>
                    <td className="py-2 px-4 text-right">
                      <Button
                        variant="destructive"
                        size="sm"
                        disabled={deleteMutation.isPending}
                        onClick={() => deleteMutation.mutate(entry.id)}
                      >
                        Delete
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
```

- [ ] **Step 2: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors. If you see `Cannot find module '../../api/client'` exports, ensure Task 4 is complete.

- [ ] **Step 3: Commit**

```bash
git add dashboard/src/pages/admin/IPAllowlistPage.tsx
git commit -m "feat(dashboard): add IP Allowlist management page"
```

---

## Task 6: Wire route + nav — `App.tsx` and `Layout.tsx`

**Files:**
- Modify: `dashboard/src/App.tsx`
- Modify: `dashboard/src/components/Layout.tsx`

- [ ] **Step 1: Add the import to `dashboard/src/App.tsx`**

After the `DepartmentReportPage` import (line 13 currently), add:

```typescript
import { IPAllowlistPage } from "./pages/admin/IPAllowlistPage";
```

- [ ] **Step 2: Add the route inside the nested `<Route path="/">` in `dashboard/src/App.tsx`**

After the `admin/reports` route (after line 40 currently), add:

```tsx
<Route path="admin/ip-allowlist" element={<ProtectedRoute adminOnly><IPAllowlistPage /></ProtectedRoute>} />
```

The full routes block should now read:

```tsx
<Route path="admin/dashboard" element={<ProtectedRoute adminOnly><DashboardPage /></ProtectedRoute>} />
<Route path="admin/users" element={<ProtectedRoute adminOnly><UsersPage /></ProtectedRoute>} />
<Route path="admin/models" element={<ProtectedRoute adminOnly><ModelsPage /></ProtectedRoute>} />
<Route path="admin/quota" element={<QuotaPage />} />
<Route path="admin/kpi" element={<ProtectedRoute adminOnly><KpiPage /></ProtectedRoute>} />
<Route path="admin/integrations" element={<ProtectedRoute adminOnly><IntegrationsPage /></ProtectedRoute>} />
<Route path="admin/reports" element={<ProtectedRoute adminOnly><DepartmentReportPage /></ProtectedRoute>} />
<Route path="admin/ip-allowlist" element={<ProtectedRoute adminOnly><IPAllowlistPage /></ProtectedRoute>} />
<Route path="me" element={<MyUsagePage />} />
```

- [ ] **Step 3: Add sidebar nav item to `dashboard/src/components/Layout.tsx`**

In the `adminNavItems` array, add the IP Allowlist entry after `Integrations`:

```typescript
const adminNavItems = [
  { label: "Dashboard", href: "/admin/dashboard" },
  { label: "Employees", href: "/admin/users" },
  { label: "Models", href: "/admin/models" },
  { label: "Quota Requests", href: "/admin/quota" },
  { label: "KPI", href: "/admin/kpi" },
  { label: "Reports", href: "/admin/reports" },
  { label: "Integrations", href: "/admin/integrations" },
  { label: "IP Allowlist", href: "/admin/ip-allowlist" },
  { label: "My Usage", href: "/me" },
];
```

- [ ] **Step 4: TypeScript typecheck**

```bash
cd /Users/sugac.275/ToTra/dashboard && npx tsc --noEmit
```

Expected: no errors.

- [ ] **Step 5: Smoke-test the UI**

```bash
cd /Users/sugac.275/ToTra/dashboard && npm run dev
```

Open `http://localhost:3000`, sign in as an admin, click "IP Allowlist" in the sidebar. Confirm:
- The page loads with "No entries — all IPs are currently allowed."
- Entering `10.0.0.0/8` with label `office` and clicking "Add Entry" adds a row.
- Clicking "Delete" on the row removes it.
- Entering an invalid CIDR (e.g. `999.999.999.999/33`) shows the error from the server.

- [ ] **Step 6: Commit**

```bash
git add dashboard/src/App.tsx dashboard/src/components/Layout.tsx
git commit -m "feat(dashboard): wire IP Allowlist route and sidebar nav"
```

---

## Self-Review

### 1. Spec Coverage

| Requirement | Covered By |
|---|---|
| `tenant_ip_allowlists` table with `id`, `tenant_id`, `cidr`, `label`, `created_at`, unique `(tenant_id, cidr)` | Task 1 SQL |
| Zero entries → allow all IPs (opt-in) | Task 2 `IPAllowlistMiddleware` — `len(cidrs) == 0` guard |
| ≥1 entry → only matching IPs pass | Task 2 middleware CIDR loop + 403 fallthrough |
| Applies to all protected routes after JWT middleware | Task 3 Step 5 — `protected.Use(services.IPAllowlistMiddleware(allowlistSvc))` before all `Register*` calls |
| `c.IP()` for client IP | Task 2 — `net.ParseIP(c.IP())` |
| `net.ParseCIDR` + `(*net.IPNet).Contains` for matching | Task 2 middleware loop |
| CIDR validation on Add | Task 2 `Add()` — `net.ParseCIDR` guard returning 400 from handler |
| Admin UI: list CIDRs | Task 5 entries table |
| Admin UI: add entry (CIDR + label) | Task 5 add form with `addIPAllowlistEntry` mutation |
| Admin UI: delete entry | Task 5 Delete button with `deleteIPAllowlistEntry` mutation |
| Route `/admin/ip-allowlist` | Task 6 App.tsx route |
| Sidebar nav link | Task 6 `adminNavItems` entry |
| Admin-only enforcement (backend) | Task 3 — `claims.Role != "admin"` check in all three handlers |
| Admin-only enforcement (frontend) | Task 6 — `<ProtectedRoute adminOnly>` |
| Middleware registered after JWT | Task 3 Step 5 — `protected.Use(IPAllowlistMiddleware)` on line after `app.Group("/", jwtMiddleware)` |
| Middleware calls `c.Next()` (gate, not terminal) | Task 2 — all success paths call `c.Next()` |
| `LoadTenantCIDRs` per-request DB call (no extra cache) | Task 2 — no sync.Map/cache; direct pool query per request |

All spec requirements are covered.

### 2. Placeholder Scan

No TBD, TODO, "implement later", "add appropriate error handling", or "similar to Task N" patterns are present. Every step contains complete, compilable code.

### 3. Type Consistency

- `IPAllowlistEntry` Go struct fields (`ID`, `CIDR`, `Label`, `CreatedAt` with json tags `id`, `cidr`, `label`, `created_at`) → TS interface fields (`id`, `cidr`, `label`, `created_at`) — consistent.
- `IPAllowlistServiceIface` in `admin/api/ip_allowlist.go` declares `List`, `Add`, `Delete` — the concrete `*services.IPAllowlistService` implements exactly those three method signatures. `stubAllowlistService` in the test implements the same interface — consistent.
- `RegisterIPAllowlistRoutes(app fiber.Router, svc IPAllowlistServiceIface)` defined in Task 3, called in Task 3 Step 5 as `api.RegisterIPAllowlistRoutes(protected, allowlistSvc)` where `allowlistSvc` is `*services.IPAllowlistService` — satisfies `IPAllowlistServiceIface` — consistent.
- `services.IPAllowlistMiddleware(allowlistSvc)` — `allowlistSvc` is `*services.IPAllowlistService`, parameter type in middleware is `*IPAllowlistService` — consistent.
- `listIPAllowlist()` returns `{ entries: IPAllowlistEntry[] }` (Task 4), accessed as `data?.entries ?? []` in Task 5 — consistent.
- `addIPAllowlistEntry(cidr, label)` returns `IPAllowlistEntry` (Task 4), backend POST returns 201 with entry JSON (Task 3) — consistent.
- `deleteIPAllowlistEntry(id)` returns `{ status: string }` (Task 4), backend DELETE returns `{"status":"deleted"}` (Task 3) — consistent.
- `useMutation` in Task 5 uses `mutationFn: () => addIPAllowlistEntry(cidr.trim(), label.trim())` — `cidr` and `label` are both `string` from `useState("")` — matches `addIPAllowlistEntry(cidr: string, label: string)` signature — consistent.
- `queryClient.invalidateQueries({ queryKey: ["ip-allowlist"] })` in both add and delete mutations invalidates the same key used by `useQuery({ queryKey: ["ip-allowlist"] })` — consistent.
