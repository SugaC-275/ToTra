package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

type AudioModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

type AudioUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

// NewAudioTranscriptionHandler returns a Fiber handler for POST /v1/audio/transcriptions.
// Proxies multipart/form-data to an OpenAI-compatible Whisper endpoint, or to
// ElevenLabs / Deepgram when the model config specifies those providers.
//
// After a successful transcription the response text is scanned for PII. If PII
// is found a non-blocking SIEM event is emitted (when siemChan != nil) and the
// response header X-PII-Detected is set. The transcription result is never
// blocked because the audio has already been processed.
func NewAudioTranscriptionHandler(
	lookup AudioModelLookup,
	usageRecorder AudioUsageRecorder,
	siemChan chan<- middleware.SIEMEvent,
) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		modelName := c.FormValue("model")
		if modelName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model field required", "type": "bad_request"},
			})
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured", modelName),
					"type":    "model_not_found",
				},
			})
		}

		provCfg, err := buildAudioProviderConfig(modelCfg.Provider, modelCfg.BaseURL, modelCfg.APIKey)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error(), "type": "unsupported_provider"},
			})
		}

		// Deepgram accepts model/language as query params rather than form fields.
		upstreamURL := provCfg.URL
		if modelCfg.Provider == "deepgram" {
			upstreamURL = appendDeepgramParams(upstreamURL, c.FormValue)
		}

		start := time.Now()
		result, err := forwardAudio(
			c.Context(),
			upstreamURL,
			provCfg.AuthKey,
			provCfg.AuthValue,
			string(c.Request().Header.ContentType()),
			c.Body(),
		)
		if err != nil {
			slog.Error("audio upstream error", "tenant", user.TenantID, "model", modelName, "err", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		responseMS := int(time.Since(start).Milliseconds())
		if usageRecorder != nil {
			usageRecorder.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				ResponseMS:    responseMS,
			})
		}

		// Normalise Deepgram response to OpenAI format before further processing.
		body := result.Body
		if modelCfg.Provider == "deepgram" && result.StatusCode == http.StatusOK {
			body = convertDeepgramToOpenAI(body)
		}

		// PII scan on the transcribed text. Transcription is never blocked — the
		// audio has already been processed — but we emit a SIEM event and set a
		// warning header so downstream consumers can act.
		if result.StatusCode == http.StatusOK {
			scanAudioResponseForPII(body, user, c.Path(), siemChan, c)
		}

		for k, vs := range result.Header {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(body)
	}
}

// scanAudioResponseForPII extracts the "text" field from an OpenAI-format
// transcription body, runs ScanForPII on it, and — when PII is detected —
// sets X-PII-Detected on the response and emits a non-blocking SIEM event.
func scanAudioResponseForPII(
	body []byte,
	user *middleware.UserInfo,
	path string,
	siemChan chan<- middleware.SIEMEvent,
	c *fiber.Ctx,
) {
	var resp openAITranscriptionResponse
	if err := json.Unmarshal(body, &resp); err != nil || resp.Text == "" {
		return
	}

	piiType, found := middleware.ScanForPII(resp.Text)
	if !found {
		return
	}

	c.Set("X-PII-Detected", piiType)
	slog.Warn("PII detected in audio transcription",
		"tenant", user.TenantID,
		"user", user.UserID,
		"pii_type", piiType,
		"path", path,
	)

	if siemChan == nil {
		return
	}
	event := middleware.SIEMEvent{
		TenantID:  user.TenantID,
		EventType: "pii_in_audio_transcription",
		Payload: map[string]any{
			"source":      "totra",
			"tenant_id":   user.TenantID,
			"event_type":  "pii_in_audio_transcription",
			"occurred_at": time.Now().UTC().Format(time.RFC3339),
			"detail": map[string]any{
				"user_id":  user.UserID,
				"pii_type": piiType,
				"action":   "detected",
				"path":     path,
			},
		},
	}
	select {
	case siemChan <- event:
	default: // drop if channel is full — never block the response path
	}
}

// forwardAudio POSTs body to url with the provided auth header and returns
// the raw upstream response.
func forwardAudio(
	ctx context.Context,
	url, authKey, authValue, contentType string,
	body []byte,
) (*proxyResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("audio: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	if authKey != "" && authValue != "" {
		httpReq.Header.Set(authKey, authValue)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("audio: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("audio: read response: %w", err)
	}
	return &proxyResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}
