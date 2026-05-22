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

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

type stubOIDCSvc struct {
	cfg         *services.OIDCConfig
	getCfgErr   error
	setCfgErr   error
	testConnErr error
	authURL     string
	authURLErr  error
	stateMap    map[string]string // state -> tenantID
	stateErr    error
	accessToken string
	exchErr     error
	email, name string
	userInfoErr error
}

func (s *stubOIDCSvc) GetConfig(_ context.Context, _ string) (*services.OIDCConfig, error) {
	return s.cfg, s.getCfgErr
}
func (s *stubOIDCSvc) SetConfig(_ context.Context, _ *services.OIDCConfig) error {
	return s.setCfgErr
}
func (s *stubOIDCSvc) TestConnection(_ context.Context, _ string) error { return s.testConnErr }
func (s *stubOIDCSvc) GenerateAuthURL(_ context.Context, _ *services.OIDCConfig, _ string) (string, error) {
	return s.authURL, s.authURLErr
}
func (s *stubOIDCSvc) ValidateState(state string) (string, error) {
	if s.stateErr != nil {
		return "", s.stateErr
	}
	tid, ok := s.stateMap[state]
	if !ok {
		return "", assert.AnError
	}
	return tid, nil
}
func (s *stubOIDCSvc) ExchangeCode(_ context.Context, _ *services.OIDCConfig, _ string) (string, error) {
	return s.accessToken, s.exchErr
}
func (s *stubOIDCSvc) GetUserInfo(_ context.Context, _ *services.OIDCConfig, _ string) (string, string, error) {
	return s.email, s.name, s.userInfoErr
}

type stubUserLookup struct {
	users    []*services.User
	listErr  error
	created  *services.User
	createErr error
}

func (s *stubUserLookup) List(_ context.Context, _ string) ([]*services.User, error) {
	return s.users, s.listErr
}
func (s *stubUserLookup) Create(_ context.Context, _ string, req services.CreateUserRequest) (*services.User, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	if s.created != nil {
		return s.created, nil
	}
	return &services.User{ID: "new-uid", Email: req.Email, Name: req.Name, Role: req.Role}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func setupOIDCApp(oidcSvc api.OIDCServiceIface, userSvc api.UserLookupIface, role string) *fiber.App {
	app := fiber.New()
	jwtSvc := services.NewJWTService("test-secret", 24*60*60*1000000000)
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("claims", &services.Claims{UserID: "uid1", TenantID: "tenant1", Role: role})
		return c.Next()
	})
	api.RegisterOIDCRoutes(app, oidcSvc, userSvc, jwtSvc)
	return app
}

func doRequest(app *fiber.App, method, path string, body interface{}) *http.Response {
	var req *http.Request
	if body != nil {
		b, _ := json.Marshal(body)
		req = httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	resp, _ := app.Test(req, -1)
	return resp
}

// ---------------------------------------------------------------------------
// GET /api/admin/sso/config
// ---------------------------------------------------------------------------

func TestGetSSOConfig_OK(t *testing.T) {
	cfg := &services.OIDCConfig{
		TenantID: "tenant1", Issuer: "https://idp.example.com",
		ClientID: "cid", ClientSecret: "secret", RedirectURI: "https://app/cb", Enabled: true,
	}
	svc := &stubOIDCSvc{cfg: cfg}
	app := setupOIDCApp(svc, &stubUserLookup{}, "admin")

	resp := doRequest(app, "GET", "/api/admin/sso/config", nil)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "https://idp.example.com", body["issuer"])
	assert.Equal(t, "***", body["client_secret"])
	assert.Equal(t, true, body["enabled"])
}

func TestGetSSOConfig_NotFound(t *testing.T) {
	svc := &stubOIDCSvc{getCfgErr: assert.AnError}
	app := setupOIDCApp(svc, &stubUserLookup{}, "admin")

	resp := doRequest(app, "GET", "/api/admin/sso/config", nil)
	assert.Equal(t, 404, resp.StatusCode)
}

