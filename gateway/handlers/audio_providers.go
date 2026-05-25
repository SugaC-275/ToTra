package handlers

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// audioProviderConfig holds the resolved upstream URL and the auth header
// key/value pair to use for a given provider.
type audioProviderConfig struct {
	URL       string
	AuthKey   string
	AuthValue string
}

// buildAudioProviderConfig returns the upstream endpoint and authentication
// header for the given provider. It replaces the old buildAudioURL helper so
// that provider-specific auth (xi-api-key, Token) is handled here rather than
// in forwardAudio.
func buildAudioProviderConfig(provider, baseURL string, apiKey string) (audioProviderConfig, error) {
	base := strings.TrimRight(baseURL, "/")
	switch provider {
	case "openai", "azure", "local", "":
		return audioProviderConfig{
			URL:       base + "/audio/transcriptions",
			AuthKey:   "Authorization",
			AuthValue: "Bearer " + apiKey,
		}, nil

	case "elevenlabs":
		return audioProviderConfig{
			URL:       base + "/v1/speech-to-text",
			AuthKey:   "xi-api-key",
			AuthValue: apiKey,
		}, nil

	case "deepgram":
		return audioProviderConfig{
			URL:       base + "/v1/listen",
			AuthKey:   "Authorization",
			AuthValue: "Token " + apiKey,
		}, nil

	case "anthropic", "gemini", "bedrock":
		return audioProviderConfig{}, fmt.Errorf("provider %q does not support audio transcription", provider)

	default:
		return audioProviderConfig{
			URL:       base + "/audio/transcriptions",
			AuthKey:   "Authorization",
			AuthValue: "Bearer " + apiKey,
		}, nil
	}
}

// deepgramQueryParams lists the form fields that should be forwarded as Deepgram
// query parameters when the provider is "deepgram".
var deepgramQueryParams = []string{
	"model", "language", "punctuate", "diarize", "smart_format",
	"filler_words", "utterances", "tier",
}

// appendDeepgramParams appends recognised Deepgram query parameters taken from
// form fields to the target URL, returning the modified URL string.
// The formValue signature matches fiber.Ctx.FormValue (variadic default).
func appendDeepgramParams(rawURL string, formValue func(string, ...string) string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	for _, param := range deepgramQueryParams {
		if v := formValue(param); v != "" {
			q.Set(param, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// deepgramResponse is the top-level shape returned by the Deepgram v1/listen
// endpoint.
type deepgramResponse struct {
	Results struct {
		Channels []struct {
			Alternatives []struct {
				Transcript string `json:"transcript"`
			} `json:"alternatives"`
		} `json:"channels"`
	} `json:"results"`
}

// openAITranscriptionResponse is the minimal OpenAI-compatible transcription
// shape: {"text": "..."}.
type openAITranscriptionResponse struct {
	Text string `json:"text"`
}

// convertDeepgramToOpenAI converts a raw Deepgram response body to the OpenAI
// transcription format {"text": "..."}. If the body cannot be parsed as a
// Deepgram response, the original body is returned unchanged.
func convertDeepgramToOpenAI(body []byte) []byte {
	var dg deepgramResponse
	if err := json.Unmarshal(body, &dg); err != nil {
		return body
	}
	if len(dg.Results.Channels) == 0 || len(dg.Results.Channels[0].Alternatives) == 0 {
		return body
	}
	transcript := dg.Results.Channels[0].Alternatives[0].Transcript
	out, err := json.Marshal(openAITranscriptionResponse{Text: transcript})
	if err != nil {
		return body
	}
	return out
}
