package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestScanForInsuranceData_Claim(t *testing.T) {
	types := ScanForInsuranceData("claim number CLM-2024-98765 filed last week.")
	if !containsInsurance(types, InsuranceClaim) {
		t.Errorf("expected claim type, got %v", types)
	}
}

func TestScanForInsuranceData_Actuarial(t *testing.T) {
	types := ScanForInsuranceData("The loss ratio and IBNR calculation need review.")
	if !containsInsurance(types, InsuranceActuarial) {
		t.Errorf("expected actuarial type, got %v", types)
	}
}

func TestScanForInsuranceData_Policy(t *testing.T) {
	types := ScanForInsuranceData("policy #POL-12345 premium due next month.")
	if !containsInsurance(types, InsurancePolicy) {
		t.Errorf("expected policy type, got %v", types)
	}
}

func TestScanForInsuranceData_CleanText(t *testing.T) {
	// Generic homeowners mention with no specific sensitive fields.
	types := ScanForInsuranceData("homeowners insurance quote for a new property.")
	if len(types) != 0 {
		t.Errorf("expected no insurance types for generic text, got %v", types)
	}
}

func TestScanForInsuranceData_Deduplication(t *testing.T) {
	// Multiple claim-pattern matches should produce exactly one claim entry.
	text := "claim number CLM-001 loss run claim reserve claimant information."
	types := ScanForInsuranceData(text)
	count := 0
	for _, tp := range types {
		if tp == InsuranceClaim {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 claim type, got %d (types=%v)", count, types)
	}
}

func TestInsuranceMiddlewareSetsLocalsAndHeader(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan SIEMEvent, 8)

	var capturedDetected any
	var capturedTypes any

	app.Use(NewInsuranceMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("insurance_data_detected")
		capturedTypes = c.Locals("insurance_data_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"claim number CLM-2024-00001 loss ratio actuarial review"}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected != true {
		t.Errorf("expected insurance_data_detected=true, got %v", capturedDetected)
	}
	types, ok := capturedTypes.([]InsuranceDataType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty insurance_data_types slice, got %v", capturedTypes)
	}

	if h := resp.Header.Get("X-Insurance-Data-Signal"); h != "true" {
		t.Errorf("expected X-Insurance-Data-Signal: true, got %q", h)
	}
}

func TestInsuranceMiddlewareCleanPassThrough(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan SIEMEvent, 8)

	var capturedDetected any

	app.Use(NewInsuranceMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("insurance_data_detected")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"what is machine learning?"}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected == true {
		t.Errorf("expected no insurance detection for clean text")
	}
	if h := resp.Header.Get("X-Insurance-Data-Signal"); h != "" {
		t.Errorf("expected no X-Insurance-Data-Signal header for clean text, got %q", h)
	}
}

// containsInsurance reports whether a given InsuranceDataType is present in the slice.
func containsInsurance(types []InsuranceDataType, want InsuranceDataType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
