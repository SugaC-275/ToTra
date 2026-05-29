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

// VectorStoreModelLookup is satisfied by *storage.PGModelLookup.
type VectorStoreModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// vectorStoreCreateResponse captures only the fields needed for local metadata
// storage from an upstream create response.
type vectorStoreCreateResponse struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	FileCounts map[string]int `json:"file_counts"`
	ExpiresAt  *int64         `json:"expires_at"` // unix timestamp or null
}

// RegisterVectorStoreRoutes registers all OpenAI Vector Stores API proxy routes
// onto router. Metadata is persisted locally; the upstream provider owns the
// actual vector store objects.
func RegisterVectorStoreRoutes(router fiber.Router, store *storage.VectorStoreStore, lookup VectorStoreModelLookup) {
	h := &vectorStoreHandler{store: store, lookup: lookup}

	// Vector stores collection
	router.Post("/vector_stores", h.proxy(true, false))
	router.Get("/vector_stores", h.proxy(false, false))

	// Single vector store
	router.Get("/vector_stores/:id", h.proxy(false, false))
	router.Post("/vector_stores/:id", h.proxy(false, false))
	router.Delete("/vector_stores/:id", h.proxy(false, true))

	// Files within a vector store
	router.Post("/vector_stores/:id/files", h.proxy(false, false))
	router.Get("/vector_stores/:id/files", h.proxy(false, false))
	router.Get("/vector_stores/:id/files/:file_id", h.proxy(false, false))
	router.Delete("/vector_stores/:id/files/:file_id", h.proxy(false, false))

	// File batches
	router.Post("/vector_stores/:id/file_batches", h.proxy(false, false))
	router.Get("/vector_stores/:id/file_batches/:batch_id", h.proxy(false, false))
	router.Post("/vector_stores/:id/file_batches/:batch_id/cancel", h.proxy(false, false))
}

type vectorStoreHandler struct {
	store  *storage.VectorStoreStore
	lookup VectorStoreModelLookup
}

// proxy returns a Fiber handler. When storeOnCreate is true the handler
// persists metadata from the upstream response into the local database.
// When deleteLocal is true it removes the local record after a successful
// upstream deletion.
func (h *vectorStoreHandler) proxy(storeOnCreate, deleteLocal bool) fiber.Handler {
	client := &http.Client{Timeout: 30 * time.Second}

	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": fiber.Map{"message": "unauthorized", "type": "auth_error"},
			})
		}

		// Resolve model name: X-Model header, then query param, then 400.
		modelName := c.Get("X-Model")
		if modelName == "" {
			modelName = c.Query("model")
		}
		if modelName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": "X-Model header or model query parameter is required",
					"type":    "invalid_request_error",
				},
			})
		}

		modelCfg, err := h.lookup.GetByName(c.Context(), user.TenantID, modelName)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured for your tenant", modelName),
					"type":    "model_not_found",
				},
			})
		}

		// Build upstream URL from the base (which already contains /v1) + the
		// path suffix after stripping the gateway's leading /v1.
		base := strings.TrimRight(modelCfg.BaseURL, "/")
		upstreamPath := c.Path()
		if strings.HasPrefix(upstreamPath, "/v1") {
			upstreamPath = upstreamPath[len("/v1"):]
		}
		upstreamURL := base + upstreamPath
		if qs := string(c.Request().URI().QueryString()); qs != "" {
			upstreamURL += "?" + qs
		}

		result, err := forwardVectorStore(c.Context(), client, c.Method(), upstreamURL, modelCfg.APIKey, c.Get("Content-Type"), c.Body())
		if err != nil {
			slog.Error("vector_stores upstream error",
				"tenant", user.TenantID,
				"model", modelName,
				"path", c.Path(),
				"err", err,
			)
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"error": "upstream unavailable"})
		}

		// Persist metadata locally after a successful create.
		if storeOnCreate && result.StatusCode >= 200 && result.StatusCode < 300 {
			var resp vectorStoreCreateResponse
			if jsonErr := json.Unmarshal(result.Body, &resp); jsonErr == nil && resp.ID != "" {
				var expiresAt *time.Time
				if resp.ExpiresAt != nil {
					t := time.Unix(*resp.ExpiresAt, 0).UTC()
					expiresAt = &t
				}
				status := resp.Status
				if status == "" {
					status = "in_progress"
				}
				_ = h.store.Create(c.Context(), &storage.VectorStoreRecord{
					ID:            resp.ID,
					TenantID:      user.TenantID,
					UserID:        user.UserID,
					ModelConfigID: modelCfg.ID,
					Name:          resp.Name,
					Status:        status,
					ExpiresAt:     expiresAt,
					FileCounts:    resp.FileCounts,
				})
			}
		}

		// Remove local metadata after a successful upstream deletion.
		if deleteLocal && result.StatusCode >= 200 && result.StatusCode < 300 {
			vsID := c.Params("id")
			if vsID != "" {
				_ = h.store.Delete(c.Context(), user.TenantID, vsID)
			}
		}

		for k, vs := range result.Header {
			for _, v := range vs {
				c.Set(k, v)
			}
		}
		return c.Status(result.StatusCode).Send(result.Body)
	}
}

// forwardVectorStore issues the HTTP request to the upstream and returns the
// raw response.
func forwardVectorStore(ctx context.Context, client *http.Client, method, url, apiKey, contentType string, body []byte) (*proxyResult, error) {
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("vector_stores: create request: %w", err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	} else if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("OpenAI-Beta", "assistants=v2")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vector_stores: do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vector_stores: read response: %w", err)
	}
	return &proxyResult{StatusCode: resp.StatusCode, Header: resp.Header, Body: respBody}, nil
}
