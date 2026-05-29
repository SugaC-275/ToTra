package middleware_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- ScanForEEOCBias unit tests ---

func TestScanForEEOCBias_GenderManpower(t *testing.T) {
	types := middleware.ScanForEEOCBias("We need additional manpower for the project.")
	if !containsEEOC(types, middleware.BiasGender) {
		t.Errorf("expected gender bias, got %v", types)
	}
}

func TestScanForEEOCBias_AgeRecentGraduate(t *testing.T) {
	types := middleware.ScanForEEOCBias("We are looking for a recent graduate preferred for this role.")
	if !containsEEOC(types, middleware.BiasAge) {
		t.Errorf("expected age bias, got %v", types)
	}
}

func TestScanForEEOCBias_DisabilityAbleBodied(t *testing.T) {
	types := middleware.ScanForEEOCBias("We require an able-bodied candidate required for this position.")
	if !containsEEOC(types, middleware.BiasDisability) {
		t.Errorf("expected disability bias, got %v", types)
	}
}

func TestScanForEEOCBias_SeniorTitleClean(t *testing.T) {
	// "senior" in a job title is a role level, not an age bias signal
	types := middleware.ScanForEEOCBias("We are hiring a Senior Software Engineer to join our platform team.")
	if len(types) != 0 {
		t.Errorf("expected no EEOC bias for senior job title, got %v", types)
	}
}

func TestScanForEEOCBias_CleanText(t *testing.T) {
	types := middleware.ScanForEEOCBias("The candidate should have 5 years of Go experience and strong communication skills.")
	if len(types) != 0 {
		t.Errorf("expected no EEOC bias for clean text, got %v", types)
	}
}

// --- Middleware integration test ---

func TestEEOCBiasMiddlewareSetsLocalsAndHeader(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan middleware.SIEMEvent, 8)

	var capturedDetected any
	var capturedTypes any

	app.Use(middleware.NewEEOCBiasMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("eeoc_bias_detected")
		capturedTypes = c.Locals("eeoc_bias_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"Job posting: seeking manpower with rockstar energy."}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected != true {
		t.Errorf("expected eeoc_bias_detected=true, got %v", capturedDetected)
	}
	types, ok := capturedTypes.([]middleware.EEOCBiasType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty eeoc_bias_types slice, got %v", capturedTypes)
	}

	headerVal := resp.Header.Get("X-EEOC-Bias-Signal")
	if headerVal == "" {
		t.Errorf("expected X-EEOC-Bias-Signal header to be set, got empty string")
	}
}

// --- helpers ---

func containsEEOC(types []middleware.EEOCBiasType, want middleware.EEOCBiasType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
