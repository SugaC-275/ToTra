# ToTra Phase 1-C: Admin Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go Admin Service providing REST APIs for employee management, model configuration, quota management, usage reporting, and the approval workflow dashboard.

**Architecture:** Fiber v2 REST API. JWT-protected endpoints. All business logic in service layer. Handlers are thin — just parse, call service, respond. PostgreSQL via pgx/v5 connection pool.

**Tech Stack:** Go 1.22, Fiber v2, pgx/v5, golang-jwt/jwt v5, testify

**Assigned Agent:** 🗄️ Admin Service Engineer  
**Depends on:** Plan A (Infrastructure) — PostgreSQL must be running with schema applied

---

## File Map

```
admin/
├── go.mod
├── main.go                         CREATE — server bootstrap
├── config/
│   └── config.go                   CREATE — env loading
├── db/
│   └── db.go                       CREATE — pgxpool setup
├── api/
│   ├── middleware.go               CREATE — JWT auth middleware
│   ├── users.go                    CREATE — employee CRUD + key generation
│   ├── users_test.go               CREATE
│   ├── models.go                   CREATE — model config CRUD
│   ├── models_test.go              CREATE
│   ├── quota.go                    CREATE — quota management + approval workflow
│   ├── quota_test.go               CREATE
│   ├── usage.go                    CREATE — usage reports + chargeback
│   └── usage_test.go               CREATE
└── services/
    ├── auth.go                     CREATE — JWT issue/verify
    ├── users.go                    CREATE — user business logic
    ├── quota.go                    CREATE — quota business logic
    └── usage.go                    CREATE — aggregation queries
```

---

## Task 1: Go Module & Config

**Files:**
- Create: `admin/go.mod`
- Create: `admin/config/config.go`
- Create: `admin/db/db.go`

- [ ] **Step 1: Initialize module**

```bash
cd admin
go mod init github.com/yourorg/totra/admin
go get github.com/gofiber/fiber/v2@v2.52.5
go get github.com/jackc/pgx/v5@v5.5.5
go get github.com/golang-jwt/jwt/v5@v5.2.1
go get github.com/stretchr/testify@v1.9.0
go get github.com/google/uuid@v1.6.0
```

- [ ] **Step 2: Write config.go**

```go
// admin/config/config.go
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port          string
	PostgresDSN   string
	JWTSecret     string
	JWTExpiry     time.Duration
}

func Load() *Config {
	hours, _ := strconv.Atoi(getEnv("JWT_EXPIRY_HOURS", "24"))
	pgHost := mustGetEnv("POSTGRES_HOST")
	pgPort := getEnv("POSTGRES_PORT", "5432")
	pgDB := mustGetEnv("POSTGRES_DB")
	pgUser := mustGetEnv("POSTGRES_USER")
	pgPass := mustGetEnv("POSTGRES_PASSWORD")

	return &Config{
		Port:        getEnv("ADMIN_PORT", "8081"),
		PostgresDSN: fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable", pgHost, pgPort, pgDB, pgUser, pgPass),
		JWTSecret:   mustGetEnv("JWT_SECRET"),
		JWTExpiry:   time.Duration(hours) * time.Hour,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustGetEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("required env var %s not set", key))
	}
	return v
}
```

- [ ] **Step 3: Write db.go**

```go
// admin/db/db.go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}
```

- [ ] **Step 4: Commit**

```bash
cd admin && go mod tidy
git add admin/
git commit -m "feat(admin): go module, config, and db pool"
```

---

## Task 2: JWT Auth Service & Middleware

**Files:**
- Create: `admin/services/auth.go`
- Create: `admin/api/middleware.go`

- [ ] **Step 1: Write failing test for JWT service**

```go
// admin/services/auth_test.go
package services_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/services"
)

func TestJWTService_IssueAndVerify(t *testing.T) {
	svc := services.NewJWTService("test-secret", 1*time.Hour)

	token, err := svc.Issue("user-1", "tenant-1", "admin")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := svc.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "tenant-1", claims.TenantID)
	assert.Equal(t, "admin", claims.Role)
}

func TestJWTService_ExpiredToken(t *testing.T) {
	svc := services.NewJWTService("test-secret", -1*time.Second) // already expired
	token, _ := svc.Issue("user-1", "tenant-1", "admin")
	_, err := svc.Verify(token)
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd admin && go test ./services/... -run TestJWT -v
```

