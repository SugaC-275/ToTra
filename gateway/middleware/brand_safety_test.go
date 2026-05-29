package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func newBrandSafetyApp(configs []BrandSafetyConfig) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &UserInfo{TenantID: "tenant1", UserID: "user1"})
		return c.Next()
	})
	app.Use(NewBrandSafetyMiddleware(configs))
	app.Post("/test", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})
	return app
}

func doPost(app *fiber.App, body string) (*http.Response, error) {
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return app.Test(req, -1)
}

func TestBrandSafety_CleanPassthrough(t *testing.T) {
	configs := []BrandSafetyConfig{
		{BlockedKeywords: []string{"competitor", "badword"}},
	}
	app := newBrandSafetyApp(configs)

	resp, err := doPost(app, `{"prompt":"a friendly cat"}`)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Brand-Safety-Check") != "passed" {
		t.Error("expected X-Brand-Safety-Check: passed header")
	}
}

func TestBrandSafety_BlockedKeyword(t *testing.T) {
	configs := []BrandSafetyConfig{
		{BlockedKeywords: []string{"competitor", "badword"}},
	}
	app := newBrandSafetyApp(configs)

	resp, err := doPost(app, `{"prompt":"use competitor product"}`)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "brand_safety_violation") {
		t.Errorf("body should contain brand_safety_violation, got: %s", body)
	}
	if !strings.Contains(string(body), "competitor") {
		t.Errorf("body should contain matched keyword, got: %s", body)
	}
}

func TestBrandSafety_CaseInsensitive(t *testing.T) {
	configs := []BrandSafetyConfig{
		{BlockedKeywords: []string{"Badword"}},
	}
	app := newBrandSafetyApp(configs)

	// keyword stored as "badword" (lowercased), body has "BADWORD"
	resp, err := doPost(app, `{"prompt":"this contains BADWORD in uppercase"}`)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestBrandSafety_TenantScoped_Match(t *testing.T) {
	configs := []BrandSafetyConfig{
		{BlockedKeywords: []string{"secret"}, TenantID: "tenant1"},
	}
	app := newBrandSafetyApp(configs)

	resp, err := doPost(app, `{"prompt":"this is secret"}`)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for matching tenant", resp.StatusCode)
	}
}

func TestBrandSafety_TenantScoped_OtherTenantSkipped(t *testing.T) {
	configs := []BrandSafetyConfig{
		{BlockedKeywords: []string{"secret"}, TenantID: "tenant99"},
	}
	app := newBrandSafetyApp(configs) // middleware sets tenant1, config targets tenant99

	resp, err := doPost(app, `{"prompt":"this is secret"}`)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (tenant mismatch should skip)", resp.StatusCode)
	}
}

func TestBrandSafety_EmptyKeywordIgnored(t *testing.T) {
	configs := []BrandSafetyConfig{
		{BlockedKeywords: []string{"", "  ", "actual"}},
	}
	app := newBrandSafetyApp(configs)

	// empty / blank keywords must not match everything
	resp, err := doPost(app, `{"prompt":"totally clean request"}`)
	if err != nil {
		t.Fatalf("test request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestLoadBrandSafetyConfig_EnvVar(t *testing.T) {
	t.Setenv("BRAND_SAFETY_KEYWORDS", "kw1, kw2 , kw3")
	cfgs := LoadBrandSafetyConfig()
	if len(cfgs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(cfgs))
	}
	if len(cfgs[0].BlockedKeywords) != 3 {
		t.Errorf("expected 3 keywords, got %d: %v", len(cfgs[0].BlockedKeywords), cfgs[0].BlockedKeywords)
	}
}

func TestLoadBrandSafetyConfig_Empty(t *testing.T) {
	os.Unsetenv("BRAND_SAFETY_KEYWORDS")
	cfgs := LoadBrandSafetyConfig()
	if len(cfgs) != 0 {
		t.Errorf("expected nil/empty, got %v", cfgs)
	}
}

func TestLoadBrandSafetyConfig_BlankEnvVar(t *testing.T) {
	t.Setenv("BRAND_SAFETY_KEYWORDS", "   ")
	cfgs := LoadBrandSafetyConfig()
	if len(cfgs) != 0 {
		t.Errorf("expected nil/empty for blank env, got %v", cfgs)
	}
}
