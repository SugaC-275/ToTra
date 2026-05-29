package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	runwayBaseURL    = "https://api.dev.runwayml.com"
	runwayAPIVersion = "2024-11-06"
	runwayPollMax    = 5 * time.Minute
	runwayPollInterval = 5 * time.Second
)

// runwayRequest captures the OpenAI-style video generation request fields.
type runwayRequest struct {
	Model    string `json:"model"`
	Prompt   string `json:"prompt"`
	Duration int    `json:"duration"` // seconds; default 5
	Ratio    string `json:"ratio"`    // WIDTHxHEIGHT or WIDTH:HEIGHT
}

// runwaySubmitBody is the Runway Gen-3 task creation payload.
type runwaySubmitBody struct {
	Model      string `json:"model"`
	PromptText string `json:"promptText"`
	Duration   int    `json:"duration"`
	Ratio      string `json:"ratio"`
}

// runwayTaskResponse is the polling response from GET /v1/tasks/{id}.
type runwayTaskResponse struct {
	ID     string   `json:"id"`
	Status string   `json:"status"` // PENDING, RUNNING, SUCCEEDED, FAILED
	Output []string `json:"output"` // video URLs when SUCCEEDED
	Error  string   `json:"error"`
}

// RunwayAdapter sends video generation requests to Runway ML Gen-3 and polls
// until the task completes, returning an OpenAI-compatible response.
type RunwayAdapter struct {
	apiKey string
	client *http.Client
}

func NewRunwayAdapter(apiKey string) *RunwayAdapter {
	return &RunwayAdapter{
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Forward submits a video generation task to Runway and blocks until the task
// completes or the context is cancelled. Returns an OpenAI-style response.
func (a *RunwayAdapter) Forward(ctx context.Context, body []byte) (*ForwardResult, *Usage, error) {
	var req runwayRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, nil, fmt.Errorf("runway: parse request: %w", err)
	}

	model := req.Model
	if model == "" {
		model = "gen3a_turbo"
	}
	duration := req.Duration
	if duration <= 0 {
		duration = 5
	}
	ratio := normaliseRunwayRatio(req.Ratio)

	taskID, err := a.submitTask(ctx, model, req.Prompt, duration, ratio)
	if err != nil {
		return nil, nil, fmt.Errorf("runway: submit task: %w", err)
	}

	task, err := a.pollTask(ctx, taskID)
	if err != nil {
		return nil, nil, fmt.Errorf("runway: poll task %s: %w", taskID, err)
	}

	if task.Status != "SUCCEEDED" {
		return nil, nil, fmt.Errorf("runway: task %s ended with status %q: %s", taskID, task.Status, task.Error)
	}

	type videoItem struct {
		URL string `json:"url"`
	}
	type videoResponse struct {
		ID     string      `json:"id"`
		Object string      `json:"object"`
		Data   []videoItem `json:"data"`
	}

	items := make([]videoItem, 0, len(task.Output))
	for _, u := range task.Output {
		items = append(items, videoItem{URL: u})
	}
	outResp := videoResponse{ID: taskID, Object: "video.generation", Data: items}
	outBody, err := json.Marshal(outResp)
	if err != nil {
		return nil, nil, fmt.Errorf("runway: encode response: %w", err)
	}

	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	return &ForwardResult{
		StatusCode: http.StatusOK,
		Headers:    headers,
		Body:       outBody,
	}, &Usage{PromptTokens: 1}, nil
}

// ForwardStream is not applicable for asynchronous video generation.
func (a *RunwayAdapter) ForwardStream(_ context.Context, _ []byte, _ func([]byte) error) error {
	return ErrNotSupported
}

// BuildFilePrompt returns an empty body; Runway is video-only.
func (a *RunwayAdapter) BuildFilePrompt(_, _, _ string) []byte { return []byte("{}") }

func (a *RunwayAdapter) submitTask(ctx context.Context, model, prompt string, duration int, ratio string) (string, error) {
	payload := runwaySubmitBody{
		Model:      model,
		PromptText: prompt,
		Duration:   duration,
		Ratio:      ratio,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, runwayBaseURL+"/v1/text_to_video", bytes.NewReader(payloadBytes))
	if err != nil {
		return "", err
	}
	a.setRunwayHeaders(req)

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("upstream %d: %s", resp.StatusCode, respBody)
	}

	var task runwayTaskResponse
	if err := json.Unmarshal(respBody, &task); err != nil {
		return "", fmt.Errorf("parse task: %w", err)
	}
	return task.ID, nil
}

func (a *RunwayAdapter) pollTask(ctx context.Context, taskID string) (*runwayTaskResponse, error) {
	deadline := time.Now().Add(runwayPollMax)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out after %s", runwayPollMax)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, runwayBaseURL+"/v1/tasks/"+taskID, nil)
		if err != nil {
			return nil, err
		}
		a.setRunwayHeaders(req)

		resp, err := a.client.Do(req)
		if err != nil {
			return nil, err
		}
		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read poll body: %w", err)
		}

		var task runwayTaskResponse
		if err := json.Unmarshal(respBody, &task); err != nil {
			return nil, fmt.Errorf("parse poll response: %w", err)
		}

		switch task.Status {
		case "SUCCEEDED", "FAILED":
			return &task, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(runwayPollInterval):
		}
	}
}

func (a *RunwayAdapter) setRunwayHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-Runway-Version", runwayAPIVersion)
	req.Header.Set("Content-Type", "application/json")
}

// normaliseRunwayRatio converts "WIDTHxHEIGHT" to "WIDTH:HEIGHT".
func normaliseRunwayRatio(r string) string {
	if r == "" {
		return "1280:720"
	}
	for i, ch := range r {
		if ch == 'x' || ch == 'X' {
			return r[:i] + ":" + r[i+1:]
		}
	}
	return r
}

func init() {
	Register("runway", func(_, apiKey string) Adapter {
		return NewRunwayAdapter(apiKey)
	})
}
