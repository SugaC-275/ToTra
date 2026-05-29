package middleware_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- ScanForCOPPA unit tests ---

func TestScanForCOPPA_AgeMinorContext(t *testing.T) {
	signals := middleware.ScanForCOPPA("The student is age 10 years old and needs help.")
	if !containsCOPPA(signals, middleware.COPPAMinorContext) {
		t.Errorf("expected minor_context signal, got %v", signals)
	}
}

func TestScanForCOPPA_ParentalConsent(t *testing.T) {
	signals := middleware.ScanForCOPPA("We need parental consent before processing this request.")
	if !containsCOPPA(signals, middleware.COPPAMinorContext) {
		t.Errorf("expected minor_context signal for parental consent, got %v", signals)
	}
}

func TestScanForCOPPA_AdultBusinessText(t *testing.T) {
	signals := middleware.ScanForCOPPA("Our Q3 revenue forecast shows strong growth in enterprise contracts.")
	if len(signals) != 0 {
		t.Errorf("expected no COPPA signals for adult business text, got %v", signals)
	}
}

// --- RatingAllows tests ---

func TestRatingAllows_GRejectsRating_PG(t *testing.T) {
	if middleware.RatingAllows(middleware.RatingG, middleware.RatingPG) {
		t.Error("G-rated model should reject PG content")
	}
}

func TestRatingAllows_UnratedAllowsR(t *testing.T) {
	if !middleware.RatingAllows(middleware.RatingUnrated, middleware.RatingR) {
		t.Error("unrated model should allow R content")
	}
}

// --- Middleware integration test ---

func TestCOPPAMiddlewareSetsHeader(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan middleware.SIEMEvent, 8)

	app.Use(middleware.NewCOPPAMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"My child is age 8 years old. Can you recommend activities?"}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	headerVal := resp.Header.Get("X-COPPA-Signal")
	if headerVal != "minor_context" {
		t.Errorf("expected X-COPPA-Signal: minor_context header, got %q", headerVal)
	}
}

// --- helpers ---

func containsCOPPA(signals []middleware.COPPASignal, want middleware.COPPASignal) bool {
	for _, s := range signals {
		if s == want {
			return true
		}
	}
	return false
}
