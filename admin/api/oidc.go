package api

import (
	"context"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// OIDCServiceIface is satisfied by services.OIDCService.
type OIDCServiceIface interface {
	GetConfig(ctx context.Context, tenantID string) (*services.OIDCConfig, error)
	SetConfig(ctx context.Context, cfg *services.OIDCConfig) error
	TestConnection(ctx context.Context, issuer string) error
	GenerateAuthURL(ctx context.Context, cfg *services.OIDCConfig, tenantID string) (string, error)
	ValidateState(state string) (string, error)
	ExchangeCode(ctx context.Context, cfg *services.OIDCConfig, code string) (string, error)
	GetUserInfo(ctx context.Context, cfg *services.OIDCConfig, accessToken string) (email, name string, err error)
}

// UserLookupIface is the subset of UserService needed for SSO callback.
type UserLookupIface interface {
	List(ctx context.Context, tenantID string) ([]*services.User, error)
	Create(ctx context.Context, tenantID string, req services.CreateUserRequest) (*services.User, error)
}

// RegisterOIDCRoutes registers all SSO/OIDC routes on the same router.
// Use RegisterOIDCAdminRoutes + RegisterOIDCPublicRoutes when the admin and
// public routes need to be on different fiber.Router groups.
func RegisterOIDCRoutes(app fiber.Router, svc OIDCServiceIface, users UserLookupIface, jwtSvc *services.JWTService) {
	app.Get("/api/admin/sso/config", getSSOConfig(svc))
	app.Put("/api/admin/sso/config", putSSOConfig(svc))
	app.Post("/api/admin/sso/test", testSSOConnection(svc))
	app.Get("/api/auth/oidc/login", oidcLoginRedirect(svc))
	app.Get("/api/auth/oidc/callback", oidcCallback(svc, users, jwtSvc))
}

// RegisterOIDCAdminRoutes registers only the JWT-protected SSO config routes.
func RegisterOIDCAdminRoutes(r fiber.Router, svc OIDCServiceIface) {
	r.Get("/api/admin/sso/config", getSSOConfig(svc))
	r.Put("/api/admin/sso/config", putSSOConfig(svc))
	r.Post("/api/admin/sso/test", testSSOConnection(svc))
}

// RegisterOIDCPublicRoutes registers the public OIDC login/callback routes (no JWT required).
func RegisterOIDCPublicRoutes(r fiber.Router, svc OIDCServiceIface, users UserLookupIface, jwtSvc *services.JWTService) {
	r.Get("/api/auth/oidc/login", oidcLoginRedirect(svc))
	r.Get("/api/auth/oidc/callback", oidcCallback(svc, users, jwtSvc))
}

// getSSOConfig returns the current OIDC config for the caller's tenant (client_secret masked).
func getSSOConfig(svc OIDCServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		cfg, err := svc.GetConfig(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "no SSO config found"})
		}
		return c.JSON(fiber.Map{
			"tenant_id":    cfg.TenantID,
			"issuer":       cfg.Issuer,
			"client_id":    cfg.ClientID,
			"client_secret": "***",
			"redirect_uri": cfg.RedirectURI,
			"enabled":      cfg.Enabled,
		})
	}
}

// putSSOConfig creates or updates the OIDC config for the caller's tenant.
func putSSOConfig(svc OIDCServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body struct {
			Issuer       string `json:"issuer"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			RedirectURI  string `json:"redirect_uri"`
			Enabled      bool   `json:"enabled"`
		}
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		if body.Issuer == "" || body.ClientID == "" || body.ClientSecret == "" || body.RedirectURI == "" {
			return c.Status(400).JSON(fiber.Map{"error": "issuer, client_id, client_secret and redirect_uri are required"})
		}
		cfg := &services.OIDCConfig{
			TenantID:     claims.TenantID,
			Issuer:       body.Issuer,
			ClientID:     body.ClientID,
			ClientSecret: body.ClientSecret,
			RedirectURI:  body.RedirectURI,
			Enabled:      body.Enabled,
		}
		if err := svc.SetConfig(c.Context(), cfg); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// testSSOConnection fetches the OIDC discovery document to verify connectivity.
func testSSOConnection(svc OIDCServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		cfg, err := svc.GetConfig(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "no SSO config found"})
		}
		if err := svc.TestConnection(c.Context(), cfg.Issuer); err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "could not reach OIDC issuer"})
		}
		return c.JSON(fiber.Map{"status": "ok", "issuer": cfg.Issuer})
	}
}

// oidcLoginRedirect redirects the browser to the OIDC provider's authorization endpoint.
// Query param: tenant_id (required).
func oidcLoginRedirect(svc OIDCServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		cfg, err := svc.GetConfig(c.Context(), tenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "SSO not configured for this tenant"})
		}
		if !cfg.Enabled {
			return c.Status(403).JSON(fiber.Map{"error": "SSO is disabled for this tenant"})
		}
		authURL, err := svc.GenerateAuthURL(c.Context(), cfg, tenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.Redirect(authURL, fiber.StatusFound)
	}
}

// oidcCallback handles the provider redirect, issues a JWT, and redirects to the dashboard.
func oidcCallback(svc OIDCServiceIface, users UserLookupIface, jwtSvc *services.JWTService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		state := c.Query("state")
		code := c.Query("code")
		if state == "" || code == "" {
			return c.Status(400).JSON(fiber.Map{"error": "state and code are required"})
		}

		// Validate state and recover tenantID.
		tenantID, err := svc.ValidateState(state)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid or expired state"})
		}

		cfg, err := svc.GetConfig(c.Context(), tenantID)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "SSO config unavailable"})
		}

		// Exchange code for access token.
		accessToken, err := svc.ExchangeCode(c.Context(), cfg, code)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": "token exchange failed"})
		}

		// Fetch user identity from provider.
		email, name, err := svc.GetUserInfo(c.Context(), cfg, accessToken)
		if err != nil {
			return c.Status(502).JSON(fiber.Map{"error": "userinfo fetch failed"})
		}

		// Look up or create local user.
		userID, role, err := findOrCreateUser(c.Context(), users, tenantID, email, name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "user provisioning failed"})
		}

		// Issue JWT.
		token, err := jwtSvc.Issue(userID, tenantID, role)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "token generation failed"})
		}

		// Redirect to dashboard with token in fragment.
		return c.Redirect("/#/sso-callback?token="+token, fiber.StatusFound)
	}
}

// findOrCreateUser looks up a user by email in the tenant; creates one (role=standard) if absent.
func findOrCreateUser(ctx context.Context, users UserLookupIface, tenantID, email, name string) (userID, role string, err error) {
	list, err := users.List(ctx, tenantID)
	if err != nil {
		return "", "", err
	}
	for _, u := range list {
		if u.Email == email {
			return u.ID, u.Role, nil
		}
	}
	// Provision new user via SSO.
	displayName := name
	if displayName == "" {
		displayName = email
	}
	u, err := users.Create(ctx, tenantID, services.CreateUserRequest{
		Name:  displayName,
		Email: email,
		Role:  "standard",
	})
	if err != nil {
		return "", "", err
	}
	return u.ID, u.Role, nil
}