func TestGetSSOConfig_NotAdmin(t *testing.T) {
	svc := &stubOIDCSvc{cfg: &services.OIDCConfig{}}
	app := setupOIDCApp(svc, &stubUserLookup{}, "standard")

	resp := doRequest(app, "GET", "/api/admin/sso/config", nil)
	assert.Equal(t, 403, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// PUT /api/admin/sso/config
// ---------------------------------------------------------------------------

func TestPutSSOConfig_OK(t *testing.T) {
	svc := &stubOIDCSvc{}
	app := setupOIDCApp(svc, &stubUserLookup{}, "admin")

	payload := map[string]interface{}{
		"issuer":        "https://idp.example.com",
		"client_id":     "cid",
		"client_secret": "secret",
		"redirect_uri":  "https://app/cb",
		"enabled":       true,
	}
	resp := doRequest(app, "PUT", "/api/admin/sso/config", payload)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "ok", body["status"])
}

func TestPutSSOConfig_MissingFields(t *testing.T) {
	svc := &stubOIDCSvc{}
	app := setupOIDCApp(svc, &stubUserLookup{}, "admin")

	resp := doRequest(app, "PUT", "/api/admin/sso/config", map[string]string{"issuer": "x"})
	assert.Equal(t, 400, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// POST /api/admin/sso/test
// ---------------------------------------------------------------------------

func TestTestSSOConnection_OK(t *testing.T) {
	svc := &stubOIDCSvc{cfg: &services.OIDCConfig{Issuer: "https://idp.example.com"}}
	app := setupOIDCApp(svc, &stubUserLookup{}, "admin")

	resp := doRequest(app, "POST", "/api/admin/sso/test", nil)
	assert.Equal(t, 200, resp.StatusCode)

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	assert.Equal(t, "ok", body["status"])
}

func TestTestSSOConnection_ConnFail(t *testing.T) {
	svc := &stubOIDCSvc{
		cfg:         &services.OIDCConfig{Issuer: "https://bad.example.com"},
		testConnErr: assert.AnError,
	}
	app := setupOIDCApp(svc, &stubUserLookup{}, "admin")

	resp := doRequest(app, "POST", "/api/admin/sso/test", nil)
	assert.Equal(t, 502, resp.StatusCode)
}

// ---------------------------------------------------------------------------
// GET /api/auth/oidc/login
// ---------------------------------------------------------------------------

func TestOIDCLogin_MissingTenant(t *testing.T) {
	svc := &stubOIDCSvc{}
	app := setupOIDCApp(svc, &stubUserLookup{}, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/login", nil)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestOIDCLogin_SSODisabled(t *testing.T) {
	svc := &stubOIDCSvc{cfg: &services.OIDCConfig{Enabled: false}}
	app := setupOIDCApp(svc, &stubUserLookup{}, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/login?tenant_id=tenant1", nil)
	assert.Equal(t, 403, resp.StatusCode)
}

func TestOIDCLogin_Redirect(t *testing.T) {
	svc := &stubOIDCSvc{
		cfg:     &services.OIDCConfig{Enabled: true},
		authURL: "https://idp.example.com/auth?response_type=code",
	}
	app := setupOIDCApp(svc, &stubUserLookup{}, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/login?tenant_id=tenant1", nil)
	assert.Equal(t, 302, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "https://idp.example.com/auth")
}

// ---------------------------------------------------------------------------
// GET /api/auth/oidc/callback
// ---------------------------------------------------------------------------

func TestOIDCCallback_MissingParams(t *testing.T) {
	svc := &stubOIDCSvc{}
	app := setupOIDCApp(svc, &stubUserLookup{}, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/callback", nil)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestOIDCCallback_InvalidState(t *testing.T) {
	svc := &stubOIDCSvc{stateErr: assert.AnError}
	app := setupOIDCApp(svc, &stubUserLookup{}, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/callback?state=bad&code=abc", nil)
	assert.Equal(t, 400, resp.StatusCode)
}

func TestOIDCCallback_ExistingUser(t *testing.T) {
	svc := &stubOIDCSvc{
		cfg:         &services.OIDCConfig{Issuer: "https://idp.example.com", Enabled: true},
		stateMap:    map[string]string{"validstate": "tenant1"},
		accessToken: "tok",
		email:       "alice@acme.com",
		name:        "Alice",
	}
	userSvc := &stubUserLookup{
		users: []*services.User{{ID: "u1", Email: "alice@acme.com", Role: "admin"}},
	}
	app := setupOIDCApp(svc, userSvc, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/callback?state=validstate&code=mycode", nil)
	require.Equal(t, 302, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "sso-callback?token=")
}

func TestOIDCCallback_NewUser(t *testing.T) {
	svc := &stubOIDCSvc{
		cfg:         &services.OIDCConfig{Issuer: "https://idp.example.com"},
		stateMap:    map[string]string{"s2": "tenant1"},
		accessToken: "tok",
		email:       "bob@acme.com",
		name:        "Bob",
	}
	userSvc := &stubUserLookup{users: []*services.User{}} // no existing users
	app := setupOIDCApp(svc, userSvc, "standard")

	resp := doRequest(app, "GET", "/api/auth/oidc/callback?state=s2&code=code2", nil)
	require.Equal(t, 302, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Location"), "sso-callback?token=")
}
