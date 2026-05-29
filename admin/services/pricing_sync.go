package services

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ModelPrice holds the known authoritative pricing for a model as of 2026-05-28.
type ModelPrice struct {
	Provider       string
	InputPerMTok  float64 // USD per million input tokens
	OutputPerMTok float64 // USD per million output tokens
}

// knownPricing is the local authoritative pricing table.
// Prices are in USD per million tokens.
var knownPricing = map[string]ModelPrice{
	"gpt-4o":                     {Provider: "openai", InputPerMTok: 2.50, OutputPerMTok: 10.00},
	"gpt-4o-mini":                {Provider: "openai", InputPerMTok: 0.15, OutputPerMTok: 0.60},
	"gpt-4-turbo":                {Provider: "openai", InputPerMTok: 10.00, OutputPerMTok: 30.00},
	"claude-3-5-sonnet-20241022": {Provider: "anthropic", InputPerMTok: 3.00, OutputPerMTok: 15.00},
	"claude-3-haiku-20240307":    {Provider: "anthropic", InputPerMTok: 0.25, OutputPerMTok: 1.25},
	"claude-3-opus-20240229":     {Provider: "anthropic", InputPerMTok: 15.00, OutputPerMTok: 75.00},
	"gemini-1.5-pro":             {Provider: "google", InputPerMTok: 1.25, OutputPerMTok: 5.00},
	"gemini-1.5-flash":           {Provider: "google", InputPerMTok: 0.075, OutputPerMTok: 0.30},
}

// ModelPricingRow is a row from model_pricing_history / current_model_pricing view.
type ModelPricingRow struct {
	ID            string    `json:"id"`
	ModelName     string    `json:"model_name"`
	Provider      string    `json:"provider"`
	InputPerMTok  float64   `json:"input_per_mtok"`
	OutputPerMTok float64   `json:"output_per_mtok"`
	EffectiveFrom time.Time `json:"effective_from"`
	Source        string    `json:"source"`
}

// PricingSyncService compares the local authoritative pricing table against the DB,
// inserts new rows on price changes >5%, and emits webhook notifications when the
// projected monthly spend shifts by >10%.
type PricingSyncService struct {
	pool    *pgxpool.Pool
	pushSvc *AlertPushService
}

// NewPricingSyncService creates a PricingSyncService.
func NewPricingSyncService(pool *pgxpool.Pool, pushSvc *AlertPushService) *PricingSyncService {
	return &PricingSyncService{pool: pool, pushSvc: pushSvc}
}

// SyncPricing compares knownPricing against the DB and inserts new history rows
// for any model whose price changed by >5%. Returns the number of models updated.
func SyncPricing(ctx context.Context, pool *pgxpool.Pool) error {
	svc := &PricingSyncService{pool: pool}
	_, err := svc.sync(ctx)
	return err
}

// sync is the core sync logic; returns count of models updated.
func (s *PricingSyncService) sync(ctx context.Context) (int, error) {
	updated := 0
	for modelName, price := range knownPricing {
		// Fetch current price from DB.
		var curInput, curOutput float64
		err := s.pool.QueryRow(ctx,
			`SELECT input_per_mtok, output_per_mtok
			 FROM current_model_pricing
			 WHERE model_name = $1`,
			modelName,
		).Scan(&curInput, &curOutput)

		isNew := err == pgx.ErrNoRows
		if err != nil && !isNew {
			slog.Warn("pricing_sync: fetch current price", "model", modelName, "err", err)
			continue
		}

		// Check if price changed by more than 5%.
		if !isNew {
			inputDelta := math.Abs(price.InputPerMTok-curInput) / math.Max(curInput, 1e-9)
			outputDelta := math.Abs(price.OutputPerMTok-curOutput) / math.Max(curOutput, 1e-9)
			if inputDelta <= 0.05 && outputDelta <= 0.05 {
				continue // no significant change
			}
		}

		// Insert new history row.
		_, err = s.pool.Exec(ctx,
			`INSERT INTO model_pricing_history
			   (model_name, provider, input_per_mtok, output_per_mtok, source)
			 VALUES ($1, $2, $3, $4, 'auto_sync')`,
			modelName, price.Provider, price.InputPerMTok, price.OutputPerMTok,
		)
		if err != nil {
			slog.Warn("pricing_sync: insert history", "model", modelName, "err", err)
			continue
		}
		updated++

		// Check projected monthly spend impact and notify if shift >10%.
		if !isNew && s.pushSvc != nil {
			inputShift := (price.InputPerMTok - curInput) / math.Max(curInput, 1e-9)
			outputShift := (price.OutputPerMTok - curOutput) / math.Max(curOutput, 1e-9)
			maxShift := inputShift
			if math.Abs(outputShift) > math.Abs(maxShift) {
				maxShift = outputShift
			}
			if math.Abs(maxShift) > 0.10 {
				s.notifyPriceChange(ctx, modelName, price, curInput, curOutput, maxShift)
			}
		}

		slog.Info("pricing_sync: updated", "model", modelName,
			"input_per_mtok", price.InputPerMTok,
			"output_per_mtok", price.OutputPerMTok)
	}
	return updated, nil
}