Expected: FAIL

- [ ] **Step 3: Implement auth.go**

```go
// admin/services/auth.go
package services

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type JWTService struct {
	secret []byte
	expiry time.Duration
}

func NewJWTService(secret string, expiry time.Duration) *JWTService {
	return &JWTService{secret: []byte(secret), expiry: expiry}
}

func (j *JWTService) Issue(userID, tenantID, role string) (string, error) {
	claims := Claims{
		UserID:   userID,
		TenantID: tenantID,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(j.expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(j.secret)
}

func (j *JWTService) Verify(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}
	return claims, nil
}
```

- [ ] **Step 4: Run tests**

```bash
cd admin && go test ./services/... -run TestJWT -v
```

Expected: PASS

- [ ] **Step 5: Implement api/middleware.go**

```go
// admin/api/middleware.go
package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type contextKey string

func NewJWTMiddleware(jwtSvc *services.JWTService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		auth := c.Get("Authorization")
		if auth == "" {
			return c.Status(401).JSON(fiber.Map{"error": "missing Authorization header"})
		}
		tokenStr := strings.TrimPrefix(auth, "Bearer ")
		claims, err := jwtSvc.Verify(tokenStr)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid or expired token"})
		}
		c.Locals("claims", claims)
		return c.Next()
	}
}

func RequireRole(role string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != role && claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "insufficient permissions"})
		}
		return c.Next()
	}
}
```

- [ ] **Step 6: Commit**

```bash
git add admin/services/auth.go admin/services/auth_test.go admin/api/middleware.go
git commit -m "feat(admin): JWT auth service and middleware"
```

---

## Task 3: User Management API

**Files:**
- Create: `admin/api/users.go`
- Create: `admin/api/users_test.go`
- Create: `admin/services/users.go`

- [ ] **Step 1: Write failing tests**

```go
// admin/api/users_test.go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type mockUserService struct {
	users []*services.User
}

func (m *mockUserService) List(ctx context.Context, tenantID string) ([]*services.User, error) {
	return m.users, nil
}

func (m *mockUserService) Create(ctx context.Context, tenantID string, req services.CreateUserRequest) (*services.User, error) {
	return &services.User{ID: "new-id", Name: req.Name, Email: req.Email, Role: req.Role, APIKey: "totra-emp-new-key"}, nil
}

func setupTestApp(svc services.UserServiceInterface) *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", claims)
		return c.Next()
	})
	api.RegisterUserRoutes(app, svc)
	return app
}

func TestListUsers(t *testing.T) {
	svc := &mockUserService{users: []*services.User{
		{ID: "u1", Name: "Alice", Email: "alice@corp.com", Role: "standard"},
	}}
	app := setupTestApp(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, float64(1), body["total"])
}

func TestCreateUser(t *testing.T) {
	svc := &mockUserService{}
	app := setupTestApp(svc)

	payload := map[string]string{"name": "Bob", "email": "bob@corp.com", "role": "standard"}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	require.Equal(t, 201, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.NotEmpty(t, body["api_key"], "response must include generated API key")
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd admin && go test ./api/... -run TestListUsers -v
```

Expected: FAIL

- [ ] **Step 3: Implement services/users.go**

