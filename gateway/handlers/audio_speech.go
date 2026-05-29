package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// elevenLabsVoiceMap maps OpenAI voice names to ElevenLabs voice IDs.
var elevenLabsVoiceMap = map[string]string{
	"alloy":   "21m00Tcm4TlvDq8ikWAM",
	"echo":    "AZnzlk1XvdvUeBnXmlld",
	"fable":   "ErXwobaYiN019PkySvjV",
	"onyx":    "VR6AewLTigWG4xSOukaG",
	"nova":    "pFZP5JQG7iQjIQuC4Bku",
	"shimmer": "ThT5KcBeYPX3keUQqHPh",
}

// ttsRequest mirrors the OpenAI POST /v1/audio/speech request body.
type ttsRequest struct {
	Model          string  `json:"model"`
	Input          string  `json:"input"`
	Voice          string  `json:"voice"`
	ResponseFormat string  `json:"response_format,omitempty"`
	Speed          float64 `json:"speed,omitempty"`
}

// NewAudioSpeechHandler returns a Fiber handler for POST /v1/audio/speech.
// It proxies to OpenAI TTS, ElevenLabs TTS, or Deepgram TTS depending on
// the provider set in the model config.
func NewAudioSpeechHandler(lookup AudioModelLookup, usageStore AudioUsageRecorder) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		var req ttsRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "invalid request body", "type": "bad_request"},
			})
		}
		if req.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model field required", "type": "bad_request"},
			})
		}
		if req.Input == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "input field required", "type": "bad_request"},
			})
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, req.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured", req.Model),
					"type":    "model_not_found",
				},
			})
		}

		start := time.Now()
		audioBytes, contentType, err := forwardTTS(c.Context(), modelCfg, &req)
		if err != nil {
			slog.Error("tts upstream error", "tenant", user.TenantID, "model", req.Model, "err", err)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		responseMS := int(time.Since(start).Milliseconds())
		// TTS pricing is per character; approximate token count as chars/4.
		approxTokens := len(req.Input) / 4
		if approxTokens < 1 {
			approxTokens = 1
		}
		if usageStore != nil {
			usageStore.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				PromptTokens:  approxTokens,
				ResponseMS:    responseMS,
			})
		}

		c.Set("Content-Type", contentType)
		return c.Status(fiber.StatusOK).Send(audioBytes)
	}
}

// forwardTTS dispatches a TTS request to the appropriate provider and returns
// the raw audio bytes plus the upstream Content-Type header value.
func forwardTTS(ctx context.Context, modelCfg *storage.ModelConfig, req *ttsRequest) ([]byte, string, error) {
	switch modelCfg.Provider {
	case "elevenlabs":
		return forwardElevenLabsTTS(ctx, modelCfg.APIKey, req)
	case "deepgram":
		return forwardDeepgramTTS(ctx, modelCfg.APIKey, req)
	default:
		// openai, azure, local, or any OpenAI-compatible base URL
		return forwardOpenAITTS(ctx, modelCfg.BaseURL, modelCfg.APIKey, req)
	}
}

// forwardOpenAITTS forwards to {baseURL}/audio/speech with the original JSON body.
func forwardOpenAITTS(ctx context.Context, baseURL, apiKey string, req *ttsRequest) ([]byte, string, error) {
	url := strings.TrimRight(baseURL, "/") + "/audio/speech"
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, "", fmt.Errorf("tts openai: marshal body: %w", err)
	}
	return doTTSRequest(ctx, http.MethodPost, url, "Authorization", "Bearer "+apiKey, "application/json", bodyBytes)
}

// forwardElevenLabsTTS forwards to the ElevenLabs text-to-speech endpoint.
func forwardElevenLabsTTS(ctx context.Context, apiKey string, req *ttsRequest) ([]byte, string, error) {
	voiceID, ok := elevenLabsVoiceMap[req.Voice]
	if !ok {
		voiceID = elevenLabsVoiceMap["alloy"] // fallback to first voice
	}
	url := "https://api.elevenlabs.io/v1/text-to-speech/" + voiceID

	body := map[string]any{
		"text":     req.Input,
		"model_id": "eleven_monolingual_v1",
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("tts elevenlabs: marshal body: %w", err)
	}
	return doTTSRequest(ctx, http.MethodPost, url, "xi-api-key", apiKey, "application/json", bodyBytes)
}

// forwardDeepgramTTS forwards to the Deepgram TTS endpoint.
func forwardDeepgramTTS(ctx context.Context, apiKey string, req *ttsRequest) ([]byte, string, error) {
	const url = "https://api.deepgram.com/v1/speak?model=aura-asteria-en"
	body := map[string]string{"text": req.Input}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("tts deepgram: marshal body: %w", err)
	}
	return doTTSRequest(ctx, http.MethodPost, url, "Authorization", "Token "+apiKey, "application/json", bodyBytes)
}

// doTTSRequest executes a POST to the given TTS URL and returns the audio bytes
// together with the upstream Content-Type.
func doTTSRequest(ctx context.Context, method, url, authKey, authValue, contentType string, body []byte) ([]byte, string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("tts: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentType)
	if authKey != "" && authValue != "" {
		httpReq.Header.Set(authKey, authValue)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("tts: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("tts: upstream %d: %s", resp.StatusCode, string(errBody))
	}

	audioBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("tts: read response: %w", err)
	}

	upstreamCT := resp.Header.Get("Content-Type")
	if upstreamCT == "" {
		upstreamCT = "audio/mpeg"
	}
	return audioBytes, upstreamCT, nil
}
