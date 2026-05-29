package api

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/xml"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/admin/services"
)

// SAMLServiceIface is the subset of services.SAMLService used by the handlers.
type SAMLServiceIface interface {
	GetConfig(ctx context.Context, tenantID string) (*services.SAMLConfig, error)
	UpsertConfig(ctx context.Context, cfg *services.SAMLConfig) error
	GetAttributeBundleRules(ctx context.Context, tenantID string) ([]services.AttributeBundleRule, error)
	UpsertAttributeBundleRule(ctx context.Context, rule *services.AttributeBundleRule) error
	DeleteAttributeBundleRule(ctx context.Context, id, tenantID string) error
	ResolveBundlesFromAttributes(ctx context.Context, tenantID string, attrs map[string][]string) ([]string, error)
}

// RegisterSAMLAdminRoutes mounts JWT-protected SAML admin routes.
func RegisterSAMLAdminRoutes(r fiber.Router, svc SAMLServiceIface) {
	r.Get("/api/admin/saml/config", getSAMLConfig(svc))
	r.Put("/api/admin/saml/config", putSAMLConfig(svc))
	r.Get("/api/admin/saml/rules", listSAMLRules(svc))
	r.Post("/api/admin/saml/rules", createSAMLRule(svc))
	r.Delete("/api/admin/saml/rules/:id", deleteSAMLRule(svc))
}

// RegisterSAMLPublicRoutes mounts unauthenticated SAML SP endpoints.
func RegisterSAMLPublicRoutes(r fiber.Router, svc SAMLServiceIface, users UserLookupIface, jwtSvc *services.JWTService) {
	r.Get("/saml/metadata", samlMetadata(svc))
	r.Get("/saml/login", samlLoginRedirect(svc))
	r.Post("/saml/acs", samlACS(svc, users, jwtSvc))
}

// ---------------------------------------------------------------------------
// Admin config handlers
// ---------------------------------------------------------------------------

func getSAMLConfig(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		cfg, err := svc.GetConfig(c.Context(), claims.TenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "no SAML config found"})
		}
		return c.JSON(cfg)
	}
}

func putSAMLConfig(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var body services.SAMLConfig
		if err := c.BodyParser(&body); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		body.TenantID = claims.TenantID
		if err := svc.UpsertConfig(c.Context(), &body); err != nil {
			return serverError(c, err)
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// Attribute-bundle rule management
// ---------------------------------------------------------------------------

func listSAMLRules(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		rules, err := svc.GetAttributeBundleRules(c.Context(), claims.TenantID)
		if err != nil {
			return serverError(c, err)
		}
		if rules == nil {
			rules = []services.AttributeBundleRule{}
		}
		return c.JSON(rules)
	}
}

func createSAMLRule(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		var rule services.AttributeBundleRule
		if err := c.BodyParser(&rule); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid body"})
		}
		rule.TenantID = claims.TenantID
		if err := svc.UpsertAttributeBundleRule(c.Context(), &rule); err != nil {
			return serverError(c, err)
		}
		return c.Status(201).JSON(fiber.Map{"status": "ok"})
	}
}

func deleteSAMLRule(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		claims := c.Locals("claims").(*services.Claims)
		if claims.Role != "admin" {
			return c.Status(403).JSON(fiber.Map{"error": "admin only"})
		}
		id := c.Params("id")
		if id == "" {
			return c.Status(400).JSON(fiber.Map{"error": "id required"})
		}
		if err := svc.DeleteAttributeBundleRule(c.Context(), id, claims.TenantID); err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"status": "ok"})
	}
}

// ---------------------------------------------------------------------------
// SAML SP public endpoints
// ---------------------------------------------------------------------------

// samlMetadata serves the SP metadata XML so the IdP can configure trust.
// Query param: tenant_id (required).
func samlMetadata(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		sp, err := buildSP(c.Context(), svc, tenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		metaXML, err := xml.MarshalIndent(sp.Metadata(), "", "  ")
		if err != nil {
			return serverError(c, err)
		}
		c.Set("Content-Type", "application/xml")
		return c.Send(metaXML)
	}
}

// samlLoginRedirect initiates the SP-initiated SSO flow.
// Query param: tenant_id (required).
func samlLoginRedirect(svc SAMLServiceIface) fiber.Handler {
	return func(c *fiber.Ctx) error {
		tenantID := c.Query("tenant_id")
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required"})
		}
		sp, err := buildSP(c.Context(), svc, tenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		authURL, err := sp.MakeRedirectAuthenticationRequest(tenantID)
		if err != nil {
			return serverError(c, err)
		}
		return c.Redirect(authURL.String(), fiber.StatusFound)
	}
}

