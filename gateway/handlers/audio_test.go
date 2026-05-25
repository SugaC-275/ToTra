package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/handlers"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// --- fakes ---

type fakeAudioLookup struct {
	cfg *storage.ModelConfig
	err error
}

func (f *fakeAudioLookup) GetByName(_ context.Context, _, _ string) (*storage.ModelConfig, error) {
	return f.cfg, f.err
}

type fakeAudioUsageRecorder struct{}

func (f *fakeAudioUsageRecorder) Record(_ *storage.UsageRecord) {}

// buildAudioApp wires up a minimal Fiber app pointing to the given upstream server.
func buildAudioApp(
	lookup handlers.AudioModelLookup,
	rec handlers.AudioUsageRecorder,
	user *middleware.UserInfo,
	siemChan chan<- middleware.SIEMEvent,
) *fiber.App {
	app := fiber.New()
	app.Post("/v1/audio/transcriptions", func(c *fiber.Ctx) error {
		c.Locals("user", user)
		return c.Next()
	}, handlers.NewAudioTranscriptionHandler(lookup, rec, siemChan))
	return app
}

// multipartBody creates a minimal multipart/form-data body with a "model" field
// and a dummy "file" field.
func multipartBody(t *testing.T, fields map[string]string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatalf("write field %q: %v", k, err)
		}
	}
	// Dummy file part required by most transcription endpoints.
	fw, err := w.CreateFormFile("file", "audio.mp3")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	fw.Write([]byte("dummy audio content"))
	w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

// --- convertDeepgramToOpenAI tests ---

// convertDeepgramToOpenAI is an internal function; we test it by posting to a
// fake Deepgram-shaped upstream and confirming the response body is OpenAI-format.

func TestConvertDeepgramToOpenAI_ValidResponse(t *testing.T) {
	deepgramJSON := `{
		"results": {
			"channels": [{
				"alternatives": [{
					"transcript": "Hello world this is a test"
				}]
			}]
		}
	}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, deepgramJSON)
	}))
	defer upstream.Close()

	lookup := &fakeAudioLookup{
		cfg: &storage.ModelConfig{
			ID:       "cfg-1",
			Provider: "deepgram",
			APIKey:   "test-key",
			BaseURL:  upstream.URL,
		},
	}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}
	app := buildAudioApp(lookup, &fakeAudioUsageRecorder{}, user, nil)

	body, ct := multipartBody(t, map[string]string{"model": "nova-2"})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got struct{ Text string `json:"text"` }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := "Hello world this is a test"
	if got.Text != want {
		t.Errorf("text = %q, want %q", got.Text, want)
	}
}

func TestConvertDeepgramToOpenAI_EmptyChannels(t *testing.T) {
	deepgramJSON := `{"results": {"channels": []}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, deepgramJSON)
	}))
	defer upstream.Close()

	lookup := &fakeAudioLookup{
		cfg: &storage.ModelConfig{
			Provider: "deepgram",
			APIKey:   "k",
			BaseURL:  upstream.URL,
		},
	}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}
	app := buildAudioApp(lookup, &fakeAudioUsageRecorder{}, user, nil)

	body, ct := multipartBody(t, map[string]string{"model": "nova-2"})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	// When conversion falls back the original (unparseable as OpenAI) body is
	// returned, so we just verify it parses without the text field populated.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(rawBody), "channels") {
		t.Errorf("expected original body with 'channels', got %s", rawBody)
	}
}

// --- PII scanning tests ---

