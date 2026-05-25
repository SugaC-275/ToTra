package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- MaskPII unit tests ---

func TestMaskPII_Email(t *testing.T) {
	input := "Contact us at support@example.com for help."
	masked, found, piiType := middleware.MaskPII(input)
	if !found {
		t.Fatal("expected PII to be found")
	}
	if piiType != "email" {
		t.Errorf("expected piiType=email, got %q", piiType)
	}
	if strings.Contains(masked, "support@example.com") {
		t.Errorf("email should be redacted, got: %s", masked)
	}
	if !strings.Contains(masked, "[REDACTED:email]") {
		t.Errorf("expected [REDACTED:email] in output, got: %s", masked)
	}
}

func TestMaskPII_USSSN(t *testing.T) {
	input := "SSN: 123-45-6789"
	masked, found, _ := middleware.MaskPII(input)
	if !found {
		t.Fatal("expected PII to be found")
	}
	if strings.Contains(masked, "123-45-6789") {
		t.Errorf("SSN should be redacted, got: %s", masked)
	}
}

func TestMaskPII_NoPII(t *testing.T) {
	input := "The answer is 42. No PII here."
	masked, found, piiType := middleware.MaskPII(input)
	if found {
		t.Errorf("expected no PII, but found %q", piiType)
	}
	if masked != input {
		t.Errorf("text should be unchanged, got: %s", masked)
	}
}

func TestMaskPII_MultipleTypes(t *testing.T) {
	input := "Email: alice@corp.io SSN: 987-65-4321"
	masked, found, _ := middleware.MaskPII(input)
	if !found {
		t.Fatal("expected PII")
	}
	if strings.Contains(masked, "alice@corp.io") {
		t.Error("email should be redacted")
	}
	if strings.Contains(masked, "987-65-4321") {
		t.Error("SSN should be redacted")
	}
}

// --- NewPIIResponseMiddleware integration tests ---

// buildPIIResponseApp constructs a Fiber app that returns responseBody as a
// plain-text response and then runs the PII response middleware after it.
//
// NOTE: In Fiber, middleware registered *before* a handler runs before it. To
// achieve post-call semantics (i.e., scan after the handler writes the body),
// we register the middleware first (it calls c.Next() internally) and the
// downstream handler after.
func buildPIIResponseApp(responseBody string, siemChan chan<- middleware.SIEMEvent, user *middleware.UserInfo) *fiber.App {
	app := fiber.New()
	app.Get("/test", middleware.NewPIIResponseMiddleware(siemChan), func(c *fiber.Ctx) error {
		if user != nil {
			c.Locals("user", user)
		}
		return c.Status(fiber.StatusOK).SendString(responseBody)
	})
	return app
}

func TestPIIResponseMiddleware_MasksEmail(t *testing.T) {
	siemChan := make(chan middleware.SIEMEvent, 1)
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}

	app := buildPIIResponseApp("Here is your result. Contact alice@secret.com for details.", siemChan, user)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req, 3000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)

	if strings.Contains(bodyStr, "alice@secret.com") {
		t.Errorf("email should be masked, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "[REDACTED:email]") {
		t.Errorf("expected [REDACTED:email] in body, got: %s", bodyStr)
	}
	if resp.Header.Get("X-PII-Masked") != "true" {
		t.Errorf("expected X-PII-Masked: true header")
	}

	// SIEM event should have been sent.
	select {
	case evt := <-siemChan:
		if evt.EventType != "pii_in_response" {
			t.Errorf("expected event_type=pii_in_response, got %q", evt.EventType)
		}
	default:
		t.Error("expected SIEM event but channel was empty")
	}
}

func TestPIIResponseMiddleware_NoPII(t *testing.T) {
	siemChan := make(chan middleware.SIEMEvent, 1)

	app := buildPIIResponseApp("The result is: forty-two.", siemChan, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req, 3000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "forty-two") {
		t.Errorf("body should be unchanged, got: %s", body)
	}
	if resp.Header.Get("X-PII-Masked") != "" {
		t.Error("X-PII-Masked header should not be set when no PII found")
	}

	select {
	case evt := <-siemChan:
		t.Errorf("unexpected SIEM event: %+v", evt)
	default:
		// correct — no event expected
	}
}

func TestPIIResponseMiddleware_NilSIEMChan(t *testing.T) {
	// Verify no panic when siemChan is nil and PII is present.
	app := buildPIIResponseApp("Call 555-123-4567 now!", nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req, 3000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}
