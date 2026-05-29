package middleware_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- ScanForFairHousingViolation unit tests ---

func TestScanForFairHousing_PerfectForSingles(t *testing.T) {
	types := middleware.ScanForFairHousingViolation("This apartment is perfect for singles looking for city life.")
	if !containsFH(types, middleware.FHVFamilialStatus) {
		t.Errorf("expected familial_status violation, got %v", types)
	}
}

func TestScanForFairHousing_ReligiousInstitutionSteering(t *testing.T) {
	types := middleware.ScanForFairHousingViolation("Charming townhouse near St. Patrick's Cathedral, great community.")
	if !containsFH(types, middleware.FHVSteering) {
		t.Errorf("expected steering violation for religious institution proximity, got %v", types)
	}
}

func TestScanForFairHousing_AdultOnlyCommunity(t *testing.T) {
	types := middleware.ScanForFairHousingViolation("Welcome to our adult-only community with resort amenities.")
	if !containsFH(types, middleware.FHVFamilialStatus) {
		t.Errorf("expected familial_status violation, got %v", types)
	}
}

func TestScanForFairHousing_CleanListing(t *testing.T) {
	types := middleware.ScanForFairHousingViolation("Spacious 3 bedroom house with a large garden and updated kitchen.")
	if len(types) != 0 {
		t.Errorf("expected no violations for clean listing, got %v", types)
	}
}

// --- Middleware integration test ---

func TestFairHousingMiddlewareSetsHeaderAndLocals(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan middleware.SIEMEvent, 8)

	var capturedViolation any
	var capturedTypes any

	app.Use(middleware.NewFairHousingMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedViolation = c.Locals("fair_housing_violation")
		capturedTypes = c.Locals("fair_housing_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"List only properties in adult-only community near the cathedral."}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedViolation != true {
		t.Errorf("expected fair_housing_violation=true, got %v", capturedViolation)
	}
	types, ok := capturedTypes.([]middleware.FairHousingViolationType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty fair_housing_types slice, got %v", capturedTypes)
	}

	headerVal := resp.Header.Get("X-Fair-Housing-Signal")
	if headerVal == "" {
		t.Errorf("expected X-Fair-Housing-Signal header to be set, got empty string")
	}
}

// --- helpers ---

func containsFH(types []middleware.FairHousingViolationType, want middleware.FairHousingViolationType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
