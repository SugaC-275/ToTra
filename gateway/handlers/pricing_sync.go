package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const liteLLMPricingURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// pricingEntry mirrors the fields we need from each LiteLLM pricing entry.
type pricingEntry struct {
	InputCostPerToken  float64 `json:"input_cost_per_token"`
	OutputCostPerToken float64 `json:"output_cost_per_token"`
	MaxTokens          int     `json:"max_tokens"`
}

// PricingSyncer periodically fetches LiteLLM's public pricing JSON and upserts
// it into canonical_model_pricing, then back-fills model_configs rows that have
// no price yet.
type PricingSyncer struct {
	pool       *pgxpool.Pool
	interval   time.Duration
	httpClient *http.Client
}

// NewPricingSyncer creates a PricingSyncer that will run every interval.
func NewPricingSyncer(pool *pgxpool.Pool, interval time.Duration) *PricingSyncer {
	return &PricingSyncer{
		pool:     pool,
		interval: interval,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Run starts the sync loop. It runs an initial sync immediately, then repeats
// every s.interval until ctx is cancelled.
func (s *PricingSyncer) Run(ctx context.Context) {
	if err := s.sync(ctx); err != nil {
		slog.Error("pricing sync: initial sync failed", "err", err)
	}

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sync(ctx); err != nil {
				slog.Error("pricing sync: sync failed", "err", err)
			}
		}
	}
}

// sync performs one full pricing sync cycle.
func (s *PricingSyncer) sync(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, liteLLMPricingURL, nil)
	if err != nil {
		return fmt.Errorf("pricing sync: create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("pricing sync: fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pricing sync: upstream returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("pricing sync: read body: %w", err)
	}

	var pricing map[string]pricingEntry
	if err := json.Unmarshal(body, &pricing); err != nil {
		return fmt.Errorf("pricing sync: parse JSON: %w", err)
	}

	synced := 0
	for modelName, entry := range pricing {
		_, execErr := s.pool.Exec(ctx, `
			INSERT INTO canonical_model_pricing
				(model_name, input_cost_per_token, output_cost_per_token, max_tokens, synced_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (model_name) DO UPDATE SET
				input_cost_per_token  = EXCLUDED.input_cost_per_token,
				output_cost_per_token = EXCLUDED.output_cost_per_token,
				max_tokens            = EXCLUDED.max_tokens,
				synced_at             = NOW()`,
			modelName,
			entry.InputCostPerToken,
			entry.OutputCostPerToken,
			entry.MaxTokens,
		)
		if execErr != nil {
			slog.Warn("pricing sync: upsert failed", "model", modelName, "err", execErr)
			continue
		}
		synced++
	}

	// Back-fill model_configs rows that have no price set yet.
	_, err = s.pool.Exec(ctx, `
		UPDATE model_configs mc
		SET price_per_m_input  = cmp.input_cost_per_token  * 1000000,
		    price_per_m_output = cmp.output_cost_per_token * 1000000
		FROM canonical_model_pricing cmp
		WHERE mc.name = cmp.model_name
		  AND mc.price_per_m_input IS NULL`)
	if err != nil {
		slog.Warn("pricing sync: back-fill failed", "err", err)
	}

	slog.Info("pricing sync: complete", "rows_synced", synced)
	return nil
}
