//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/yourorg/totra/admin/api"
	"github.com/yourorg/totra/admin/services"
)

// getTestDSN returns the Postgres DSN from the environment, or skips the test.
func getTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — skipping integration test")
	}
	return dsn
}

// setupApp builds a Fiber app identical to main.go, using the provided pool.
func setupApp(t *testing.T, pool *pgxpool.Pool) *fiber.App {
	t.Helper()

	kpiSvc := services.NewKPIService(pool)
	fuelSvc := services.NewFuelService(pool)

	jwtSvc := services.NewJWTService("test-integration-secret", 24*time.Hour)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)

	app := fiber.New(fiber.Config{
		// Suppress error logs during tests
		DisableStartupMessage: true,
	})
	app.Use(cors.New(cors.Config{AllowOrigins: "*"}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"status": "ok"})
	})

	app.Post("/api/auth/login", api.LoginHandler(pool, jwtSvc))

	webhookSvc := services.NewWebhookService(pool)
	api.RegisterWebhookRoutes(app, webhookSvc, "test-encryption-key-32-bytes-xx!")

	protected := app.Group("/", jwtMiddleware)
	api.RegisterUserRoutes(protected, services.NewUserService(pool))
	api.RegisterModelRoutes(protected, services.NewModelService(pool))
	api.RegisterUsageRoutes(protected, services.NewUsageService(pool))
	api.RegisterQuotaRoutes(protected, services.NewQuotaService(pool))
	api.RegisterIntegrationRoutes(protected, services.NewIntegrationService(pool), "test-encryption-key-32-bytes-xx!")
	api.RegisterKPIRoutes(protected, kpiSvc)
	api.RegisterFuelRoutes(protected, fuelSvc)

	return app
}

// adminJWT logs in as admin@acme.com and returns the JWT token string.
func adminJWT(t *testing.T, app *fiber.App) string {
	t.Helper()

	body := `{"email":"admin@acme.com","password":"totra-emp-dev-admin-key"}`
	req := httpNewRequest(t, http.MethodPost, "/api/auth/login",
		"application/json", []byte(body))

	resp, err := app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode, "login must succeed")

	var result struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NotEmpty(t, result.Token, "token must be non-empty")

	return result.Token
}

// httpNewRequest creates an *http.Request suitable for fiber.App.Test.
func httpNewRequest(t *testing.T, method, target, contentType string, body []byte) *http.Request {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, target, bodyReader)
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

// authRequest creates a request with an Authorization: Bearer <token> header.
func authRequest(t *testing.T, method, target, token, contentType string, body []byte) *http.Request {
	t.Helper()
	req := httpNewRequest(t, method, target, contentType, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// connectPool dials Postgres and registers cleanup.
func connectPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), dsn)
	require.NoError(t, err, "create pgx pool")
	require.NoError(t, pool.Ping(context.Background()), "ping postgres")
	t.Cleanup(func() { pool.Close() })
	return pool
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestIntegration_Health(t *testing.T) {
	dsn := getTestDSN(t)
	pool := connectPool(t, dsn)
	app := setupApp(t, pool)

	req := httpNewRequest(t, http.MethodGet, "/health", "", nil)
	resp, err := app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.Equal(t, "ok", result["status"])
}

func TestIntegration_Login(t *testing.T) {
	dsn := getTestDSN(t)
	pool := connectPool(t, dsn)
	app := setupApp(t, pool)

	body := `{"email":"admin@acme.com","password":"totra-emp-dev-admin-key"}`
	req := httpNewRequest(t, http.MethodPost, "/api/auth/login",
		"application/json", []byte(body))

	resp, err := app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Token, "response must contain a non-empty token")
}

func TestIntegration_KPISnapshot(t *testing.T) {
	dsn := getTestDSN(t)
	pool := connectPool(t, dsn)
	app := setupApp(t, pool)

	token := adminJWT(t, app)
	month := time.Now().UTC().Format("2006-01")

	req := authRequest(t, http.MethodPost,
		fmt.Sprintf("/api/admin/kpi/run?month=%s", month),
		token, "", nil)

	resp, err := app.Test(req, 30000)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify at least 1 row written to efficiency_snapshots
	var count int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM efficiency_snapshots WHERE year_month = $1`, month,
	).Scan(&count)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 1,
		"RunMonthlySnapshot must write at least 1 row to efficiency_snapshots")
}

func TestIntegration_GetSnapshots(t *testing.T) {
	dsn := getTestDSN(t)
	pool := connectPool(t, dsn)
	app := setupApp(t, pool)

	token := adminJWT(t, app)
	month := time.Now().UTC().Format("2006-01")

	// Delete existing snapshots for a clean slate
	_, err := pool.Exec(context.Background(),
		`DELETE FROM efficiency_snapshots WHERE year_month = $1`, month)
	require.NoError(t, err)

	// Trigger a fresh snapshot
	runReq := authRequest(t, http.MethodPost,
		fmt.Sprintf("/api/admin/kpi/run?month=%s", month),
		token, "", nil)
	runResp, err := app.Test(runReq, 30000)
	require.NoError(t, err)
	runResp.Body.Close()
	require.Equal(t, http.StatusOK, runResp.StatusCode)

	// GET /api/kpi/snapshots
	getReq := authRequest(t, http.MethodGet,
		fmt.Sprintf("/api/kpi/snapshots?month=%s", month),
		token, "", nil)
	getResp, err := app.Test(getReq, 10000)
	require.NoError(t, err)
	defer getResp.Body.Close()

	require.Equal(t, http.StatusOK, getResp.StatusCode)

	var result struct {
		Month     string                   `json:"month"`
		Snapshots []map[string]interface{} `json:"snapshots"`
	}
	require.NoError(t, json.NewDecoder(getResp.Body).Decode(&result))
	require.NotEmpty(t, result.Snapshots, "snapshots array must not be empty")

	// Find Alice Engineer and verify she has a snapshot (she has usage records in May 2026)
	var aliceFound bool
	for _, snap := range result.Snapshots {
		name, _ := snap["user_name"].(string)
		// Match on "Alice" prefix to handle full names like "Alice Engineer"
		if len(name) >= 5 && name[:5] == "Alice" {
			aliceFound = true
			// Alice has usage records in May 2026, so her snapshot should exist.
			// AIQ requires >= 8 active days; with seed data she may score 0 but
			// should still appear with a recorded snapshot entry.
			t.Logf("Alice snapshot: aiq_score=%v efficiency_score=%v",
				snap["aiq_score"], snap["efficiency_score"])
			break
		}
	}
	assert.True(t, aliceFound, "Alice must appear in the snapshots")
}

func TestIntegration_SubMetrics(t *testing.T) {
	dsn := getTestDSN(t)
	pool := connectPool(t, dsn)
	app := setupApp(t, pool)

	token := adminJWT(t, app)
	month := time.Now().UTC().Format("2006-01")

	req := authRequest(t, http.MethodGet,
		fmt.Sprintf("/api/me/kpi/submetrics?month=%s", month),
		token, "", nil)

	resp, err := app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var result struct {
		Month   string      `json:"month"`
		Metrics interface{} `json:"metrics"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))

	// Admin has no usage records → metrics should be nil
	assert.Nil(t, result.Metrics,
		"admin has no usage records so sub-metrics must be nil")
}
