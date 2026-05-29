package handlers

import (
	"context"
	"fmt"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/yourorg/totra/gateway/middleware"
	"github.com/yourorg/totra/gateway/storage"
)

// EvalModelLookup resolves model configs by name for a given tenant.
type EvalModelLookup interface {
	GetByName(ctx context.Context, tenantID, modelName string) (*storage.ModelConfig, error)
}

// RegisterEvalRoutes mounts eval endpoints on the provided router.
func RegisterEvalRoutes(router fiber.Router, evalStore *storage.EvalStore, promptStore *storage.PromptStore, modelLookup EvalModelLookup, dispatcher *WebhookDispatcher) {
	router.Post("/evals/suites", createEvalSuite(evalStore))
	router.Get("/evals/suites", listEvalSuites(evalStore))
	router.Get("/evals/suites/:id", getEvalSuite(evalStore))
	router.Post("/evals/suites/:id/cases", addEvalCase(evalStore))
	router.Post("/evals/suites/:id/run", triggerEvalRun(evalStore, promptStore, modelLookup, dispatcher))
	router.Get("/evals/suites/:id/trends", getEvalTrends(evalStore))
	router.Post("/evals/suites/:id/compare", compareModels(evalStore, promptStore, modelLookup, dispatcher))
	router.Get("/evals/suites/:id/recommendation", getModelRecommendation(evalStore))
	router.Get("/evals/runs/:run_id", getEvalRun(evalStore))
	router.Get("/evals/runs/:run_id/results", getEvalRunResults(evalStore))
	router.Get("/evals/benchmarks", listBenchmarks())
	router.Post("/evals/benchmarks/:id/import", importBenchmark(evalStore))
}

func createEvalSuite(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		var req struct {
			Name       string `json:"name"`
			PromptName string `json:"prompt_name"`
		}
		if err := c.BodyParser(&req); err != nil || req.Name == "" || req.PromptName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "name and prompt_name are required"},
			})
		}
		suite, err := store.CreateSuite(c.Context(), user.TenantID, req.Name, req.PromptName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(suite)
	}
}

func listEvalSuites(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suites, err := store.ListSuites(c.Context(), user.TenantID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if suites == nil {
			suites = []*storage.EvalSuite{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": suites})
	}
}

func getEvalSuite(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suite, err := store.GetSuite(c.Context(), user.TenantID, c.Params("id"))
		if err != nil || suite == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "suite not found"})
		}
		cases, err := store.ListCases(c.Context(), suite.ID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if cases == nil {
			cases = []*storage.EvalCase{}
		}
		runs, err := store.ListRuns(c.Context(), suite.ID, 10)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if runs == nil {
			runs = []*storage.EvalRun{}
		}
		return c.JSON(fiber.Map{
			"suite": suite,
			"cases": cases,
			"runs":  runs,
		})
	}
}

func addEvalCase(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suiteID := c.Params("id")
		suite, err := store.GetSuite(c.Context(), user.TenantID, suiteID)
		if err != nil || suite == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "suite not found"})
		}
		var req struct {
			InputVars        map[string]any `json:"input_vars"`
			ExpectedOutput   string         `json:"expected_output"`
			ExpectedContains []string       `json:"expected_contains"`
			ScoreMethod      string         `json:"score_method"`
		}
		if err := c.BodyParser(&req); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid payload"})
		}
		if req.ScoreMethod == "" {
			req.ScoreMethod = "contains"
		}
		ec, err := store.AddCase(c.Context(), suiteID, req.InputVars, req.ExpectedOutput, req.ExpectedContains, req.ScoreMethod)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		return c.Status(fiber.StatusCreated).JSON(ec)
	}
}

func triggerEvalRun(evalStore *storage.EvalStore, promptStore *storage.PromptStore, modelLookup EvalModelLookup, dispatcher *WebhookDispatcher) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suiteID := c.Params("id")
		suite, err := evalStore.GetSuite(c.Context(), user.TenantID, suiteID)
		if err != nil || suite == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "suite not found"})
		}
		var req struct {
			Model         string `json:"model"`
			PromptVersion int    `json:"prompt_version"`
		}
		if err := c.BodyParser(&req); err != nil || req.Model == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "model is required"},
			})
		}
		run, err := evalStore.CreateRun(c.Context(), suite.ID, user.TenantID, req.Model, req.PromptVersion)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		go runEvalSuite(context.Background(), evalStore, promptStore, modelLookup, dispatcher, run, suite)
		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"run_id": run.ID, "status": run.Status})
	}
}

func getEvalRun(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		_, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		run, err := store.GetRun(c.Context(), c.Params("run_id"))
		if err != nil || run == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "run not found"})
		}
		return c.JSON(run)
	}
}

func getEvalRunResults(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		_, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		results, err := store.GetRunResults(c.Context(), c.Params("run_id"))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if results == nil {
			results = []*storage.EvalResult{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": results})
	}
}

// GET /v1/evals/suites/:id/trends?limit=20
// Returns completed run history ordered ASC for score-over-time charting.
func getEvalTrends(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suiteID := c.Params("id")
		suite, err := store.GetSuite(c.Context(), user.TenantID, suiteID)
		if err != nil || suite == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "suite not found"})
		}
		limit := c.QueryInt("limit", 20)
		trends, err := store.ListRunTrends(c.Context(), suiteID, limit)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if trends == nil {
			trends = []*storage.RunTrend{}
		}
		return c.JSON(fiber.Map{"object": "list", "data": trends})
	}
}

