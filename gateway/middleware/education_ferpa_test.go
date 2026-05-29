package middleware_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- ScanForFERPA unit tests ---

func TestScanForFERPA_GPAPattern(t *testing.T) {
	types := middleware.ScanForFERPA("Her GPA: 3.8 this semester.")
	if !containsFERPA(types, middleware.FERPAGrades) {
		t.Errorf("expected grades type, got %v", types)
	}
}

func TestScanForFERPA_FAFSA(t *testing.T) {
	types := middleware.ScanForFERPA("Please process the FAFSA application for this student.")
	if !containsFERPA(types, middleware.FERPAFinancialAid) {
		t.Errorf("expected financial_aid type, got %v", types)
	}
}

func TestScanForFERPA_StudentID(t *testing.T) {
	types := middleware.ScanForFERPA("Your student ID is 12345678 per registration.")
	if !containsFERPA(types, middleware.FERPAStudentID) {
		t.Errorf("expected student_id type, got %v", types)
	}
}

func TestScanForFERPA_CleanText(t *testing.T) {
	types := middleware.ScanForFERPA("The weather is nice today. Please submit your assignment.")
	if len(types) != 0 {
		t.Errorf("expected no FERPA types for clean text, got %v", types)
	}
}

// --- Middleware integration test ---

func TestFERPAMiddlewareSetsLocals(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan middleware.SIEMEvent, 8)

	var capturedDetected any
	var capturedTypes any

	app.Use(middleware.NewFERPAMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("ferpa_detected")
		capturedTypes = c.Locals("ferpa_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"My GPA: 3.5. Can you help me improve it?"}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected != true {
		t.Errorf("expected ferpa_detected=true, got %v", capturedDetected)
	}
	types, ok := capturedTypes.([]middleware.FERPAType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty ferpa_types slice, got %v", capturedTypes)
	}

	headerVal := resp.Header.Get("X-FERPA-Detected")
	if headerVal != "true" {
		t.Errorf("expected X-FERPA-Detected: true header, got %q", headerVal)
	}
}

// --- helpers ---

func containsFERPA(types []middleware.FERPAType, want middleware.FERPAType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
