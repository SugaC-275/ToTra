package middleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

// CallbackConfig holds external observability sink configuration loaded from env vars.
type CallbackConfig struct {
	LangfusePublicKey string // LANGFUSE_PUBLIC_KEY
	LangfuseSecretKey string // LANGFUSE_SECRET_KEY
	LangfuseHost      string // LANGFUSE_HOST (default: https://cloud.langfuse.com)
	DatadogAPIKey     string // DATADOG_API_KEY
	DatadogSite       string // DATADOG_SITE (default: datadoghq.com)
	LangSmithAPIKey   string // LANGSMITH_API_KEY
	LangSmithProject  string // LANGSMITH_PROJECT (default: default)
	ArizeAPIKey       string // ARIZE_API_KEY
	ArizeSpaceKey     string // ARIZE_SPACE_KEY
}

func (c CallbackConfig) langfuseEnabled() bool {
	return c.LangfusePublicKey != "" && c.LangfuseSecretKey != ""
}

func (c CallbackConfig) datadogEnabled() bool {
	return c.DatadogAPIKey != ""
}

func (c CallbackConfig) langsmithEnabled() bool {
	return c.LangSmithAPIKey != ""
}

func (c CallbackConfig) arizeEnabled() bool {
	return c.ArizeAPIKey != "" && c.ArizeSpaceKey != ""
}

// LoadCallbackConfig reads callback configuration from environment variables.
func LoadCallbackConfig() CallbackConfig {
	host := os.Getenv("LANGFUSE_HOST")
	if host == "" {
		host = "https://cloud.langfuse.com"
	}
	site := os.Getenv("DATADOG_SITE")
	if site == "" {
		site = "datadoghq.com"
	}
	lsProject := os.Getenv("LANGSMITH_PROJECT")
	if lsProject == "" {
		lsProject = "default"
	}
	return CallbackConfig{
		LangfusePublicKey: os.Getenv("LANGFUSE_PUBLIC_KEY"),
		LangfuseSecretKey: os.Getenv("LANGFUSE_SECRET_KEY"),
		LangfuseHost:      host,
		DatadogAPIKey:     os.Getenv("DATADOG_API_KEY"),
		DatadogSite:       site,
		LangSmithAPIKey:   os.Getenv("LANGSMITH_API_KEY"),
		LangSmithProject:  lsProject,
		ArizeAPIKey:       os.Getenv("ARIZE_API_KEY"),
		ArizeSpaceKey:     os.Getenv("ARIZE_SPACE_KEY"),
	}
}

// callbackUsage parses the usage block from a chat completion response.
type callbackUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type callbackResponseEnvelope struct {
	Usage callbackUsage `json:"usage"`
}

// NewCallbackMiddleware sends post-request observability events to Langfuse and/or Datadog.
// All sink calls are non-blocking (async goroutines) — they never affect response latency.
func NewCallbackMiddleware(cfg CallbackConfig) fiber.Handler {
	if !cfg.langfuseEnabled() && !cfg.datadogEnabled() {
		return func(c *fiber.Ctx) error { return c.Next() }
	}

	return func(c *fiber.Ctx) error {
		start := time.Now()
		err := c.Next()

		user, _ := c.Locals("user").(*UserInfo)
		if user == nil {
			return err
		}

		var reqBody struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(c.Body(), &reqBody)

		var respEnv callbackResponseEnvelope
		_ = json.Unmarshal(c.Response().Body(), &respEnv)

		duration := time.Since(start)
		statusCode := c.Response().StatusCode()
		promptTokens := respEnv.Usage.PromptTokens
		completionTokens := respEnv.Usage.CompletionTokens

		if cfg.langfuseEnabled() {
			go sendLangfuseTrace(cfg, user.TenantID, user.UserID, reqBody.Model,
				promptTokens, completionTokens, duration, statusCode)
		}
		if cfg.datadogEnabled() {
			go sendDatadogMetrics(cfg, user.TenantID, reqBody.Model,
				promptTokens, completionTokens, duration, statusCode)
		}
		if cfg.langsmithEnabled() {
			go sendLangSmithRun(cfg, user.TenantID, user.UserID, reqBody.Model,
				promptTokens, completionTokens, duration, statusCode)
		}
		if cfg.arizeEnabled() {
			go sendArizePrediction(cfg, user.TenantID, user.UserID, reqBody.Model,
				promptTokens, completionTokens, duration, statusCode)
		}
		return err
	}
}