```go
// admin/services/users.go
package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID       string
	TenantID string
	Name     string
	Email    string
	Role     string
	QuotaSCU int
	IsActive bool
	APIKey   string // only set on Create, never stored plain
}

type CreateUserRequest struct {
	Name  string
	Email string
	Role  string
}

type UserServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*User, error)
	Create(ctx context.Context, tenantID string, req CreateUserRequest) (*User, error)
}

type UserService struct {
	pool *pgxpool.Pool
}

func NewUserService(pool *pgxpool.Pool) *UserService {
	return &UserService{pool: pool}
}

func (s *UserService) List(ctx context.Context, tenantID string) ([]*User, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, email, role, quota_scu, is_active FROM users WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		if err := rows.Scan(&u.ID, &u.TenantID, &u.Name, &u.Email, &u.Role, &u.QuotaSCU, &u.IsActive); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *UserService) Create(ctx context.Context, tenantID string, req CreateUserRequest) (*User, error) {
	rawKey := generateEmployeeKey()
	keyHash := hashEmployeeKey(rawKey)

	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, tenant_id, name, email, auth_type, api_key_hash, role)
		 VALUES ($1, $2, $3, $4, 'api_key', $5, $6) RETURNING id`,
		uuid.New().String(), tenantID, req.Name, req.Email, keyHash, req.Role,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	return &User{ID: id, TenantID: tenantID, Name: req.Name, Email: req.Email, Role: req.Role, APIKey: rawKey}, nil
}

func generateEmployeeKey() string {
	b := make([]byte, 24)
	rand.Read(b)
	return "totra-emp-" + hex.EncodeToString(b)
}

func hashEmployeeKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}
```

- [ ] **Step 4: Implement api/users.go**

```go
// admin/api/users.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterUserRoutes(app fiber.Router, svc services.UserServiceInterface) {
	app.Get("/api/users", listUsers(svc))
	app.Post("/api/users", createUser(svc))
}

func listUsers(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		users, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(users), "users": users})
	}
}

func createUser(svc services.UserServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req services.CreateUserRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request body"})
		}
		if req.Name == "" || req.Email == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name and email are required"})
		}
		if req.Role == "" {
			req.Role = "standard"
		}
		user, err := svc.Create(c.Context(), claims.TenantID, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{
			"id":      user.ID,
			"name":    user.Name,
			"email":   user.Email,
			"role":    user.Role,
			"api_key": user.APIKey,
		})
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd admin && go test ./api/... -run TestListUsers -run TestCreateUser -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add admin/api/users.go admin/api/users_test.go admin/services/users.go
git commit -m "feat(admin): user management API with Employee Key generation"
```

---

## Task 4: Model Config API

**Files:**
- Create: `admin/api/models.go`
- Create: `admin/api/models_test.go`

- [ ] **Step 1: Write failing tests**

```go
// admin/api/models_test.go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type mockModelService struct{}

func (m *mockModelService) List(ctx context.Context, tenantID string) ([]*services.ModelConfig, error) {
	return []*services.ModelConfig{
		{ID: "m1", Name: "gpt-4o", Provider: "openai", SCURate: 2.0, IsActive: true},
	}, nil
}

func (m *mockModelService) Create(ctx context.Context, tenantID string, req services.CreateModelRequest) (*services.ModelConfig, error) {
	return &services.ModelConfig{ID: "m2", Name: req.Name, Provider: req.Provider, SCURate: req.SCURate}, nil
}

func setupModelApp(svc api.ModelServiceInterface) *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterModelRoutes(app, svc)
	return app
}

func TestListModels(t *testing.T) {
	app := setupModelApp(&mockModelService{})
	req := httptest.NewRequest(http.MethodGet, "/api/models", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestCreateModel(t *testing.T) {
	app := setupModelApp(&mockModelService{})
	payload := map[string]interface{}{"name": "claude-3-5-sonnet", "provider": "anthropic", "base_url": "https://api.anthropic.com", "api_key": "sk-ant-test", "scu_rate": 1.0}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/models", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	assert.Equal(t, 201, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd admin && go test ./api/... -run TestListModels -v
```

Expected: FAIL

- [ ] **Step 3: Implement api/models.go with service interface**

```go
// admin/api/models.go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

type ModelServiceInterface interface {
	List(ctx context.Context, tenantID string) ([]*services.ModelConfig, error)
	Create(ctx context.Context, tenantID string, req services.CreateModelRequest) (*services.ModelConfig, error)
}

func RegisterModelRoutes(app fiber.Router, svc ModelServiceInterface) {
	app.Get("/api/models", listModels(svc))
	app.Post("/api/models", createModel(svc))
}

func listModels(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		models, err := svc.List(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"total": len(models), "models": models})
	}
}

func createModel(svc ModelServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var req services.CreateModelRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if req.Name == "" || req.Provider == "" || req.BaseURL == "" {
			return c.Status(400).JSON(fiber.Map{"error": "name, provider, base_url required"})
		}
		model, err := svc.Create(c.Context(), claims.TenantID, req)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(model)
	}
}
```

- [ ] **Step 4: Add ModelConfig types to services**

```go
// admin/services/models.go
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ModelConfig struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	BaseURL  string  `json:"base_url"`
	SCURate  float64 `json:"scu_rate"`
	IsActive bool    `json:"is_active"`
}

