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
	"github.com/google/uuid"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

type FineTuningModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

type createFTPayload struct {
	Model          string         `json:"model"`
	TrainingFile   string         `json:"training_file"`
	ValidationFile string         `json:"validation_file,omitempty"`
	Hyperparameters map[string]any `json:"hyperparameters,omitempty"`
}

func RegisterFineTuningRoutes(router fiber.Router, store *storage.FineTuningStore, lookup FineTuningModelLookup) {
	router.Post("/fine_tuning/jobs", createFineTuningJob(store, lookup))
	router.Get("/fine_tuning/jobs", listFineTuningJobs(store))
	router.Get("/fine_tuning/jobs/:id", getFineTuningJob(store))
	router.Post("/fine_tuning/jobs/:id/cancel", cancelFineTuningJob(store))
	router.Get("/fine_tuning/jobs/:id/events", listFineTuningJobEvents(store))
}

func createFineTuningJob(store *storage.FineTuningStore, lookup FineTuningModelLookup) fiber.Handler {
	client := &http.Client{Timeout: 30 * time.Second}
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		var payload createFTPayload
		if err := c.BodyParser(&payload); err != nil || payload.Model == "" || payload.TrainingFile == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model and training_file are required"},
			})
		}
		modelCfg, err := lookup.GetByName(c.Context(), user.TenantID, payload.Model)
		if err != nil || modelCfg == nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": fmt.Sprintf("model %q not configured", payload.Model)},
			})
		}

		jobID := "ftjob-" + uuid.New().String()
		job := &storage.FineTuningJob{
			ID:             jobID,
			TenantID:       user.TenantID,
			UserID:         user.UserID,
			ModelConfigID:  modelCfg.ID,
			BaseModel:      payload.Model,
			TrainingFile:   payload.TrainingFile,
			ValidationFile: payload.ValidationFile,
		}
		if err := store.Create(c.Context(), job); err != nil {
			slog.Error("fine_tuning create", "err", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}

		go func() {
			body, _ := json.Marshal(payload)
			endpoint := strings.TrimRight(modelCfg.BaseURL, "/") + "/fine_tuning/jobs"
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, bytes.NewReader(body))
			if err != nil {
				_ = store.UpdateStatus(context.Background(), jobID, "failed", "", err.Error())
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if modelCfg.APIKey != "" {
				req.Header.Set("Authorization", "Bearer "+modelCfg.APIKey)
			}
			resp, err := client.Do(req)
			if err != nil {
				_ = store.UpdateStatus(context.Background(), jobID, "failed", "", err.Error())
				return
			}
			defer resp.Body.Close()
			out, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 400 {
				var errResp struct {
					Error struct{ Message string `json:"message"` } `json:"error"`
				}
				_ = json.Unmarshal(out, &errResp)
				_ = store.UpdateStatus(context.Background(), jobID, "failed", "", errResp.Error.Message)
				return
			}
			var upstream struct {
				Status string `json:"status"`
			}
			_ = json.Unmarshal(out, &upstream)
			status := upstream.Status
			if status == "" {
				status = "validating_files"
			}
			_ = store.UpdateStatus(context.Background(), jobID, status, "", "")
			_ = store.AddEvent(context.Background(), jobID, "info", "job submitted to upstream provider")
		}()

		job.Status = "queued"
		return c.Status(fiber.StatusCreated).JSON(job)
	}
}

func listFineTuningJobs(store *storage.FineTuningStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		limit, _ := strconv.Atoi(c.Query("limit", "20"))
		jobs, err := store.List(c.Context(), user.TenantID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if jobs == nil {
			jobs = []*storage.FineTuningJob{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": jobs})
	}
}

func getFineTuningJob(store *storage.FineTuningStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		job, err := store.Get(c.Context(), user.TenantID, c.Params("id"))
		if err != nil || job == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "job not found"})
		}
		return c.JSON(job)
	}
}

func cancelFineTuningJob(store *storage.FineTuningStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		if err := store.Cancel(c.Context(), user.TenantID, c.Params("id")); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		job, _ := store.Get(c.Context(), user.TenantID, c.Params("id"))
		return c.JSON(job)
	}
}

func listFineTuningJobEvents(store *storage.FineTuningStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		events, err := store.ListEvents(c.Context(), user.TenantID, c.Params("id"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if events == nil {
			events = []*storage.FineTuningEvent{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": events})
	}
}

// FineTuningPoller polls running jobs every 60s and syncs status from upstream.
type FineTuningPoller struct {
	store      *storage.FineTuningStore
	lookup     FineTuningModelLookup
	dispatcher *WebhookDispatcher
	client     *http.Client
	tick       time.Duration
}

func NewFineTuningPoller(store *storage.FineTuningStore, lookup FineTuningModelLookup, dispatcher *WebhookDispatcher) *FineTuningPoller {
	return &FineTuningPoller{
		store:      store,
		lookup:     lookup,
		dispatcher: dispatcher,
		client:     &http.Client{Timeout: 15 * time.Second},
		tick:       60 * time.Second,
	}
}

func (p *FineTuningPoller) Run(ctx context.Context) {
	ticker := time.NewTicker(p.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *FineTuningPoller) poll(ctx context.Context) {
	jobs, err := p.store.ListRunning(ctx)
	if err != nil {
		slog.Error("ft poller: list running", "err", err)
		return
	}
	for _, job := range jobs {
		p.syncJob(ctx, job)
	}
}

func (p *FineTuningPoller) syncJob(ctx context.Context, job *storage.FineTuningJob) {
	modelCfg, err := p.lookup.GetByName(ctx, job.TenantID, job.BaseModel)
	if err != nil || modelCfg == nil {
		return
	}
	endpoint := strings.TrimRight(modelCfg.BaseURL, "/") + "/fine_tuning/jobs/" + job.ID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return
	}
	if modelCfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+modelCfg.APIKey)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		slog.Warn("ft poller: upstream get", "job", job.ID, "err", err)
		return
	}
	defer resp.Body.Close()
	var upstream struct {
		Status         string `json:"status"`
		FineTunedModel string `json:"fine_tuned_model"`
		Error          *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return
	}
	errMsg := ""
	if upstream.Error != nil {
		errMsg = upstream.Error.Message
	}

	prevStatus := job.Status
	_ = p.store.UpdateStatus(ctx, job.ID, upstream.Status, upstream.FineTunedModel, errMsg)
	_ = p.store.AddEvent(ctx, job.ID, "info", fmt.Sprintf("status synced: %s", upstream.Status))

	// Emit webhook event when job reaches a terminal state for the first time.
	newStatus := upstream.Status
	if p.dispatcher != nil && prevStatus != newStatus &&
		(newStatus == "succeeded" || newStatus == "failed") {
		go p.dispatcher.Dispatch(context.Background(), job.TenantID, "fine_tuning."+newStatus, map[string]any{
			"job_id":      job.ID,
			"model":       job.BaseModel,
			"status":      newStatus,
			"finished_at": time.Now().UTC(),
		})
	}
}
