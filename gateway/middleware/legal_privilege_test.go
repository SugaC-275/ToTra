package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// --- ScanForPrivilege unit tests ---

func TestScanForPrivilege_AttorneyClient(t *testing.T) {
	text := "PRIVILEGED AND CONFIDENTIAL: Please review the attached legal advice from counsel."
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeAttorneyClient) {
		t.Errorf("ScanForPrivilege(%q): expected attorney_client, got %v", text, found)
	}
}

func TestScanForPrivilege_WorkProduct(t *testing.T) {
	text := "This memorandum constitutes attorney work product and was prepared in anticipation of litigation."
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeWorkProduct) {
		t.Errorf("ScanForPrivilege(%q): expected work_product, got %v", text, found)
	}
}

func TestScanForPrivilege_LitigationHold(t *testing.T) {
	text := "Please be advised that a litigation hold is in effect for all relevant communications."
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeLitigationHold) {
		t.Errorf("ScanForPrivilege(%q): expected litigation_hold, got %v", text, found)
	}
}

func TestScanForPrivilege_CleanText(t *testing.T) {
	clean := []string{
		"Please summarize the quarterly sales report.",
		"What is the status of the project timeline?",
		"Schedule a meeting for next Tuesday at 2pm.",
		"The product launch is planned for Q3.",
	}
	for _, text := range clean {
		t.Run(text, func(t *testing.T) {
			found := ScanForPrivilege(text)
			if len(found) != 0 {
				t.Errorf("ScanForPrivilege(%q): expected empty, got %v", text, found)
			}
		})
	}
}

func TestScanForPrivilege_DeduplicatesTypes(t *testing.T) {
	// Two attorney-client patterns in one message — should only produce one entry.
	text := "PRIVILEGED AND CONFIDENTIAL — attorney-client privilege applies."
	found := ScanForPrivilege(text)
	count := 0
	for _, p := range found {
		if p == PrivilegeTypeAttorneyClient {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 attorney_client entry, got %d (found=%v)", count, found)
	}
}

func TestScanForPrivilege_AttorneyEmailDomain(t *testing.T) {
	text := "From: partner@smithjones.law To: client@example.com"
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeAttorneyClient) {
		t.Errorf("ScanForPrivilege(%q): expected attorney_client for .law domain, got %v", text, found)
	}
}

func TestScanForPrivilege_LLPEmailDomain(t *testing.T) {
	text := "CC: associate@hendersonllp.com"
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeAttorneyClient) {
		t.Errorf("ScanForPrivilege(%q): expected attorney_client for llp.com domain, got %v", text, found)
	}
}

func TestScanForPrivilege_ACPPrefix(t *testing.T) {
	text := "Subject: ACP: Re: Merger due diligence questions"
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeAttorneyClient) {
		t.Errorf("ScanForPrivilege(%q): expected attorney_client for ACP: prefix, got %v", text, found)
	}
}

func TestScanForPrivilege_LegalHoldNotice(t *testing.T) {
	text := "This is a legal hold notice. Preserve all documents and emails related to this matter."
	found := ScanForPrivilege(text)
	if !containsPrivilege(found, PrivilegeTypeLitigationHold) {
		t.Errorf("ScanForPrivilege(%q): expected litigation_hold, got %v", text, found)
	}
}

// --- Middleware integration tests ---

func setupPrivilegeApp(siemChan chan<- SIEMEvent) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user", &UserInfo{TenantID: "t1", UserID: "u1"})
		return c.Next()
	})
	app.Use(NewLegalPrivilegeMiddleware(siemChan))
	app.Post("/", func(c *fiber.Ctx) error {
		detected, _ := c.Locals("privilege_detected").(bool)
		if detected {
			return c.Status(200).JSON(fiber.Map{"privileged": true})
		}
		return c.Status(200).JSON(fiber.Map{"privileged": false})
	})
	return app
}

func TestLegalPrivilegeMiddleware_SetsLocalsOnPrivilegedRequest(t *testing.T) {
	siemChan := make(chan SIEMEvent, 4)
	app := setupPrivilegeApp(siemChan)

	body := `{"messages":[{"role":"user","content":"PRIVILEGED AND CONFIDENTIAL: seeking legal advice from our outside counsel."}]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), `"privileged":true`) {
		t.Errorf("expected privilege_detected local to be set; body=%s", raw)
	}

	// SIEM event should have been sent.
	select {
	case ev := <-siemChan:
		if ev.EventType != "privilege_detected" {
			t.Errorf("expected SIEM event type 'privilege_detected', got %q", ev.EventType)
		}
	default:
		t.Error("expected a SIEM event but channel was empty")
	}

	// Response header must be set.
	if h := resp.Header.Get("X-Legal-Privilege"); h == "" {
		t.Error("expected X-Legal-Privilege response header to be set")
	}
}

func TestLegalPrivilegeMiddleware_PassesThroughCleanRequest(t *testing.T) {
	siemChan := make(chan SIEMEvent, 4)
	app := setupPrivilegeApp(siemChan)

	body := `{"messages":[{"role":"user","content":"What is the quarterly revenue forecast?"}]}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(raw), `"privileged":false`) {
		t.Errorf("expected privilege_detected to be unset; body=%s", raw)
	}

	// No SIEM event should have been sent.
	select {
	case ev := <-siemChan:
		t.Errorf("unexpected SIEM event for clean request: %+v", ev)
	default:
		// correct — no event
	}

	if h := resp.Header.Get("X-Legal-Privilege"); h != "" {
		t.Errorf("expected no X-Legal-Privilege header for clean request, got %q", h)
	}
}

// --- IsPrivilegedRequest helper test ---

func TestIsPrivilegedRequest_TrueWhenFlagged(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		c.Locals("privilege_detected", true)
		if !IsPrivilegedRequest(c) {
			return c.Status(500).SendString("expected true")
		}
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("IsPrivilegedRequest: expected true when local is set, got status %d", resp.StatusCode)
	}
}

func TestIsPrivilegedRequest_FalseWhenNotFlagged(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c *fiber.Ctx) error {
		if IsPrivilegedRequest(c) {
			return c.Status(500).SendString("expected false")
		}
		return c.SendStatus(200)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("IsPrivilegedRequest: expected false when local not set, got status %d", resp.StatusCode)
	}
}

// containsPrivilege is a test helper.
func containsPrivilege(types []PrivilegeType, target PrivilegeType) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}