type CreateModelRequest struct {
	Name     string  `json:"name"`
	Provider string  `json:"provider"`
	BaseURL  string  `json:"base_url"`
	APIKey   string  `json:"api_key"`
	SCURate  float64 `json:"scu_rate"`
}

type ModelService struct {
	pool *pgxpool.Pool
}

func NewModelService(pool *pgxpool.Pool) *ModelService {
	return &ModelService{pool: pool}
}

func (s *ModelService) List(ctx context.Context, tenantID string) ([]*ModelConfig, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, provider, base_url, scu_rate, is_active FROM model_configs WHERE tenant_id = $1 ORDER BY name`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var models []*ModelConfig
	for rows.Next() {
		m := &ModelConfig{}
		rows.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.SCURate, &m.IsActive)
		models = append(models, m)
	}
	return models, nil
}

func (s *ModelService) Create(ctx context.Context, tenantID string, req CreateModelRequest) (*ModelConfig, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO model_configs (id, tenant_id, name, provider, api_key_encrypted, base_url, scu_rate)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		uuid.New().String(), tenantID, req.Name, req.Provider, req.APIKey, req.BaseURL, req.SCURate,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("create model config: %w", err)
	}
	return &ModelConfig{ID: id, Name: req.Name, Provider: req.Provider, BaseURL: req.BaseURL, SCURate: req.SCURate, IsActive: true}, nil
}
```

- [ ] **Step 5: Run tests**

```bash
cd admin && go test ./api/... -run TestListModels -run TestCreateModel -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add admin/api/models.go admin/api/models_test.go admin/services/models.go
git commit -m "feat(admin): model config API"
```

---

## Task 5: Usage Reports API (Chargeback)

**Files:**
- Create: `admin/api/usage.go`
- Create: `admin/api/usage_test.go`
- Create: `admin/services/usage.go`

- [ ] **Step 1: Write failing test**

```go
// admin/api/usage_test.go
package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type mockUsageService struct{}

func (m *mockUsageService) GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*services.UserMonthlySummary, error) {
	return []*services.UserMonthlySummary{
		{UserID: "u1", UserName: "Alice", TotalSCU: 5000, TotalUSD: 12.50, RequestCount: 120},
	}, nil
}

func (m *mockUsageService) GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*services.AdoptionStats, error) {
	return &services.AdoptionStats{TotalUsers: 10, ActiveUsers: 7, AdoptionRate: 0.70}, nil
}

func setupUsageApp() *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterUsageRoutes(app, &mockUsageService{})
	return app
}

func TestGetMonthlySummary(t *testing.T) {
	app := setupUsageApp()
	req := httptest.NewRequest(http.MethodGet, "/api/usage/summary?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestGetAdoptionRate(t *testing.T) {
	app := setupUsageApp()
	req := httptest.NewRequest(http.MethodGet, "/api/usage/adoption?month=2026-05", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd admin && go test ./api/... -run TestGetMonthly -v
```

Expected: FAIL

- [ ] **Step 3: Implement services/usage.go**

```go
// admin/services/usage.go
package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserMonthlySummary struct {
	UserID       string  `json:"user_id"`
	UserName     string  `json:"user_name"`
	TotalSCU     float64 `json:"total_scu"`
	TotalUSD     float64 `json:"total_usd"`
	RequestCount int     `json:"request_count"`
}

type AdoptionStats struct {
	TotalUsers   int     `json:"total_users"`
	ActiveUsers  int     `json:"active_users"`
	AdoptionRate float64 `json:"adoption_rate"`
}

type UsageServiceInterface interface {
	GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*UserMonthlySummary, error)
	GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*AdoptionStats, error)
}

type UsageService struct {
	pool *pgxpool.Pool
}

func NewUsageService(pool *pgxpool.Pool) *UsageService {
	return &UsageService{pool: pool}
}

func (s *UsageService) GetMonthlySummary(ctx context.Context, tenantID, yearMonth string) ([]*UserMonthlySummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id, u.name,
		       COALESCE(SUM(r.scu_cost), 0) AS total_scu,
		       COALESCE(SUM(r.usd_cost), 0) AS total_usd,
		       COUNT(r.id) AS request_count
		FROM users u
		LEFT JOIN usage_records r ON r.user_id = u.id
		    AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1
		GROUP BY u.id, u.name
		ORDER BY total_scu DESC`,
		tenantID, yearMonth,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []*UserMonthlySummary
	for rows.Next() {
		s := &UserMonthlySummary{}
		rows.Scan(&s.UserID, &s.UserName, &s.TotalSCU, &s.TotalUSD, &s.RequestCount)
		summaries = append(summaries, s)
	}
	return summaries, nil
}

