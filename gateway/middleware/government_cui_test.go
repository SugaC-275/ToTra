package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// ---- ScanForCUI tests -------------------------------------------------------

func TestScanForCUI_DetectsFOUO(t *testing.T) {
	cats := ScanForCUI("This document is FOUO. Handle accordingly.")
	if len(cats) == 0 {
		t.Fatal("expected at least one CUI category, got none")
	}
	found := false
	for _, c := range cats {
		if c == CUIPrivacy {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CUIPrivacy in %v", cats)
	}
}

func TestScanForCUI_DetectsITAR(t *testing.T) {
	cats := ScanForCUI("The component is ITAR-controlled under 22 CFR part 120.")
	if len(cats) == 0 {
		t.Fatal("expected at least one CUI category, got none")
	}
	found := false
	for _, c := range cats {
		if c == CUIExport {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CUIExport in %v", cats)
	}
}

func TestScanForCUI_DetectsSCADACriticalInfra(t *testing.T) {
	cats := ScanForCUI("The SCADA system monitors the water treatment facility.")
	if len(cats) == 0 {
		t.Fatal("expected at least one CUI category, got none")
	}
	found := false
	for _, c := range cats {
		if c == CUIInfrastructure {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected CUIInfrastructure in %v", cats)
	}
}

func TestScanForCUI_CleanText(t *testing.T) {
	cats := ScanForCUI("Hello, please summarize this article about machine learning.")
	if len(cats) != 0 {
		t.Errorf("expected no CUI categories for clean text, got %v", cats)
	}
}

// ---- CUIClassification tests ------------------------------------------------

func TestCUIClassification_ExportControl(t *testing.T) {
	result := CUIClassification([]CUICategory{CUIExport})
	if result != "CUI//SP-ITAR" {
		t.Errorf("expected CUI//SP-ITAR, got %q", result)
	}
}

func TestCUIClassification_EmptyCategories(t *testing.T) {
	result := CUIClassification(nil)
	if result != "" {
		t.Errorf("expected empty string for nil categories, got %q", result)
	}
}

func TestCUIClassification_LawEnforcementOnly(t *testing.T) {
	result := CUIClassification([]CUICategory{CUILawEnforcement})
	if result != "CUI//LES" {
		t.Errorf("expected CUI//LES, got %q", result)
	}
}

// ExportControl beats LawEnforcement in priority.
func TestCUIClassification_ExportTrumpsLES(t *testing.T) {
	result := CUIClassification([]CUICategory{CUILawEnforcement, CUIExport})
	if result != "CUI//SP-ITAR" {
		t.Errorf("expected CUI//SP-ITAR (highest priority), got %q", result)
	}
}

// ---- FedRAMPLevelSatisfied tests --------------------------------------------

func TestFedRAMPLevelSatisfied_HighSatisfiesModerate(t *testing.T) {
	if !FedRAMPLevelSatisfied(FedRAMPHigh, FedRAMPModerate) {
		t.Error("High should satisfy Moderate requirement")
	}
}

func TestFedRAMPLevelSatisfied_LowDoesNotSatisfyHigh(t *testing.T) {
	if FedRAMPLevelSatisfied(FedRAMPLow, FedRAMPHigh) {
		t.Error("Low should NOT satisfy High requirement")
	}
}

func TestFedRAMPLevelSatisfied_GovCloudSatisfiesAll(t *testing.T) {
	for _, req := range []FedRAMPLevel{FedRAMPLow, FedRAMPModerate, FedRAMPHigh, FedRAMPGovCloud} {
		if !FedRAMPLevelSatisfied(FedRAMPGovCloud, req) {
			t.Errorf("GovCloud should satisfy %q", req)
		}
	}
}

func TestFedRAMPLevelSatisfied_EmptyRequiredAlwaysTrue(t *testing.T) {
	if !FedRAMPLevelSatisfied("", "") {
		t.Error("empty model level should satisfy empty requirement")
	}
	if !FedRAMPLevelSatisfied(FedRAMPLow, "") {
		t.Error("any model level should satisfy empty requirement")
	}
}

func TestFedRAMPLevelSatisfied_NoModelLevelFails(t *testing.T) {
	if FedRAMPLevelSatisfied("", FedRAMPLow) {
		t.Error("no model level should NOT satisfy any requirement")
	}
}

func TestFedRAMPLevelSatisfied_SameLevelSatisfies(t *testing.T) {
	if !FedRAMPLevelSatisfied(FedRAMPModerate, FedRAMPModerate) {
		t.Error("Moderate should satisfy Moderate requirement")
	}
}

// ---- RequiredFedRAMPLevel tests ---------------------------------------------

func TestRequiredFedRAMPLevel_NoCUINoRequirement(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		level := RequiredFedRAMPLevel(c)
		if level != "" {
			return c.Status(500).SendString("unexpected level: " + string(level))
		}
		return c.SendString("ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, body)
	}
}

func TestRequiredFedRAMPLevel_ExportControlRequiresHigh(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c *fiber.Ctx) error {
		c.Locals("cui_detected", true)
		c.Locals("cui_categories", []CUICategory{CUIExport})
		level := RequiredFedRAMPLevel(c)
		if level != FedRAMPHigh {
			return c.Status(500).SendString("expected high, got " + string(level))
		}
		return c.SendString("ok")
	})
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, body)
	}
}

