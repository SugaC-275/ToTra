//go:build integration

package api_test

import (
	"bytes"
	"encoding/json"
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

func getTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — skipping integration test")
	}
	return dsn
}

func connectPool(t *testing.T, dsn string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

func setupApp(t *testing.T, pool *pgxpool.Pool) *fiber.App {
	t.Helper()

	jwtSvc := services.NewJWTService("test-integration-secret", 24*time.Hour)
	jwtMiddleware := api.NewJWTMiddleware(jwtSvc)

	app := fiber.New(fiber.Config{DisableStartupMessage: true})
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

	return app
}

func httpNewRequest(t *testing.T, method, path, contentType string, body []byte) *http.Request {
	t.Helper()
	var b io.Reader
	if body != nil {
		b = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, path, b)
	require.NoError(t, err)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func adminJWT(t *testing.T, app *fiber.App) string {
	t.Helper()
	body := `{"email":"admin@acme.com","password":"totra-emp-dev-admin-key"}`
	req := httpNewRequest(t, http.MethodPost, "/api/auth/login", "application/json", []byte(body))
	resp, err := app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result struct{ Token string `json:"token"` }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result.Token
}

func authRequest(t *testing.T, method, path, token, contentType string, body []byte) *http.Request {
	t.Helper()
	req := httpNewRequest(t, method, path, contentType, body)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestIntegration_Health(t *testing.T) {
	pool := connectPool(t, getTestDSN(t))
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
	pool := connectPool(t, getTestDSN(t))
	app := setupApp(t, pool)

	body := `{"email":"admin@acme.com","password":"totra-emp-dev-admin-key"}`
	req := httpNewRequest(t, http.MethodPost, "/api/auth/login", "application/json", []byte(body))
	resp, err := app.Test(req, 10000)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var result struct{ Token string `json:"token"` }
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	assert.NotEmpty(t, result.Token)
}