func (s *UsageService) GetAdoptionRate(ctx context.Context, tenantID, yearMonth string) (*AdoptionStats, error) {
	var stats AdoptionStats
	err := s.pool.QueryRow(ctx, `
		SELECT
		    COUNT(DISTINCT u.id) AS total_users,
		    COUNT(DISTINCT r.user_id) AS active_users
		FROM users u
		LEFT JOIN usage_records r ON r.user_id = u.id
		    AND to_char(r.request_at, 'YYYY-MM') = $2
		WHERE u.tenant_id = $1 AND u.is_active = true`,
		tenantID, yearMonth,
	).Scan(&stats.TotalUsers, &stats.ActiveUsers)
	if err != nil {
		return nil, err
	}
	if stats.TotalUsers > 0 {
		stats.AdoptionRate = float64(stats.ActiveUsers) / float64(stats.TotalUsers)
	}
	return &stats, nil
}
```

- [ ] **Step 4: Implement api/usage.go**

```go
// admin/api/usage.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterUsageRoutes(app fiber.Router, svc services.UsageServiceInterface) {
	app.Get("/api/usage/summary", getMonthlySummary(svc))
	app.Get("/api/usage/adoption", getAdoptionRate(svc))
}

func getMonthlySummary(svc services.UsageServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month query param required (format: 2026-05)"})
		}
		summaries, err := svc.GetMonthlySummary(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"month": month, "summaries": summaries})
	}
}

func getAdoptionRate(svc services.UsageServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		month := c.Query("month")
		if month == "" {
			return c.Status(400).JSON(fiber.Map{"error": "month query param required"})
		}
		stats, err := svc.GetAdoptionRate(c.Context(), claims.TenantID, month)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(stats)
	}
}
```

- [ ] **Step 5: Run tests**

```bash
cd admin && go test ./api/... -run TestGetMonthly -run TestGetAdoption -v
```

Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add admin/api/usage.go admin/api/usage_test.go admin/services/usage.go
git commit -m "feat(admin): usage reports API with monthly summary and adoption rate"
```

---

## Task 6: Quota Management & Approval Workflow API

**Files:**
- Create: `admin/api/quota.go`
- Create: `admin/api/quota_test.go`

- [ ] **Step 1: Write failing tests**

