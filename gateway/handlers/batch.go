package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// BatchModelLookup is satisfied by *storage.PGModelLookup.
type BatchModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// createBatchPayload is the request body for POST /v1/batches.
type createBatchPayload struct {
	Model    string                     `json:"model"`
	Requests []storage.BatchRequestItem `json:"requests"`
	Metadata map[string]any             `json:"metadata"`
}

// RegisterBatchRoutes registers the /v1/batches routes on the given Fiber router.
func RegisterBatchRoutes(router fiber.Router, store *storage.BatchStore, lookup BatchModelLookup) {
	router.Post("/batches", createBatch(store, lookup))
	router.Get("/batches", listBatches(store))
	router.Get("/batches/:id", getBatch(store))
	router.Post("/batches/:id/cancel", cancelBatch(store))
	router.Get("/batches/:id/results", getBatchResults(store))
}

func createBatch(store *storage.BatchStore, lookup BatchModelLookup) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}

		var payload createBatchPayload
		if err := c.BodyParser(&payload); err != nil || payload.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model and requests are required", "type": "bad_request"},
			})
		}
		if len(payload.Requests) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "requests must not be empty", "type": "bad_request"},
			})
		}

		// Validate that the model is configured for this tenant.
		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, payload.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{
					"message": fmt.Sprintf("model %q not configured", payload.Model),
					"type":    "model_not_found",
				},
			})
		}

		job, err := store.Create(c.Context(), user.TenantID, user.UserID,
			payload.Model, payload.Requests, payload.Metadata)
		if err != nil {
			slog.Error("batch create", "tenant", user.TenantID, "err", err)
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error(), "type": "bad_request"},
			})
		}
		return c.Status(fiber.StatusCreated).JSON(job)
	}
}

func listBatches(store *storage.BatchStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		jobs, err := store.List(c.Context(), user.TenantID, limit, offset)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"data": jobs, "object": "list"})
	}
}

func getBatch(store *storage.BatchStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		job, err := store.Get(c.Context(), user.TenantID, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fiber.Map{"message": "batch not found"}})
		}
		return c.JSON(job)
	}
}

func cancelBatch(store *storage.BatchStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		if err := store.Cancel(c.Context(), user.TenantID, c.Params("id")); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": err.Error()},
			})
		}
		job, _ := store.Get(c.Context(), user.TenantID, c.Params("id"))
		return c.JSON(job)
	}
}

func getBatchResults(store *storage.BatchStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fiber.Map{"message": "unauthorized"}})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "100"))
		offset, _ := strconv.Atoi(c.Query("offset", "0"))
		results, err := store.Results(c.Context(), user.TenantID, c.Params("id"), limit, offset)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.JSON(fiber.Map{"data": results, "object": "list"})
	}
}

// BatchWorker processes pending batch jobs in the background.
type BatchWorker struct {
	store  *storage.BatchStore
	lookup BatchModelLookup
	client *http.Client
	tick   time.Duration
}

// NewBatchWorker creates a worker that polls for pending jobs every interval.
func NewBatchWorker(store *storage.BatchStore, lookup BatchModelLookup, interval time.Duration) *BatchWorker {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &BatchWorker{
		store:  store,
		lookup: lookup,
		client: &http.Client{Timeout: 120 * time.Second},
		tick:   interval,
	}
}

// Run starts the processing loop; blocks until ctx is cancelled.
func (w *BatchWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processOnce(ctx)
		}
	}
}

func (w *BatchWorker) processOnce(ctx context.Context) {
	jobs, err := w.store.ClaimPending(ctx, 5)
	if err != nil {
		slog.Error("batch worker: claim pending", "err", err)
		return
	}
	for _, job := range jobs {
		if err := w.processJob(ctx, &job); err != nil {
			slog.Error("batch worker: process job", "job_id", job.ID, "err", err)
		}
	}
}

func (w *BatchWorker) processJob(ctx context.Context, job *storage.BatchJob) error {
	reqs, err := w.store.PendingRequests(ctx, job.ID)
	if err != nil {
		return fmt.Errorf("pending requests: %w", err)
	}

	modelCfg, err := w.lookup.GetByName(ctx, job.TenantID, job.ModelName)
	if err != nil || modelCfg == nil {
		return fmt.Errorf("model %q not found for tenant %s", job.ModelName, job.TenantID)
	}

	endpoint := strings.TrimRight(modelCfg.BaseURL, "/") + "/chat/completions"

	for _, req := range reqs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		respBody, callErr := w.callUpstream(ctx, endpoint, modelCfg.APIKey, req.RequestBody)
		if callErr != nil {
			slog.Warn("batch worker: upstream call failed",
				"job", job.ID, "custom_id", req.CustomID, "err", callErr)
			_ = w.store.SaveResult(ctx, job.ID, req.ID, "failed", nil, callErr.Error())
		} else {
			_ = w.store.SaveResult(ctx, job.ID, req.ID, "completed", respBody, "")
		}
	}

	return w.store.FinalizeJob(ctx, job.ID)
}

func (w *BatchWorker) callUpstream(ctx context.Context, endpoint, apiKey string, bodyBytes []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	out, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(out, &errResp)
		msg := errResp.Error.Message
		if msg == "" {
			msg = fmt.Sprintf("upstream HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out, nil
}