func TestAudioTranscription_PIIDetected_SetsHeader(t *testing.T) {
	// SSN embedded in the transcript.
	openaiResp := `{"text": "My SSN is 123-45-6789 and I live in New York"}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, openaiResp)
	}))
	defer upstream.Close()

	lookup := &fakeAudioLookup{
		cfg: &storage.ModelConfig{
			Provider: "openai",
			APIKey:   "k",
			BaseURL:  upstream.URL,
		},
	}
	siemCh := make(chan middleware.SIEMEvent, 10)
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}
	app := buildAudioApp(lookup, &fakeAudioUsageRecorder{}, user, siemCh)

	body, ct := multipartBody(t, map[string]string{"model": "whisper-1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	piiHeader := resp.Header.Get("X-Pii-Detected")
	if piiHeader == "" {
		t.Error("expected X-PII-Detected header to be set, got empty")
	}

	// Confirm a SIEM event was emitted.
	select {
	case evt := <-siemCh:
		if evt.EventType != "pii_in_audio_transcription" {
			t.Errorf("SIEM event type = %q, want pii_in_audio_transcription", evt.EventType)
		}
		if evt.TenantID != "t1" {
			t.Errorf("SIEM event tenant = %q, want t1", evt.TenantID)
		}
	default:
		t.Error("expected a SIEM event but channel was empty")
	}
}

func TestAudioTranscription_NoPII_NoHeader(t *testing.T) {
	openaiResp := `{"text": "The weather is nice today in London"}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, openaiResp)
	}))
	defer upstream.Close()

	lookup := &fakeAudioLookup{
		cfg: &storage.ModelConfig{
			Provider: "openai",
			APIKey:   "k",
			BaseURL:  upstream.URL,
		},
	}
	siemCh := make(chan middleware.SIEMEvent, 10)
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}
	app := buildAudioApp(lookup, &fakeAudioUsageRecorder{}, user, siemCh)

	body, ct := multipartBody(t, map[string]string{"model": "whisper-1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if h := resp.Header.Get("X-Pii-Detected"); h != "" {
		t.Errorf("expected no X-PII-Detected header, got %q", h)
	}
	if len(siemCh) != 0 {
		t.Errorf("expected no SIEM events, got %d", len(siemCh))
	}
}

// --- Provider URL/auth tests ---

func TestElevenLabsProviderConfig(t *testing.T) {
	openaiResp := `{"text": "hello"}`
	var capturedAuthKey, capturedAuthValue string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthKey = "xi-api-key"
		capturedAuthValue = r.Header.Get("xi-api-key")
		if !strings.HasSuffix(r.URL.Path, "/v1/speech-to-text") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, openaiResp)
	}))
	defer upstream.Close()

	lookup := &fakeAudioLookup{
		cfg: &storage.ModelConfig{
			Provider: "elevenlabs",
			APIKey:   "el-secret",
			BaseURL:  upstream.URL,
		},
	}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}
	app := buildAudioApp(lookup, &fakeAudioUsageRecorder{}, user, nil)

	body, ct := multipartBody(t, map[string]string{"model": "scribe_v1"})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	_ = capturedAuthKey
	if capturedAuthValue != "el-secret" {
		t.Errorf("xi-api-key = %q, want el-secret", capturedAuthValue)
	}
}

func TestDeepgramProviderConfig_AuthToken(t *testing.T) {
	openaiResp := `{
		"results": {
			"channels": [{"alternatives": [{"transcript": "hello deepgram"}]}]
		}
	}`
	var capturedAuth string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, openaiResp)
	}))
	defer upstream.Close()

	lookup := &fakeAudioLookup{
		cfg: &storage.ModelConfig{
			Provider: "deepgram",
			APIKey:   "dg-token",
			BaseURL:  upstream.URL,
		},
	}
	user := &middleware.UserInfo{TenantID: "t1", UserID: "u1"}
	app := buildAudioApp(lookup, &fakeAudioUsageRecorder{}, user, nil)

	body, ct := multipartBody(t, map[string]string{"model": "nova-2", "language": "en"})
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)

	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if capturedAuth != "Token dg-token" {
		t.Errorf("Authorization = %q, want Token dg-token", capturedAuth)
	}

	// Confirm response was converted to OpenAI format.
	var got struct{ Text string `json:"text"` }
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Text != "hello deepgram" {
		t.Errorf("text = %q, want hello deepgram", got.Text)
	}
}
