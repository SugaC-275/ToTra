package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestScanForFDAData_SubjectAndAdverseEvent(t *testing.T) {
	types := ScanForFDAData("Subject AB1-001234 adverse event reported in trial.")
	if !containsFDA(types, FDASubjectData) {
		t.Errorf("expected clinical_subject, got %v", types)
	}
	if !containsFDA(types, FDAAdverseEvent) {
		t.Errorf("expected adverse_event, got %v", types)
	}
}

func TestScanForFDAData_IND(t *testing.T) {
	types := ScanForFDAData("IND 123456 submission received by the FDA.")
	if !containsFDA(types, FDARegulatoryDoc) {
		t.Errorf("expected regulatory_doc, got %v", types)
	}
}

func TestScanForFDAData_BatchRecordGMP(t *testing.T) {
	types := ScanForFDAData("batch record GMP deviation found in line 3.")
	if !containsFDA(types, FDABatchRecord) {
		t.Errorf("expected batch_record, got %v", types)
	}
	if !containsFDA(types, FDAGxPProcess) {
		t.Errorf("expected gxp_process, got %v", types)
	}
}

func TestScanForFDAData_CleanText(t *testing.T) {
	types := ScanForFDAData("summarize quarterly earnings for the board meeting.")
	if len(types) != 0 {
		t.Errorf("expected no FDA types for clean text, got %v", types)
	}
}

func TestScanForFDAData_Deduplication(t *testing.T) {
	// Two adverse event patterns should produce exactly one adverse_event entry.
	text := "serious adverse event adverse event CIOMS form submitted."
	types := ScanForFDAData(text)
	count := 0
	for _, tp := range types {
		if tp == FDAAdverseEvent {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 adverse_event, got %d (types=%v)", count, types)
	}
}

func TestPharmaFDAMiddlewareSetsLocalsAndHeader(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan SIEMEvent, 8)

	var capturedDetected any
	var capturedTypes any

	app.Use(NewPharmaFDAMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("fda_data_detected")
		capturedTypes = c.Locals("fda_data_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"IND 654321 protocol deviation report GMP"}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected != true {
		t.Errorf("expected fda_data_detected=true, got %v", capturedDetected)
	}
	types, ok := capturedTypes.([]FDADataType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty fda_data_types slice, got %v", capturedTypes)
	}

	if h := resp.Header.Get("X-FDA-21CFR-Signal"); h != "true" {
		t.Errorf("expected X-FDA-21CFR-Signal: true, got %q", h)
	}
}

func TestPharmaFDAMiddlewareCleanPassThrough(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan SIEMEvent, 8)

	var capturedDetected any

	app.Use(NewPharmaFDAMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedDetected = c.Locals("fda_data_detected")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"tell me a joke"}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedDetected == true {
		t.Errorf("expected no fda detection for clean text")
	}
	if h := resp.Header.Get("X-FDA-21CFR-Signal"); h != "" {
		t.Errorf("expected no X-FDA-21CFR-Signal header for clean text, got %q", h)
	}
}

// containsFDA reports whether a given FDADataType is present in the slice.
func containsFDA(types []FDADataType, want FDADataType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
