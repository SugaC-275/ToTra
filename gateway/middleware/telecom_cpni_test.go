package middleware_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- ScanForCPNI unit tests ---

func TestScanForCPNI_CallRecords(t *testing.T) {
	types := middleware.ScanForCPNI("CDR analysis for subscriber 001")
	if !containsCPNI(types, middleware.CPNICallRecords) {
		t.Errorf("expected call_records, got %v", types)
	}
}

func TestScanForCPNI_NetworkUsage(t *testing.T) {
	types := middleware.ScanForCPNI("data usage in GB this month")
	if !containsCPNI(types, middleware.CPNINetworkUsage) {
		t.Errorf("expected network_usage, got %v", types)
	}
}

func TestScanForCPNI_IMSI(t *testing.T) {
	types := middleware.ScanForCPNI("IMSI 310260000000000 subscriber record")
	if !containsCPNI(types, middleware.CPNIServiceInfo) {
		t.Errorf("expected service_info for IMSI, got %v", types)
	}
}

func TestScanForCPNI_LocationData(t *testing.T) {
	types := middleware.ScanForCPNI("cell tower location eNodeB 42")
	if !containsCPNI(types, middleware.CPNILocationData) {
		t.Errorf("expected location_data, got %v", types)
	}
}

func TestScanForCPNI_CleanText(t *testing.T) {
	types := middleware.ScanForCPNI("quarterly revenue report for Q3 2024")
	if len(types) != 0 {
		t.Errorf("expected no CPNI types for clean text, got %v", types)
	}
}

// --- Middleware integration test ---

func TestCPNIMiddlewareSetsHeaderAndLocals(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan middleware.SIEMEvent, 8)

	var capturedDetected any
	var capturedTypes any

	app.Use(middleware.NewTelecomCPNIMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("cpni_detected")
		capturedTypes = c.Locals("cpni_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"Please pull the CDR for this subscriber account."}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected != true {
		t.Errorf("expected cpni_detected=true, got %v", capturedDetected)
	}
	types, ok := capturedTypes.([]middleware.CPNIType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty cpni_types slice, got %v", capturedTypes)
	}

	headerVal := resp.Header.Get("X-CPNI-Detected")
	if headerVal != "true" {
		t.Errorf("expected X-CPNI-Detected: true header, got %q", headerVal)
	}
}

// --- helpers ---

func containsCPNI(types []middleware.CPNIType, want middleware.CPNIType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