// ---- Middleware integration test --------------------------------------------

func TestGovernmentCUIMiddleware_SetsLocalsOnDetection(t *testing.T) {
	siemChan := make(chan SIEMEvent, 10)
	app := fiber.New()
	app.Use(NewGovernmentCUIMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		detected, _ := c.Locals("cui_detected").(bool)
		cats, _ := c.Locals("cui_categories").([]CUICategory)
		classification, _ := c.Locals("cui_classification").(string)
		if !detected {
			return c.Status(500).SendString("cui_detected not set")
		}
		if len(cats) == 0 {
			return c.Status(500).SendString("cui_categories empty")
		}
		if classification == "" {
			return c.Status(500).SendString("cui_classification empty")
		}
		return c.SendString("ok")
	})

	body := strings.NewReader(`{"messages":[{"role":"user","content":"This document is FOUO and contains ITAR-controlled technical data."}]}`)
	req := httptest.NewRequest(http.MethodPost, "/chat", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("handler failed (status %d): %s", resp.StatusCode, respBody)
	}

	// Verify X-CUI-Detected header is set.
	if resp.Header.Get("X-CUI-Detected") != "true" {
		t.Error("expected X-CUI-Detected: true header")
	}

	// Verify SIEM event was emitted.
	if len(siemChan) == 0 {
		t.Error("expected a SIEM event to be emitted")
	} else {
		event := <-siemChan
		if event.EventType != "cui_detected" {
			t.Errorf("expected SIEM event type 'cui_detected', got %q", event.EventType)
		}
	}
}

func TestGovernmentCUIMiddleware_PassesThroughCleanRequest(t *testing.T) {
	siemChan := make(chan SIEMEvent, 10)
	app := fiber.New()
	app.Use(NewGovernmentCUIMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		detected, _ := c.Locals("cui_detected").(bool)
		if detected {
			return c.Status(500).SendString("false positive: CUI detected on clean text")
		}
		return c.SendString("ok")
	})

	body := strings.NewReader(`{"messages":[{"role":"user","content":"What is the capital of France?"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/chat", body)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("handler failed (status %d): %s", resp.StatusCode, respBody)
	}

	if resp.Header.Get("X-CUI-Detected") == "true" {
		t.Error("X-CUI-Detected should not be set for clean requests")
	}
	if len(siemChan) != 0 {
		t.Errorf("expected no SIEM events for clean request, got %d", len(siemChan))
	}
}