// compareRunEntry is a single entry in the compare response payload.
type compareRunEntry struct {
	Model string `json:"model"`
	RunID string `json:"run_id"`
}

// POST /v1/evals/suites/:id/compare
// Body: { "models": ["gpt-4o", "gpt-4o-mini"], "prompt_version": 0 }
// Starts one eval run per model in parallel and returns all run IDs immediately.
func compareModels(evalStore *storage.EvalStore, promptStore *storage.PromptStore, modelLookup EvalModelLookup, dispatcher *WebhookDispatcher) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suiteID := c.Params("id")
		suite, err := evalStore.GetSuite(c.Context(), user.TenantID, suiteID)
		if err != nil || suite == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "suite not found"})
		}

		var req struct {
			Models        []string `json:"models"`
			PromptVersion *int     `json:"prompt_version"`
		}
		if err := c.BodyParser(&req); err != nil || len(req.Models) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "models array is required and must not be empty"},
			})
		}
		if len(req.Models) > 10 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "at most 10 models per comparison"},
			})
		}

		promptVersion := 0
		if req.PromptVersion != nil {
			promptVersion = *req.PromptVersion
		}

		// Create all runs upfront so we can return their IDs immediately.
		entries := make([]compareRunEntry, 0, len(req.Models))
		runs := make([]*storage.EvalRun, 0, len(req.Models))
		for _, model := range req.Models {
			run, err := evalStore.CreateRun(c.Context(), suite.ID, user.TenantID, model, promptVersion)
			if err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": fmt.Sprintf("failed to create run for model %s", model),
				})
			}
			entries = append(entries, compareRunEntry{Model: model, RunID: run.ID})
			runs = append(runs, run)
		}

		// Launch all eval goroutines in parallel.
		var wg sync.WaitGroup
		for i := range runs {
			wg.Add(1)
			run := runs[i]
			go func() {
				defer wg.Done()
				runEvalSuite(context.Background(), evalStore, promptStore, modelLookup, dispatcher, run, suite)
			}()
		}
		// Fire-and-forget: do not wait for wg here; return immediately.
		go wg.Wait()

		return c.Status(fiber.StatusAccepted).JSON(fiber.Map{"runs": entries})
	}
}

// GET /v1/evals/suites/:id/recommendation
// Returns the model with the highest score_pct from recent completed runs.
func getModelRecommendation(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		suiteID := c.Params("id")
		suite, err := store.GetSuite(c.Context(), user.TenantID, suiteID)
		if err != nil || suite == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "suite not found"})
		}
		model, scorePct, err := store.GetBestModel(c.Context(), suiteID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error"})
		}
		if model == "" {
			return c.JSON(fiber.Map{"recommended_model": nil, "reason": "no completed runs found"})
		}
		reason := fmt.Sprintf("highest score (%.0f%%) across completed runs", scorePct)
		return c.JSON(fiber.Map{
			"recommended_model": model,
			"score_pct":         scorePct,
			"reason":            reason,
		})
	}
}

// GET /v1/evals/benchmarks
// Lists all available built-in benchmark datasets (metadata only, no cases).
func listBenchmarks() fiber.Handler {
	return func(c *fiber.Ctx) error {
		_, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		all := storage.GetBuiltinBenchmarks()
		// Return metadata without the full cases array to keep the response compact.
		type meta struct {
			ID          string `json:"id"`
			Industry    string `json:"industry"`
			Name        string `json:"name"`
			Description string `json:"description"`
			CaseCount   int    `json:"case_count"`
		}
		out := make([]meta, len(all))
		for i, d := range all {
			out[i] = meta{
				ID:          d.ID,
				Industry:    d.Industry,
				Name:        d.Name,
				Description: d.Description,
				CaseCount:   d.CaseCount,
			}
		}
		return c.JSON(fiber.Map{"object": "list", "data": out})
	}
}

// POST /v1/evals/benchmarks/:id/import
// Body: { "suite_name": "my-healthcare-test", "model": "gpt-4o-mini" }
// Creates a new eval suite pre-populated with all benchmark cases and returns the suite.
func importBenchmark(store *storage.EvalStore) fiber.Handler {
	return func(c *fiber.Ctx) error {
		user, ok := c.Locals("user").(*middleware.UserInfo)
		if !ok || user == nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
		}
		benchmarkID := c.Params("id")
		dataset := storage.GetBuiltinBenchmark(benchmarkID)
		if dataset == nil {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "benchmark not found"})
		}

		var req struct {
			SuiteName string `json:"suite_name"`
			Model     string `json:"model"`
		}
		if err := c.BodyParser(&req); err != nil || req.SuiteName == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": fiber.Map{"message": "suite_name is required"},
			})
		}

		// Create suite using the benchmark's industry tag as the prompt name placeholder.
		promptName := fmt.Sprintf("benchmark/%s", dataset.ID)
		suite, err := store.CreateSuite(c.Context(), user.TenantID, req.SuiteName, promptName)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error creating suite"})
		}

		// Insert all benchmark cases.
		for _, bc := range dataset.Cases {
			vars := make(map[string]any, len(bc.InputVars))
			for k, v := range bc.InputVars {
				vars[k] = v
			}
			if _, err := store.AddCase(c.Context(), suite.ID, vars, bc.Expected, bc.Contains, bc.ScoreMethod); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "db error adding case"})
			}
		}

		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"suite_id":   suite.ID,
			"suite_name": suite.Name,
			"model":      req.Model,
			"case_count": len(dataset.Cases),
		})
	}
}