func (s *PricingSyncService) notifyPriceChange(ctx context.Context, model string, newPrice ModelPrice, oldInput, oldOutput, shift float64) {
	direction := "increased"
	if shift < 0 {
		direction = "decreased"
	}
	event := AlertEvent{
		EventType: "pricing_change",
		Title:     fmt.Sprintf("Model pricing %s: %s", direction, model),
		Message: fmt.Sprintf(
			"Model %s pricing changed by %.1f%%. Old: $%.4f/$%.4f per MTok (in/out). New: $%.4f/$%.4f.",
			model, math.Abs(shift)*100, oldInput, oldOutput, newPrice.InputPerMTok, newPrice.OutputPerMTok,
		),
		Severity:  "warning",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]any{
			"model":           model,
			"provider":        newPrice.Provider,
			"old_input_mtok":  oldInput,
			"old_output_mtok": oldOutput,
			"new_input_mtok":  newPrice.InputPerMTok,
			"new_output_mtok": newPrice.OutputPerMTok,
			"shift_pct":       math.Abs(shift) * 100,
		},
	}

	// Fan out to all tenants that have pricing_change subscriptions.
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT tenant_id FROM alert_delivery_configs
		 WHERE $1 = ANY(event_types) AND enabled = true`,
		"pricing_change",
	)
	if err != nil {
		slog.Warn("pricing_sync: list tenants for alert", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var tenantID string
		if scanErr := rows.Scan(&tenantID); scanErr != nil {
			continue
		}
		ev := event
		ev.TenantID = tenantID
		if deliverErr := s.pushSvc.Deliver(ctx, ev); deliverErr != nil {
			slog.Warn("pricing_sync: deliver alert", "tenant", tenantID, "err", deliverErr)
		}
	}
}

// GetCurrentPricing returns all current model prices from the DB view.
func (s *PricingSyncService) GetCurrentPricing(ctx context.Context) ([]*ModelPricingRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, model_name, provider, input_per_mtok, output_per_mtok, effective_from, source
		 FROM current_model_pricing
		 ORDER BY model_name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("pricing_sync get_current: %w", err)
	}
	defer rows.Close()
	var result []*ModelPricingRow
	for rows.Next() {
		var r ModelPricingRow
		if err := rows.Scan(&r.ID, &r.ModelName, &r.Provider, &r.InputPerMTok,
			&r.OutputPerMTok, &r.EffectiveFrom, &r.Source); err != nil {
			return nil, err
		}
		result = append(result, &r)
	}
	return result, rows.Err()
}

// GetPricingHistory returns the price history for a specific model.
func (s *PricingSyncService) GetPricingHistory(ctx context.Context, modelName string) ([]*ModelPricingRow, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, model_name, provider, input_per_mtok, output_per_mtok, effective_from, source
		 FROM model_pricing_history
		 WHERE model_name = $1
		 ORDER BY effective_from DESC`,
		modelName,
	)
	if err != nil {
		return nil, fmt.Errorf("pricing_sync get_history: %w", err)
	}
	defer rows.Close()
	var result []*ModelPricingRow
	for rows.Next() {
		var r ModelPricingRow
		if err := rows.Scan(&r.ID, &r.ModelName, &r.Provider, &r.InputPerMTok,
			&r.OutputPerMTok, &r.EffectiveFrom, &r.Source); err != nil {
			return nil, err
		}
		result = append(result, &r)
	}
	return result, rows.Err()
}

// TriggerSync runs a manual sync and returns the count of models updated.
func (s *PricingSyncService) TriggerSync(ctx context.Context) (int, error) {
	return s.sync(ctx)
}
