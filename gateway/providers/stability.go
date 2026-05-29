package providers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// ErrNotSupported is returned by providers that do not support a particular
// operation (e.g. streaming for image generation).
var ErrNotSupported = errors.New("operation not supported by this provider")

// stabilityRequest captures the OpenAI-format image generation request fields
// that map to Stability AI's multipart form.
type stabilityRequest struct {
	Prompt         string `json:"prompt"`
	NegativePrompt string `json:"negative_prompt"`
	Size           string `json:"size"`    // e.g. "1024x1024" → aspect_ratio "1:1"
	OutputFormat   string `json:"output_format"` // png/jpeg/webp; default png
}

// stabilityResponse wraps the OpenAI images response format.
type stabilityResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON string `json:"b64_json"`
	} `json:"data"`
}

// StabilityAdapter forwards image generation requests to Stability AI's
// v2beta REST API. It translates OpenAI-format JSON bodies to multipart/form-data.
type StabilityAdapter struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewStabilityAdapter(baseURL, apiKey string) *StabilityAdapter {
	return &StabilityAdapter{
		baseURL: baseURL,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// Forward translates an OpenAI image request body to Stability AI multipart
// form and returns an OpenAI-compatible images response with b64_json data.
func (a *StabilityAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var req stabilityRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, fmt.Errorf("stability: parse request: %w", err)
	}

	formBody, contentType, err := buildStabilityForm(req)
	if err != nil {
		return nil, nil, fmt.Errorf("stability: build form: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.baseURL+"/v2beta/stable-image/generate/core",
		formBody,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("stability: create request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.apiKey)
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Accept", "image/*")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("stability: forward: %w", err)
	}
	defer resp.Body.Close()

	imgData, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("stability: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return &ForwardResult{
			StatusCode: resp.StatusCode,
			Headers:    resp.Header,
			Body:       imgData,
		}, &Usage{}, nil
	}

	// Wrap binary image in OpenAI images response format.
	out := stabilityResponse{
		Created: time.Now().Unix(),
		Data: []struct {
			B64JSON string `json:"b64_json"`
		}{
			{B64JSON: base64.StdEncoding.EncodeToString(imgData)},
		},
	}
	outBody, err := json.Marshal(out)
	if err != nil {
		return nil, nil, fmt.Errorf("stability: encode response: %w", err)
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	return &ForwardResult{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       outBody,
	}, &Usage{PromptTokens: 1}, nil
}

// ForwardStream is not applicable for image generation.
func (a *StabilityAdapter) ForwardStream(_ context.Context, _ []byte, _ func([]byte) error) error {
	return ErrNotSupported
}

// BuildFilePrompt returns an empty body; Stability AI is image-only.
func (a *StabilityAdapter) BuildFilePrompt(_, _, _ string) []byte { return []byte("{}") }

// buildStabilityForm creates a multipart/form-data body from the OpenAI request.
func buildStabilityForm(req stabilityRequest) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if err := w.WriteField("prompt", req.Prompt); err != nil {
		return nil, "", err
	}
	if req.NegativePrompt != "" {
		if err := w.WriteField("negative_prompt", req.NegativePrompt); err != nil {
			return nil, "", err
		}
	}

	ar := sizeToAspectRatio(req.Size)
	if err := w.WriteField("aspect_ratio", ar); err != nil {
		return nil, "", err
	}

	outFmt := req.OutputFormat
	if outFmt == "" {
		outFmt = "png"
	}
	if err := w.WriteField("output_format", outFmt); err != nil {
		return nil, "", err
	}

	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return &buf, w.FormDataContentType(), nil
}

// sizeToAspectRatio converts OpenAI size strings (WxH) to Stability aspect_ratio.
func sizeToAspectRatio(size string) string {
	switch size {
	case "1792x1024":
		return "7:4"
	case "1024x1792":
		return "4:7"
	case "1280x720":
		return "16:9"
	case "720x1280":
		return "9:16"
	case "1024x1024", "":
		return "1:1"
	default:
		return "1:1"
	}
}

func init() {
	Register("stability", func(baseURL, apiKey string) Adapter {
		if baseURL == "" {
			baseURL = "https://api.stability.ai"
		}
		return NewStabilityAdapter(baseURL, apiKey)
	})
}
