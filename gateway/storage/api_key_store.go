package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// APIKeyRecord holds the data for a single provider API key entry.
type APIKeyRecord struct {
	ID            string
	ModelConfigID string
	APIKey        string
	Weight        int
	ErrorCount    int
	CooldownUntil *time.Time
}

// APIKeyStore manages per-provider API key rotation using PostgreSQL.
type APIKeyStore struct {
	pool *pgxpool.Pool
}

// NewAPIKeyStore returns an APIKeyStore backed by pool.
func NewAPIKeyStore(pool *pgxpool.Pool) *APIKeyStore {
	return &APIKeyStore{pool: pool}
}

// GetNextKey returns the best available key for a model config using weighted round-robin.
// Keys with active cooldowns are skipped. Falls back to least-recently-cooled key if all are cooling.
// Returns ("", "", nil) when no multi-keys are configured (caller uses model_config.api_key instead).
func (s *APIKeyStore) GetNextKey(ctx context.Context, modelConfigID string) (keyID, apiKey string, err error) {
	const q = `
		SELECT id, api_key_encrypted, weight
		FROM provider_api_keys
		WHERE model_config_id = $1
		  AND is_active = true
		  AND (cooldown_until IS NULL OR cooldown_until < NOW())
		ORDER BY COALESCE(last_used_at, '1970-01-01'::timestamptz) ASC, weight DESC
		LIMIT 1`

	var weight int
	err = s.pool.QueryRow(ctx, q, modelConfigID).Scan(&keyID, &apiKey, &weight)
	if err != nil {
		if err == pgx.ErrNoRows {
			// No available key — either not configured or all cooling.
			return "", "", nil
		}
		return "", "", fmt.Errorf("api_key_store: get next key: %w", err)
	}

	// Update last_used_at so the next call picks a different key.
	_, err = s.pool.Exec(ctx,
		`UPDATE provider_api_keys SET last_used_at = NOW() WHERE id = $1`,
		keyID,
	)
	if err != nil {
		return "", "", fmt.Errorf("api_key_store: update last_used_at: %w", err)
	}

	return keyID, apiKey, nil
}

// MarkSuccess clears the error count and cooldown for a key.
func (s *APIKeyStore) MarkSuccess(ctx context.Context, keyID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE provider_api_keys SET error_count = 0, cooldown_until = NULL WHERE id = $1`,
		keyID,
	)
	if err != nil {
		return fmt.Errorf("api_key_store: mark success: %w", err)
	}
	return nil
}

// MarkFailure increments the error count; after 3 cumulative errors it sets a 5-minute cooldown.
func (s *APIKeyStore) MarkFailure(ctx context.Context, keyID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE provider_api_keys
		 SET error_count    = error_count + 1,
		     cooldown_until = CASE
		                        WHEN error_count + 1 >= 3 THEN NOW() + INTERVAL '5 minutes'
		                        ELSE NULL
		                      END
		 WHERE id = $1`,
		keyID,
	)
	if err != nil {
		return fmt.Errorf("api_key_store: mark failure: %w", err)
	}
	return nil
}

// AddKey inserts a new provider API key for the given model config and returns the new key ID.
func (s *APIKeyStore) AddKey(ctx context.Context, modelConfigID, apiKey string, weight int) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx,
		`INSERT INTO provider_api_keys (model_config_id, api_key_encrypted, weight)
		 VALUES ($1, $2, $3)
		 RETURNING id`,
		modelConfigID, apiKey, weight,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("api_key_store: add key: %w", err)
	}
	return id, nil
}

// DeactivateKey marks a key as inactive so it is no longer returned by GetNextKey.
func (s *APIKeyStore) DeactivateKey(ctx context.Context, keyID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE provider_api_keys SET is_active = false WHERE id = $1`,
		keyID,
	)
	if err != nil {
		return fmt.Errorf("api_key_store: deactivate key: %w", err)
	}
	return nil
}

// ListForModel returns all active keys for a model config (for admin inspection).
func (s *APIKeyStore) ListForModel(ctx context.Context, modelConfigID string) ([]*APIKeyRecord, error) {
	const q = `
		SELECT id, model_config_id, api_key_encrypted, weight, error_count, cooldown_until
		FROM provider_api_keys
		WHERE model_config_id = $1 AND is_active = true
		ORDER BY created_at ASC`

	rows, err := s.pool.Query(ctx, q, modelConfigID)
	if err != nil {
		return nil, fmt.Errorf("api_key_store: list for model: %w", err)
	}
	defer rows.Close()

	var records []*APIKeyRecord
	for rows.Next() {
		r := &APIKeyRecord{}
		if err := rows.Scan(&r.ID, &r.ModelConfigID, &r.APIKey, &r.Weight, &r.ErrorCount, &r.CooldownUntil); err != nil {
			return nil, fmt.Errorf("api_key_store: scan: %w", err)
		}
		records = append(records, r)
	}
	return records, rows.Err()
}
