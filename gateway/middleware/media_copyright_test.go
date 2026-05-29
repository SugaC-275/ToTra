package middleware_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
)

// --- ScanForCopyrightSignals unit tests ---

func TestScanForCopyright_CopyrightNotice(t *testing.T) {
	types := middleware.ScanForCopyrightSignals("© 2024 Warner Music Group, All Rights Reserved")
	if !containsCopyright(types, middleware.CopyrightNotice) {
		t.Errorf("expected copyright_notice, got %v", types)
	}
}

func TestScanForCopyright_LyricsReproduction(t *testing.T) {
	types := middleware.ScanForCopyrightSignals("write out the full lyrics to Bohemian Rhapsody")
	if !containsCopyright(types, middleware.CopyrightLyrics) {
		t.Errorf("expected lyrics_reproduction, got %v", types)
	}
}

func TestScanForCopyright_CodeLicense(t *testing.T) {
	types := middleware.ScanForCopyrightSignals("SPDX-License-Identifier: GPL-2.0")
	if !containsCopyright(types, middleware.CopyrightCode) {
		t.Errorf("expected code_license, got %v", types)
	}
}

func TestScanForCopyright_SyntheticMedia(t *testing.T) {
	types := middleware.ScanForCopyrightSignals("voice clone to sound like Taylor Swift")
	if !containsCopyright(types, middleware.SyntheticMedia) {
		t.Errorf("expected synthetic_media, got %v", types)
	}
}

func TestScanForCopyright_CleanText(t *testing.T) {
	// Discussing a work is not reproducing it
	types := middleware.ScanForCopyrightSignals("summarize the plot of Dune by Frank Herbert")
	if len(types) != 0 {
		t.Errorf("expected no copyright signals for clean text, got %v", types)
	}
}

// --- Middleware integration test ---

func TestCopyrightMiddlewareSetsHeaderAndLocals(t *testing.T) {
	app := fiber.New()
	siemChan := make(chan middleware.SIEMEvent, 8)

	var capturedSignal any
	var capturedTypes any

	app.Use(middleware.NewMediaCopyrightMiddleware(siemChan))
	app.Post("/chat", func(c *fiber.Ctx) error {
		capturedSignal = c.Locals("copyright_signal")
		capturedTypes = c.Locals("copyright_types")
		return c.SendStatus(fiber.StatusOK)
	})

	body := `{"messages":[{"role":"user","content":"© 2024 Acme Corp, All Rights Reserved. Please analyse this contract."}]}`
	req := httptest.NewRequest("POST", "/chat", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) //nolint:errcheck

	if capturedSignal != true {
		t.Errorf("expected copyright_signal=true, got %v", capturedSignal)
	}
	types, ok := capturedTypes.([]middleware.CopyrightSignalType)
	if !ok || len(types) == 0 {
		t.Errorf("expected non-empty copyright_types slice, got %v", capturedTypes)
	}

	headerVal := resp.Header.Get("X-Copyright-Signal")
	if headerVal != "true" {
		t.Errorf("expected X-Copyright-Signal: true header, got %q", headerVal)
	}
}

// --- helpers ---

func containsCopyright(types []middleware.CopyrightSignalType, want middleware.CopyrightSignalType) bool {
	for _, t := range types {
		if t == want {
			return true
		}
	}
	return false
}
