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

// AssistantsModelLookup is satisfied by *storage.PGModelLookup.
type AssistantsModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// AssistantsUsageRecorder is satisfied by *storage.UsageStore.
type AssistantsUsageRecorder interface {
	Record(r *storage.UsageRecord)
}

const assistantsDefaultModel = "gpt-4o"

// assistantsRunRequest captures only the model field from a thread-run request body.
type assistantsRunRequest struct {
	Model string `json:"model"`
}

// RegisterAssistantsRoutes registers the OpenAI Assistants API proxy routes.
// All routes act as pure reverse proxies: auth is already enforced by the v1
// middleware group. The model config is resolved once per request to obtain the
// upstream BaseURL and APIKey.
func RegisterAssistantsRoutes(v1 fiber.Router, lookup AssistantsModelLookup, usageRecorder AssistantsUsageRecorder, sessionStore *storage.SessionStore, threadStore *storage.ThreadStore) {
	proxy := newAssistantsProxy(lookup, usageRecorder, sessionStore, threadStore)

	// Assistants collection
	v1.Get("/assistants", proxy)
	v1.Post("/assistants", proxy)

	// Single assistant
	v1.Get("/assistants/:id", proxy)
	v1.Post("/assistants/:id", proxy)
	v1.Delete("/assistants/:id", proxy)

	// Threads
	v1.Post("/threads", proxy)
	v1.Get("/threads/:thread_id", proxy)
	v1.Delete("/threads/:thread_id", proxy)

	// Messages in a thread
	v1.Post("/threads/:thread_id/messages", proxy)
	v1.Get("/threads/:thread_id/messages", proxy)

	// Runs in a thread
	v1.Post("/threads/:thread_id/runs", proxy)
	v1.Get("/threads/:thread_id/runs", proxy)
	v1.Get("/threads/:thread_id/runs/:run_id", proxy)
	v1.Post("/threads/:thread_id/runs/:run_id/cancel", proxy)
}

// isMessageCreatingEndpoint returns true for endpoints that submit user content
// and therefore require PII scanning before forwarding.
func isMessageCreatingEndpoint(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	p := strings.ToLower(path)
	return strings.Contains(p, "/messages") ||
		strings.Contains(p, "/runs") ||
		strings.HasSuffix(p, "/assistants") ||
		strings.Contains(p, "/threads")
}

// newAssistantsProxy returns a single Fiber handler that services all Assistants
// API routes. It resolves the model config from the request body when present,
// falling back to the default "gpt-4o" model.
func newAssistantsProxy(lookup AssistantsModelLookup, usageRecorder AssistantsUsageRecorder, sessionStore *storage.SessionStore, threadStore *storage.ThreadStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		// PII scan for message-creating endpoints.
		piiHit := false
		if isMessageCreatingEndpoint(c.Method(), c.Path()) && len(c.Body()) > 0 {
			if piiType, found := middleware.ScanForPII(string(c.Body())); found {
				piiHit = true
				// Block policy: any PII match returns 422. Log-only policies would
				// continue here — ToTra always blocks by default in the Assistants path.
				_ = piiType
				return c.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
					"error": fiber.Map{
						"message": "request blocked: potential PII detected (" + piiType + ")",
						"type":    "pii_blocked",
					},
				})
			}
		}

		// Extract model from body if present; default otherwise.
		modelName := assistantsDefaultModel
		if body := c.Body(); len(body) > 0 {
			var run assistantsRunRequest
			if err := json.Unmarshal(body, &run); err == nil && run.Model != "" {
				modelName = run.Model
			}
		}

		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured for your tenant", modelName),
					"type":    "model_not_found",
				},
			})
		}

		// Resolve or create session for cost attribution.
		sessionID := c.Get("X-ToTra-Session-Id")
		// If no explicit session, use thread_id from path as session key.
		if sessionID == "" {
			sessionID = c.Params("thread_id")
		}
		var resolvedSession *storage.Session
		if sessionStore != nil {
			resolvedSession, _ = sessionStore.GetOrCreate(c.Context(), user.TenantID, user.UserID, sessionID)
			if resolvedSession != nil {
				c.Set("X-ToTra-Session-Id", resolvedSession.ID)
			}
		}

		// Build upstream URL: BaseURL already includes /v1 from the model config.
		base := strings.TrimRight(modelCfg.BaseURL, "/")
		// Reconstruct path: c.Path() is /v1/... — strip the leading /v1 prefix so
		// we can append it to the base which already carries /v1.
		upstreamPath := c.Path()
		if strings.HasPrefix(upstreamPath, "/v1") {
			upstreamPath = upstreamPath[len("/v1"):]
		}
		// Preserve query string (e.g. ?limit=20&after=xyz).
		upstreamURL := base + upstreamPath
		if qs := string(c.Request().URI().QueryString()); qs != "" {
			upstreamURL += "?" + qs
		}

		start := time.Now()
		result, err := forwardAssistants(c.Context(), c.Method(), upstreamURL, modelCfg.APIKey, c.Body())
		if err != nil {
			slog.Error("assistants upstream error",
				"tenant", user.TenantID,
				"model", modelName,
				"path", c.Path(),
				"err", err,
			)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		// Bind newly created thread to tenant/session.
		if c.Method() == http.MethodPost && strings.HasSuffix(strings.TrimRight(c.Path(), "/"), "/threads") &&
			result.StatusCode == http.StatusOK && threadStore != nil {
			var threadResp struct {
				ID string `json:"id"`
			}
			if jsonErr := json.Unmarshal(result.Body, &threadResp); jsonErr == nil && threadResp.ID != "" {
				bindSessID := ""
				if resolvedSession != nil {
					bindSessID = resolvedSession.ID
				}
				go func(tid, tenantID, sid string) {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_ = threadStore.BindThread(ctx, tid, tenantID, sid)
				}(threadResp.ID, user.TenantID, bindSessID)
			}
		}

		responseMS := int(time.Since(start).Milliseconds())

		// Extract token usage from response for session stats.
		var totalTokens int64
		if len(result.Body) > 0 {
			var usageResp struct {
				Usage struct {
					TotalTokens int `json:"total_tokens"`
				} `json:"usage"`
			}
			if jsonErr := json.Unmarshal(result.Body, &usageResp); jsonErr == nil {
				totalTokens = int64(usageResp.Usage.TotalTokens)
			}
		}

		if usageRecorder != nil {
			usageRecorder.Record(&storage.UsageRecord{
				TenantID:      user.TenantID,
				UserID:        user.UserID,
				ModelConfigID: modelCfg.ID,
				ResponseMS:    responseMS,
			})
		}

		// Update session stats async (non-blocking).
		if sessionStore != nil && resolvedSession != nil {
			sessionStore.UpdateStatsAsync(resolvedSession.ID, totalTokens, 0, piiHit)
		}

		for k, vs := range result.Header {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}

// forwardAssistants issues the given HTTP method + body to the upstream URL and
// returns the raw response.
func forwardAssistants(ctx context.Context, method, url, apiKey string, body []byte) (*proxyResult, error) {
	var bodyReader *bytes.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("assistants: create request: %w", err)
	}

	if len(body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	httpReq.Header.Set("OpenAI-Beta", "assistants=v2")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("assistants: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("assistants: read response: %w", err)
	}
	return &proxyResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}