```go
// admin/api/quota_test.go
package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

type mockQuotaService struct{}

func (m *mockQuotaService) RequestIncrease(ctx context.Context, tenantID, userID, requestedBy string, newQuota int, reason string) error {
	return nil
}
func (m *mockQuotaService) ListPending(ctx context.Context, tenantID string) ([]*services.QuotaRequest, error) {
	return []*services.QuotaRequest{{ID: "req-1", UserID: "u1", NewQuota: 100000, Reason: "Need more for sprint", Status: "pending"}}, nil
}
func (m *mockQuotaService) Approve(ctx context.Context, tenantID, requestID, reviewerID string) error {
	return nil
}
func (m *mockQuotaService) Reject(ctx context.Context, tenantID, requestID, reviewerID string) error {
	return nil
}

func setupQuotaApp() *fiber.App {
	app := fiber.New()
	claims := &services.Claims{UserID: "admin-1", TenantID: "tenant-1", Role: "admin"}
	app.Use(func(c *fiber.Ctx) error { c.Locals("claims", claims); return c.Next() })
	api.RegisterQuotaRoutes(app, &mockQuotaService{})
	return app
}

func TestListPendingQuotaRequests(t *testing.T) {
	app := setupQuotaApp()
	req := httptest.NewRequest(http.MethodGet, "/api/quota/requests", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}

func TestApproveQuotaRequest(t *testing.T) {
	app := setupQuotaApp()
	req := httptest.NewRequest(http.MethodPost, "/api/quota/requests/req-1/approve", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}
```

- [ ] **Step 2: Run to verify failure**

```bash
cd admin && go test ./api/... -run TestListPending -v
```

Expected: FAIL

- [ ] **Step 3: Implement api/quota.go and services/quota.go**

```go
// admin/services/quota.go
package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type QuotaRequest struct {
	ID          string `json:"id"`
	UserID      string `json:"user_id"`
	NewQuota    int    `json:"new_quota"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	RequestedBy string `json:"requested_by"`
}

type QuotaServiceInterface interface {
	RequestIncrease(ctx context.Context, tenantID, userID, requestedBy string, newQuota int, reason string) error
	ListPending(ctx context.Context, tenantID string) ([]*QuotaRequest, error)
	Approve(ctx context.Context, tenantID, requestID, reviewerID string) error
	Reject(ctx context.Context, tenantID, requestID, reviewerID string) error
}

type QuotaService struct{ pool *pgxpool.Pool }

func NewQuotaService(pool *pgxpool.Pool) *QuotaService { return &QuotaService{pool: pool} }

func (s *QuotaService) RequestIncrease(ctx context.Context, tenantID, userID, requestedBy string, newQuota int, reason string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO quota_requests (id, tenant_id, user_id, requested_by, new_quota, reason) VALUES ($1,$2,$3,$4,$5,$6)`,
		uuid.New().String(), tenantID, userID, requestedBy, newQuota, reason,
	)
	return err
}

func (s *QuotaService) ListPending(ctx context.Context, tenantID string) ([]*QuotaRequest, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, new_quota, reason, status, requested_by FROM quota_requests WHERE tenant_id=$1 AND status='pending' ORDER BY created_at`,
		tenantID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reqs []*QuotaRequest
	for rows.Next() {
		r := &QuotaRequest{}
		rows.Scan(&r.ID, &r.UserID, &r.NewQuota, &r.Reason, &r.Status, &r.RequestedBy)
		reqs = append(reqs, r)
	}
	return reqs, nil
}