func sendLangfuseTrace(cfg CallbackConfig, tenantID, userID, model string,
	promptTokens, completionTokens int, duration time.Duration, statusCode int) {

	now := time.Now().UTC()
	traceID := fmt.Sprintf("totra-%s-%d", tenantID, now.UnixNano())

	payload := map[string]any{
		"batch": []map[string]any{
			{
				"id":        traceID,
				"type":      "trace-create",
				"timestamp": now.Format(time.RFC3339),
				"body": map[string]any{
					"id":        traceID,
					"name":      "llm_request",
					"userId":    userID,
					"metadata":  map[string]any{"tenant_id": tenantID},
					"input":     model,
					"startTime": now.Add(-duration).Format(time.RFC3339),
					"endTime":   now.Format(time.RFC3339),
					"usage": map[string]any{
						"input":  promptTokens,
						"output": completionTokens,
						"total":  promptTokens + completionTokens,
						"unit":   "TOKENS",
					},
					"statusMessage": fmt.Sprintf("HTTP %d", statusCode),
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		cfg.LangfuseHost+"/api/public/ingestion", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	creds := base64.StdEncoding.EncodeToString(
		[]byte(cfg.LangfusePublicKey + ":" + cfg.LangfuseSecretKey))
	req.Header.Set("Authorization", "Basic "+creds)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("langfuse: send failed", "err", err)
		return
	}
	resp.Body.Close()
}

type ddSeries struct {
	Series []ddMetric `json:"series"`
}

type ddMetric struct {
	Metric string    `json:"metric"`
	Points [][]int64 `json:"points"`
	Type   string    `json:"type"`
	Tags   []string  `json:"tags"`
}

func sendDatadogMetrics(cfg CallbackConfig, tenantID, model string,
	promptTokens, completionTokens int, duration time.Duration, statusCode int) {

	now := time.Now().Unix()
	tags := []string{
		"source:totra",
		fmt.Sprintf("tenant:%s", tenantID),
		fmt.Sprintf("model:%s", model),
		fmt.Sprintf("status:%d", statusCode),
	}

	series := ddSeries{
		Series: []ddMetric{
			{Metric: "totra.llm.prompt_tokens", Points: [][]int64{{now, int64(promptTokens)}}, Type: "gauge", Tags: tags},
			{Metric: "totra.llm.completion_tokens", Points: [][]int64{{now, int64(completionTokens)}}, Type: "gauge", Tags: tags},
			{Metric: "totra.llm.latency_ms", Points: [][]int64{{now, duration.Milliseconds()}}, Type: "gauge", Tags: tags},
		},
	}

	body, err := json.Marshal(series)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.%s/api/v1/series", cfg.DatadogSite)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("DD-API-KEY", cfg.DatadogAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("datadog: send failed", "err", err)
		return
	}
	resp.Body.Close()
}

func sendLangSmithRun(cfg CallbackConfig, tenantID, userID, model string,
	promptTokens, completionTokens int, duration time.Duration, statusCode int) {

	now := time.Now().UTC()
	runID := fmt.Sprintf("totra-%s-%d", tenantID, now.UnixNano())

	payload := map[string]any{
		"id":           runID,
		"name":         "llm_request",
		"run_type":     "llm",
		"start_time":   now.Add(-duration).Format(time.RFC3339Nano),
		"end_time":     now.Format(time.RFC3339Nano),
		"inputs":       map[string]any{"model": model},
		"outputs":      map[string]any{"status": statusCode},
		"extra":        map[string]any{"tenant_id": tenantID, "user_id": userID},
		"session_name": cfg.LangSmithProject,
		"token_usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.smith.langchain.com/runs", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.LangSmithAPIKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("langsmith: send failed", "err", err)
		return
	}
	resp.Body.Close()
}

func sendArizePrediction(cfg CallbackConfig, tenantID, userID, model string,
	promptTokens, completionTokens int, duration time.Duration, statusCode int) {

	now := time.Now().UTC()
	predID := fmt.Sprintf("totra-%s-%d", tenantID, now.UnixNano())

	payload := map[string]any{
		"prediction_id":    predID,
		"model_id":         model,
		"model_version":    "1",
		"prediction_label": fmt.Sprintf("%d", statusCode),
		"features": map[string]any{
			"tenant_id":         tenantID,
			"user_id":           userID,
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"latency_ms":        duration.Milliseconds(),
		},
		"timestamp": now.Unix(),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.arize.com/v1/log", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", cfg.ArizeAPIKey)
	req.Header.Set("space-key", cfg.ArizeSpaceKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("arize: send failed", "err", err)
		return
	}
	resp.Body.Close()
}
