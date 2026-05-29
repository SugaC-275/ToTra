package handlers

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourorg/totra/gateway/middleware"
)

// CostEstimator queries canonical_model_pricing to produce pre-request cost estimates.
type CostEstimator struct {
	pool *pgxpool.Pool
}

// NewCostEstimator creates a CostEstimator backed by the given connection pool.
func NewCostEstimator(pool *pgxpool.Pool) *CostEstimator {
	return &CostEstimator{pool: pool}
}

// LookupPricing fetches input/output cost per token for a model from canonical_model_pricing.
// Returns (inputCostPerToken, outputCostPerToken, ok).
func LookupPricing(pool *pgxpool.Pool, ctx context.Context, model string) (float64, float64, bool) {
	var inputCost, outputCost float64
	err := pool.QueryRow(ctx,
		`SELECT input_cost_per_token, output_cost_per_token
		   FROM canonical_model_pricing
		  WHERE model_name = $1`,
		model,
	).Scan(&inputCost, &outputCost)
	if err != nil {
		return 0, 0, false
	}
	return inputCost, outputCost, true
}

type estimateRequest struct {
	Model     string `json:"model"`
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	MaxTokens *int `json:"max_tokens"`
}

type estimateResponse struct {
	Model                 string  `json:"model"`
	EstimatedPromptTokens int     `json:"estimated_prompt_tokens"`
	MaxCompletionTokens   int     `json:"max_completion_tokens"`
	EstimatedCostUSD      float64 `json:"estimated_cost_usd"`
	CostPerMillionInput   float64 `json:"cost_per_million_input"`
	CostPerMillionOutput  float64 `json:"cost_per_million_output"`
	Note                  string  `json:"note"`
}

// RegisterCostEstimateRoute mounts POST /v1/estimate on the given router.
func RegisterCostEstimateRoute(router fiber.Router, ce *CostEstimator) {
	router.Post("/estimate", ce.handleEstimate)
}

func (ce *CostEstimator) handleEstimate(c *fiber.Ctx) error {
	_, ok := c.Locals("user").(*middleware.UserInfo)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}

	var req estimateRequest
	if err := c.BodyParser(&req); err != nil || req.Model == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "model field required"})
	}

	inputCostPerToken, outputCostPerToken, found := LookupPricing(ce.pool, c.Context(), req.Model)
	if !found {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": fmt.Sprintf("pricing not available for model %s", req.Model),
			"model": req.Model,
		})
	}

	promptTokens := EstimateChatTokens(c.Body())

	maxCompletion := 500
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxCompletion = *req.MaxTokens
	}

	promptCost := float64(promptTokens) * inputCostPerToken
	completionCost := float64(maxCompletion) * outputCostPerToken
	totalCost := promptCost + completionCost

	return c.JSON(estimateResponse{
		Model:                 req.Model,
		EstimatedPromptTokens: promptTokens,
		MaxCompletionTokens:   maxCompletion,
		EstimatedCostUSD:      totalCost,
		CostPerMillionInput:   inputCostPerToken * 1_000_000,
		CostPerMillionOutput:  outputCostPerToken * 1_000_000,
		Note:                  "Estimate based on character count. Actual may vary ±15%.",
	})
}