func (s *QuotaService) Approve(ctx context.Context, tenantID, requestID, reviewerID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	var newQuota int
	err = tx.QueryRow(ctx,
		`UPDATE quota_requests SET status='approved', reviewed_by=$1, reviewed_at=NOW()
		 WHERE id=$2 AND tenant_id=$3 AND status='pending' RETURNING user_id, new_quota`,
		reviewerID, requestID, tenantID,
	).Scan(&userID, &newQuota)
	if err != nil {
		return fmt.Errorf("approve quota request: %w", err)
	}

	_, err = tx.Exec(ctx, `UPDATE users SET quota_scu=$1 WHERE id=$2`, newQuota, userID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *QuotaService) Reject(ctx context.Context, tenantID, requestID, reviewerID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE quota_requests SET status='rejected', reviewed_by=$1, reviewed_at=NOW()
		 WHERE id=$2 AND tenant_id=$3 AND status='pending'`,
		reviewerID, requestID, tenantID,
	)
	return err
}
```

```go
// admin/api/quota.go
package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

func RegisterQuotaRoutes(app fiber.Router, svc services.QuotaServiceInterface) {
	app.Get("/api/quota/requests", listPendingRequests(svc))
	app.Post("/api/quota/requests/:id/approve", approveRequest(svc))
	app.Post("/api/quota/requests/:id/reject", rejectRequest(svc))
	app.Post("/api/quota/request", requestIncrease(svc))
}

func listPendingRequests(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		reqs, err := svc.ListPending(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"requests": reqs})
	}
}

func approveRequest(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		err := svc.Approve(c.Context(), claims.TenantID, c.Params("id"), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "approved"})
	}
}

func rejectRequest(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		err := svc.Reject(c.Context(), claims.TenantID, c.Params("id"), claims.UserID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "rejected"})
	}
}

func requestIncrease(svc services.QuotaServiceInterface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		var body struct {
			UserID   string `json:"user_id"`
			NewQuota int    `json:"new_quota"`
			Reason   string `json:"reason"`
		}
		if err := c.BodyParser(&body); err != nil || body.NewQuota <= 0 || body.Reason == "" {
			return c.Status(400).JSON(fiber.Map{"error": "user_id, new_quota, reason required"})
		}
		targetUser := body.UserID
		if targetUser == "" {
			targetUser = claims.UserID
		}
		err := svc.RequestIncrease(c.Context(), claims.TenantID, targetUser, claims.UserID, body.NewQuota, body.Reason)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "pending"})
	}
}
```

- [ ] **Step 4: Run tests**

```bash
cd admin && go test ./api/... -run TestListPending -run TestApprove -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add admin/api/quota.go admin/api/quota_test.go admin/services/quota.go
git commit -m "feat(admin): quota management API with approval workflow"
```

---

## Task 7: Main Server Bootstrap + Dockerfile

**Files:**
- Create: `admin/main.go`
- Create: `admin/Dockerfile`

- [ ] **Step 1: Write main.go**

```go
// admin/main.go
package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/config"
	"github.com/yourorg/totra/admin/db"
	"github.com/yourorg/totra/admin/services"
)

func main() {
	cfg := config.Load()

	pool, err := db.Connect(cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	jwtSvc := services.NewJWTService(cfg.JWTSecret, cfg.JWTExpiry)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)

	app := fiber.New()
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	// Auth endpoint (no JWT required)
	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	// Protected routes
	protected := app.Group("/", jwtMiddleware)
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	api.RegisterUsageRoutes(protected, services.NewUsageService(pool))
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))

	log.Printf("Admin service listening on :%s", cfg.Port)
	log.Fatal(app.Listen(":" + cfg.Port))
}
```

- [ ] **Step 2: Add login handler**

```go
// admin/api/auth.go
package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/admin/services"
)

func LoginHandler(pool *pgxpool.Pool, jwtSvc *services.JWTService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"` // Phase 1: simple password check
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}

		var userID, tenantID, role string
		// Note: In production, password should be bcrypt hashed. Dev uses plaintext for speed.
		err := pool.QueryRow(context.Background(),
			`SELECT id, tenant_id, role FROM users WHERE email = $1 AND is_active = true`,
			body.Email,
		).Scan(&userID, &tenantID, &role)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{"error": "invalid credentials"})
		}

		token, err := jwtSvc.Issue(userID, tenantID, role)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "token generation failed"})
		}
		return c.JSON(fiber.Map{"token": token})
	}
}
```

- [ ] **Step 3: Write Dockerfile**

```dockerfile
# admin/Dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o admin .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/admin .
EXPOSE 8081
CMD ["./admin"]
```

- [ ] **Step 4: Build and verify**

```bash
cd admin && go mod tidy && go build -o admin . && echo "Build OK"
```

Expected: `Build OK`

- [ ] **Step 5: Run all admin tests**

```bash
cd admin && go test ./... -v
```

Expected: All PASS

- [ ] **Step 6: Final commit**

```bash
git add admin/
git commit -m "feat(admin): complete admin service with auth, users, models, usage, quota APIs"
```