// samlACS is the Assertion Consumer Service endpoint.
// It validates the SAML response, extracts attributes, resolves bundles,
// and issues a JWT with bundle_ids — mirroring the OIDC callback flow.
func samlACS(svc SAMLServiceIface, users UserLookupIface, jwtSvc *services.JWTService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// RelayState carries the tenant_id set during MakeRedirectAuthenticationRequest.
		tenantID := c.FormValue("RelayState")
		if tenantID == "" {
			tenantID = c.Query("tenant_id")
		}
		if tenantID == "" {
			return c.Status(400).JSON(fiber.Map{"error": "tenant_id required (RelayState or query param)"})
		}

		sp, err := buildSP(c.Context(), svc, tenantID)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": "SAML not configured for tenant"})
		}

		samlResponse := c.FormValue("SAMLResponse")
		if samlResponse == "" {
			return c.Status(400).JSON(fiber.Map{"error": "missing SAMLResponse"})
		}

		// Build a synthetic http.Request so crewjam/saml can parse the POST body.
		req, err := buildHTTPRequest(c, samlResponse, tenantID)
		if err != nil {
			return serverError(c, err)
		}

		assertion, err := sp.ParseResponse(req, []string{""})
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": fmt.Sprintf("invalid SAML assertion: %v", err)})
		}

		// Extract attributes from assertion.
		email, name, attrs := extractAssertionAttrs(assertion)
		if email == "" {
			return c.Status(400).JSON(fiber.Map{"error": "SAML assertion missing email attribute"})
		}

		// Resolve compliance bundles from IdP attributes.
		bundleIDs, err := svc.ResolveBundlesFromAttributes(c.Context(), tenantID, attrs)
		if err != nil {
			// Non-fatal: proceed without bundle assignment.
			bundleIDs = nil
		}

		// Look up or provision the user.
		userID, role, err := findOrCreateUser(c.Context(), users, tenantID, email, name)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "user provisioning failed"})
		}

		// Issue JWT with bundle_ids when bundles were resolved.
		var token string
		if len(bundleIDs) > 0 {
			token, err = jwtSvc.IssueWithBundles(userID, tenantID, role, bundleIDs)
		} else {
			token, err = jwtSvc.Issue(userID, tenantID, role)
		}
		if err != nil {
			return c.Status(500).JSON(fiber.Map{"error": "token generation failed"})
		}

		return c.Redirect("/#/sso-callback?token="+token, fiber.StatusFound)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// buildSP constructs a crewjam/saml ServiceProvider from the stored config.
func buildSP(ctx context.Context, svc SAMLServiceIface, tenantID string) (*saml.ServiceProvider, error) {
	cfg, err := svc.GetConfig(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("saml config not found: %w", err)
	}
	if !cfg.IsActive {
		return nil, fmt.Errorf("SAML is disabled for this tenant")
	}

	acsURL, err := url.Parse(cfg.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("invalid acs_url: %w", err)
	}

	// Fetch or parse IdP metadata.
	var idpMeta *saml.EntityDescriptor
	switch {
	case cfg.IDPMetadataURL != "":
		idpMeta, err = fetchIDPMetadata(ctx, cfg.IDPMetadataURL)
		if err != nil {
			return nil, fmt.Errorf("fetch idp metadata: %w", err)
		}
	case cfg.IDPMetadataXML != "":
		idpMeta, err = samlsp.ParseMetadata([]byte(cfg.IDPMetadataXML))
		if err != nil {
			return nil, fmt.Errorf("parse idp metadata xml: %w", err)
		}
	default:
		return nil, fmt.Errorf("either idp_metadata_url or idp_metadata_xml must be set")
	}

	// Generate an ephemeral RSA key pair for the SP.
	key, cert, err := generateSPKeypair(cfg.EntityID)
	if err != nil {
		return nil, fmt.Errorf("generate sp keypair: %w", err)
	}

	sp := saml.ServiceProvider{
		EntityID:    cfg.EntityID,
		Key:         key,
		Certificate: cert,
		AcsURL:      *acsURL,
		IDPMetadata: idpMeta,
	}
	return &sp, nil
}

// generateSPKeypair creates a self-signed RSA key pair for the SP.
// In production, replace with a persisted certificate loaded from config.
func generateSPKeypair(commonName string) (*rsa.PrivateKey, *x509.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(10 * 365 * 24 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}
	return key, cert, nil
}

// fetchIDPMetadata downloads and parses IdP metadata from a URL.
func fetchIDPMetadata(ctx context.Context, metaURL string) (*saml.EntityDescriptor, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch metadata: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read metadata body: %w", err)
	}
	return samlsp.ParseMetadata(raw)
}

// extractAssertionAttrs pulls email, name, and all SAML attributes from an assertion.
func extractAssertionAttrs(a *saml.Assertion) (email, name string, attrs map[string][]string) {
	attrs = make(map[string][]string)
	if a == nil {
		return
	}
	for _, stmt := range a.AttributeStatements {
		for _, attr := range stmt.Attributes {
			var vals []string
			for _, v := range attr.Values {
				vals = append(vals, v.Value)
			}
			attrs[attr.Name] = vals
			if attr.FriendlyName != "" {
				attrs[attr.FriendlyName] = vals
			}
		}
	}
	// Common email attribute names across IdPs.
	for _, key := range []string{
		"email", "mail", "emailAddress",
		"urn:oid:0.9.2342.19200300.100.1.3",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
	} {
		if vals, ok := attrs[key]; ok && len(vals) > 0 {
			email = vals[0]
			break
		}
	}
	// Subject NameID as fallback email.
	if email == "" && a.Subject != nil && a.Subject.NameID != nil {
		email = a.Subject.NameID.Value
	}
	// Common display name attribute names.
	for _, key := range []string{
		"displayName", "cn", "name",
		"http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name",
	} {
		if vals, ok := attrs[key]; ok && len(vals) > 0 {
			name = vals[0]
			break
		}
	}
	return
}

// buildHTTPRequest constructs a minimal *http.Request from a Fiber context
// so that crewjam/saml can parse the POST SAMLResponse.
func buildHTTPRequest(c *fiber.Ctx, samlResponse, relayState string) (*http.Request, error) {
	form := url.Values{}
	form.Set("SAMLResponse", samlResponse)
	if relayState != "" {
		form.Set("RelayState", relayState)
	}
	req, err := http.NewRequest(http.MethodPost, string(c.Request().URI().FullURI()), nil)
	if err != nil {
		return nil, err
	}
	req.Form = form
	req.PostForm = form
	req.Header = make(http.Header)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Mark as HTTPS to satisfy crewjam/saml's TLS check.
	req.TLS = &tls.ConnectionState{}
	return req, nil
}
