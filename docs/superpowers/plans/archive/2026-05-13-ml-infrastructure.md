# ML Infrastructure (Plan A) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build offline batch ML infrastructure that accumulates KPI feature data from every monthly snapshot and allows a Python Ridge regression trainer to replace the hand-tuned v3 pillar weights with learned ones.

**Architecture:** Four layers built in order: (1) Postgres tables for feature store and weight store, (2) Go feature extraction service that writes to the feature store after each monthly snapshot, (3) Go weight-loading that reads trained weights from Postgres with automatic fallback to v3 hardcoded values, (4) Python `train.py` containerized as a Docker Compose profile service. All v3 behavior is unchanged when the feature store is empty — zero disruption during rollout.

**Tech Stack:** Go 1.26 + pgx/v5, Python 3.12 + scikit-learn 1.4 + psycopg2-binary 2.9 + numpy 1.26, Postgres 16, Docker Compose profiles

**Spec:** `docs/superpowers/specs/2026-05-13-ml-infrastructure-design.md`

---

## File Map

| File | Action | Purpose |
|------|--------|---------|
| `infra/postgres/014_ml_infrastructure.sql` | Create | Two new tables: `ml_feature_snapshots`, `ml_model_weights` |
| `docker-compose.yml` | Modify | Add volume mounts for migrations 006–014; add `ml-trainer` service |
| `admin/services/ml_features.go` | Create | `MLFeatureService` + `SnapshotSummary` + `BuildFeatureRows` + `ExtractFeatures` |
| `admin/services/ml_features_test.go` | Create | Tests for `BuildFeatureRows` (pure function) |
| `admin/services/kpi.go` | Modify | Add `pillarWeights` type + `ResolveWeights` + `loadMLWeights` + wire `mlSvc` and weight loading into `RunMonthlySnapshot` |
| `admin/services/kpi_test.go` | Modify | Add tests for `ResolveWeights` |
| `ml/train.py` | Create | Ridge regression trainer: reads feature store, writes trained weights |
| `ml/requirements.txt` | Create | Python deps |
| `ml/Dockerfile` | Create | Python 3.12-slim container |
| `ml/test_train.py` | Create | Unit tests for pure normalization functions |

---

## Task 1: Migration 014 + docker-compose volumes

**Files:**
- Create: `infra/postgres/014_ml_infrastructure.sql`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Create migration file**

```sql
-- infra/postgres/014_ml_infrastructure.sql
-- Feature store: one row per user per month.
-- Phase 1 training uses aiq_score/oss_score/gts_score (pillar scores).
-- aiq_* raw dimensions are stored for future phase 2 model (dimension weight learning).
CREATE TABLE ml_feature_snapshots (
    id                    BIGSERIAL    PRIMARY KEY,
    user_id               UUID         NOT NULL,
    tenant_id             UUID         NOT NULL,
    year_month            VARCHAR(7)   NOT NULL,
    integration_level     INT          NOT NULL DEFAULT 1,
    aiq_score             FLOAT        NOT NULL DEFAULT 0,
    oss_score             FLOAT        NOT NULL DEFAULT 0,
    gts_score             FLOAT        NOT NULL DEFAULT 0,
    aiq_output_density    FLOAT        NOT NULL DEFAULT 0,
    aiq_usage_consistency FLOAT        NOT NULL DEFAULT 0,
    aiq_task_depth        FLOAT        NOT NULL DEFAULT 0,
    aiq_cost_efficiency   FLOAT        NOT NULL DEFAULT 0,
    label_composite       FLOAT        NOT NULL DEFAULT 0,
    label_source          VARCHAR(20)  NOT NULL DEFAULT 'auto_v3',
    label_manager         FLOAT,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, year_month)
);

-- Trained pillar weights: one active row per integration level.
-- Phase 1 stores w_aiq/w_oss/w_gts only. Phase 2 may add dimension weight columns.
CREATE TABLE ml_model_weights (
    id                BIGSERIAL    PRIMARY KEY,
    integration_level INT          NOT NULL,
    w_aiq             FLOAT        NOT NULL,
    w_oss             FLOAT        NOT NULL,
    w_gts             FLOAT        NOT NULL,
    trained_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    n_samples         INT          NOT NULL,
    rmse              FLOAT        NOT NULL,
    model_version     VARCHAR(20)  NOT NULL DEFAULT 'ridge_v1',
    is_active         BOOLEAN      NOT NULL DEFAULT TRUE
);

-- Partial unique index: only one active weight row per level.
CREATE UNIQUE INDEX ON ml_model_weights(integration_level) WHERE is_active = TRUE;
```

- [ ] **Step 2: Add migration volumes to docker-compose.yml**

Open `docker-compose.yml`. The `postgres` service currently has volumes for 001–005 only. Replace the volumes block (keep all non-SQL lines) so it includes 001–014 in order:

```yaml
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - ./infra/postgres/001_init.sql:/docker-entrypoint-initdb.d/001_init.sql:ro
      - ./infra/postgres/002_seed_dev.sql:/docker-entrypoint-initdb.d/002_seed_dev.sql:ro
      - ./infra/postgres/003_phase2.sql:/docker-entrypoint-initdb.d/003_phase2.sql:ro
      - ./infra/postgres/004_algorithm_v2.sql:/docker-entrypoint-initdb.d/004_algorithm_v2.sql:ro
      - ./infra/postgres/005_anomaly_detection.sql:/docker-entrypoint-initdb.d/005_anomaly_detection.sql:ro
      - ./infra/postgres/006_ip_allowlist.sql:/docker-entrypoint-initdb.d/006_ip_allowlist.sql:ro
      - ./infra/postgres/007_add_gitlab_confluence_platforms.sql:/docker-entrypoint-initdb.d/007_add_gitlab_confluence_platforms.sql:ro
      - ./infra/postgres/008_bot_configs.sql:/docker-entrypoint-initdb.d/008_bot_configs.sql:ro
      - ./infra/postgres/009_roi_reports.sql:/docker-entrypoint-initdb.d/009_roi_reports.sql:ro
      - ./infra/postgres/010_agent_sessions.sql:/docker-entrypoint-initdb.d/010_agent_sessions.sql:ro
      - ./infra/postgres/011_audit_log.sql:/docker-entrypoint-initdb.d/011_audit_log.sql:ro
      - ./infra/postgres/012_gdpr_compliance.sql:/docker-entrypoint-initdb.d/012_gdpr_compliance.sql:ro
      - ./infra/postgres/013_kpi_v3.sql:/docker-entrypoint-initdb.d/013_kpi_v3.sql:ro
      - ./infra/postgres/014_ml_infrastructure.sql:/docker-entrypoint-initdb.d/014_ml_infrastructure.sql:ro
```

- [ ] **Step 3: Commit**

```bash
git add infra/postgres/014_ml_infrastructure.sql docker-compose.yml
git commit -m "feat(db): migration 014 — ml_feature_snapshots + ml_model_weights"
```

---

## Task 2: MLFeatureService — feature extraction

**Files:**
- Create: `admin/services/ml_features.go`
- Create: `admin/services/ml_features_test.go`

- [ ] **Step 1: Write the failing test**

Create `admin/services/ml_features_test.go`:

```go
package services_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourorg/totra/admin/services"
)

func TestBuildFeatureRows_PopulatesAllFields(t *testing.T) {
	snapshots := []services.SnapshotSummary{
		{UserID: "u1", AIQScore: 70, OSSScore: 60, GTSScore: 50,
			IntegrationLevel: 2, EfficiencyScore: 65},
	}
	rawByUser := map[string]*services.RawAIQMetrics{
		"u1": {UserID: "u1", OutputDensity: 1.2, UsageConsistency: 0.8,
			TaskDepth: 3.4, CostEfficiency: 5.6},
	}

	rows := services.BuildFeatureRows("tenant-1", "2026-05", snapshots, rawByUser)

	assert.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "u1", r.UserID)
	assert.Equal(t, "tenant-1", r.TenantID)
	assert.Equal(t, "2026-05", r.YearMonth)
	assert.Equal(t, 2, r.IntegrationLevel)
	assert.Equal(t, 70.0, r.AIQScore)
	assert.Equal(t, 60.0, r.OSSScore)
	assert.Equal(t, 50.0, r.GTSScore)
	assert.InDelta(t, 1.2, r.AIQOutputDensity, 0.001)
	assert.InDelta(t, 0.8, r.AIQUsageConsistency, 0.001)
	assert.InDelta(t, 3.4, r.AIQTaskDepth, 0.001)
	assert.InDelta(t, 5.6, r.AIQCostEfficiency, 0.001)
	assert.Equal(t, 65.0, r.LabelComposite)
}

func TestBuildFeatureRows_MissingRawMetrics_ZerosDimensions(t *testing.T) {
	snapshots := []services.SnapshotSummary{
		{UserID: "u2", AIQScore: 55, OSSScore: 40, GTSScore: 30,
			IntegrationLevel: 1, EfficiencyScore: 45},
	}
	// No raw metrics for u2
	rows := services.BuildFeatureRows("tenant-1", "2026-05", snapshots, map[string]*services.RawAIQMetrics{})

	assert.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, 55.0, r.AIQScore)   // pillar scores still set
	assert.Equal(t, 0.0, r.AIQOutputDensity)    // raw dims zero
	assert.Equal(t, 0.0, r.AIQUsageConsistency) // raw dims zero
}

func TestBuildFeatureRows_EmptyInput(t *testing.T) {
	rows := services.BuildFeatureRows("t", "2026-05", nil, nil)
	assert.Empty(t, rows)
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestBuildFeatureRows -v 2>&1 | head -20
```

Expected: FAIL — `services.SnapshotSummary undefined`, `services.BuildFeatureRows undefined`

- [ ] **Step 3: Implement ml_features.go**

Create `admin/services/ml_features.go`:

```go
package services

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SnapshotSummary is the input to BuildFeatureRows, read from efficiency_snapshots.
type SnapshotSummary struct {
	UserID           string
	AIQScore         float64
	OSSScore         float64
	GTSScore         float64
	IntegrationLevel int
	EfficiencyScore  float64
}

// MLFeatureRow is one row written to ml_feature_snapshots.
type MLFeatureRow struct {
	UserID              string
	TenantID            string
	YearMonth           string
	IntegrationLevel    int
	AIQScore            float64
	OSSScore            float64
	GTSScore            float64
	AIQOutputDensity    float64
	AIQUsageConsistency float64
	AIQTaskDepth        float64
	AIQCostEfficiency   float64
	LabelComposite      float64
}

// BuildFeatureRows is the pure join of snapshot summaries with raw AIQ metrics.
// Exported for unit testing. Called by ExtractFeatures before upserting to DB.
func BuildFeatureRows(tenantID, yearMonth string, snapshots []SnapshotSummary, rawByUser map[string]*RawAIQMetrics) []MLFeatureRow {
	if len(snapshots) == 0 {
		return nil
	}
	result := make([]MLFeatureRow, 0, len(snapshots))
	for _, snap := range snapshots {
		row := MLFeatureRow{
			UserID:           snap.UserID,
			TenantID:         tenantID,
			YearMonth:        yearMonth,
			IntegrationLevel: snap.IntegrationLevel,
			AIQScore:         snap.AIQScore,
			OSSScore:         snap.OSSScore,
			GTSScore:         snap.GTSScore,
			LabelComposite:   snap.EfficiencyScore,
		}
		if raw, ok := rawByUser[snap.UserID]; ok {
			row.AIQOutputDensity = raw.OutputDensity
			row.AIQUsageConsistency = raw.UsageConsistency
			row.AIQTaskDepth = raw.TaskDepth
			row.AIQCostEfficiency = raw.CostEfficiency
		}
		result = append(result, row)
	}
	return result
}

// MLFeatureService writes per-user feature vectors to ml_feature_snapshots after each monthly snapshot.
type MLFeatureService struct {
	pool *pgxpool.Pool
}

func NewMLFeatureService(pool *pgxpool.Pool) *MLFeatureService {
	return &MLFeatureService{pool: pool}
}

// ExtractFeatures reads the just-written efficiency_snapshots for yearMonth,
// joins with rawMetrics (already computed during RunMonthlySnapshot), and
// upserts one row per user into ml_feature_snapshots.
func (s *MLFeatureService) ExtractFeatures(ctx context.Context, tenantID, yearMonth string, rawMetrics []*RawAIQMetrics) error {
	rawByUser := make(map[string]*RawAIQMetrics, len(rawMetrics))
	for _, m := range rawMetrics {
		rawByUser[m.UserID] = m
	}

	rows, err := s.pool.Query(ctx, `
		SELECT user_id, COALESCE(aiq_score,0), COALESCE(oss_score,0), COALESCE(gts_score,0),
		       COALESCE(integration_level,1), efficiency_score
		FROM efficiency_snapshots
		WHERE tenant_id=$1 AND year_month=$2`, tenantID, yearMonth)
	if err != nil {
		return err
	}
	defer rows.Close()

	var snapshots []SnapshotSummary
	for rows.Next() {
		var snap SnapshotSummary
		if err := rows.Scan(&snap.UserID, &snap.AIQScore, &snap.OSSScore, &snap.GTSScore,
			&snap.IntegrationLevel, &snap.EfficiencyScore); err != nil {
			return err
		}
		snapshots = append(snapshots, snap)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	featureRows := BuildFeatureRows(tenantID, yearMonth, snapshots, rawByUser)
	for _, r := range featureRows {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO ml_feature_snapshots
			  (user_id, tenant_id, year_month, integration_level,
			   aiq_score, oss_score, gts_score,
			   aiq_output_density, aiq_usage_consistency, aiq_task_depth, aiq_cost_efficiency,
			   label_composite, label_source)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'auto_v3')
			ON CONFLICT (user_id, year_month) DO UPDATE
			SET integration_level=$4, aiq_score=$5, oss_score=$6, gts_score=$7,
			    aiq_output_density=$8, aiq_usage_consistency=$9,
			    aiq_task_depth=$10, aiq_cost_efficiency=$11, label_composite=$12`,
			r.UserID, r.TenantID, r.YearMonth, r.IntegrationLevel,
			r.AIQScore, r.OSSScore, r.GTSScore,
			r.AIQOutputDensity, r.AIQUsageConsistency, r.AIQTaskDepth, r.AIQCostEfficiency,
			r.LabelComposite,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestBuildFeatureRows -v 2>&1
```

Expected:
```
--- PASS: TestBuildFeatureRows_PopulatesAllFields
--- PASS: TestBuildFeatureRows_MissingRawMetrics_ZerosDimensions
--- PASS: TestBuildFeatureRows_EmptyInput
PASS
```

- [ ] **Step 5: Commit**

```bash
git add admin/services/ml_features.go admin/services/ml_features_test.go
git commit -m "feat(ml): MLFeatureService — feature extraction and upsert to ml_feature_snapshots"
```

---

## Task 3: Weight loading + ResolveWeights

**Files:**
- Modify: `admin/services/kpi.go` (add `pillarWeights` type, `ResolveWeights`, `loadMLWeights`)
- Modify: `admin/services/kpi_test.go` (add `TestResolveWeights_*`)

- [ ] **Step 1: Write the failing tests**

Add to the end of `admin/services/kpi_test.go`:

```go
func TestResolveWeights_FallsBackToV3WhenNoMLWeights(t *testing.T) {
	// Empty map → same result as AdaptiveWeights
	emptyML := map[int]services.PillarWeights{}

	a1, o1, g1 := services.AdaptiveWeights(1, true)
	a2, o2, g2 := services.ResolveWeights(1, true, emptyML)
	assert.InDelta(t, a1, a2, 1e-9)
	assert.InDelta(t, o1, o2, 1e-9)
	assert.InDelta(t, g1, g2, 1e-9)
}

func TestResolveWeights_UsesMLWeightsWhenPresent(t *testing.T) {
	ml := map[int]services.PillarWeights{
		2: {AIQ: 0.45, OSS: 0.35, GTS: 0.20},
	}
	a, o, g := services.ResolveWeights(2, true, ml)
	assert.InDelta(t, 0.45, a, 1e-9)
	assert.InDelta(t, 0.35, o, 1e-9)
	assert.InDelta(t, 0.20, g, 1e-9)
}

func TestResolveWeights_NoGTSRedistributesGTSToAIQ(t *testing.T) {
	ml := map[int]services.PillarWeights{
		1: {AIQ: 0.55, OSS: 0.20, GTS: 0.25},
	}
	// hasGTS=false → GTS weight should be added to AIQ, GTS returns 0
	a, o, g := services.ResolveWeights(1, false, ml)
	assert.InDelta(t, 0.80, a, 1e-9) // 0.55 + 0.25
	assert.InDelta(t, 0.20, o, 1e-9)
	assert.Equal(t, 0.0, g)
}

func TestResolveWeights_FallsBackForUnknownLevel(t *testing.T) {
	// ML has level 2 but we ask for level 99 → fallback to v3 (which defaults to L1)
	ml := map[int]services.PillarWeights{
		2: {AIQ: 0.40, OSS: 0.40, GTS: 0.20},
	}
	a, o, g := services.ResolveWeights(99, true, ml)
	a0, o0, g0 := services.AdaptiveWeights(99, true)
	assert.InDelta(t, a0, a, 1e-9)
	assert.InDelta(t, o0, o, 1e-9)
	assert.InDelta(t, g0, g, 1e-9)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestResolveWeights -v 2>&1 | head -20
```

Expected: FAIL — `services.PillarWeights undefined`, `services.ResolveWeights undefined`

- [ ] **Step 3: Add PillarWeights, ResolveWeights, and loadMLWeights to kpi.go**

In `admin/services/kpi.go`, add these three items directly after the `IsAnomalySCU` function (after line ~126):

```go
// PillarWeights holds the three KPI pillar weights (must sum to 1.0).
// Exported so tests in package services_test can construct test maps.
type PillarWeights struct {
	AIQ float64
	OSS float64
	GTS float64
}

// ResolveWeights returns pillar weights for the given integration level.
// If ml contains a trained weight for level, it is used; otherwise falls back to AdaptiveWeights.
// Exported for unit testing; called inside RunMonthlySnapshot.
func ResolveWeights(level int, hasGTS bool, ml map[int]PillarWeights) (float64, float64, float64) {
	if w, ok := ml[level]; ok {
		if !hasGTS {
			return w.AIQ + w.GTS, w.OSS, 0
		}
		return w.AIQ, w.OSS, w.GTS
	}
	return AdaptiveWeights(level, hasGTS)
}

// loadMLWeights reads active trained pillar weights from ml_model_weights.
// Returns an empty map (not an error) if the table is missing or empty — callers fall back to v3.
func (s *KPIService) loadMLWeights(ctx context.Context) map[int]PillarWeights {
	rows, err := s.pool.Query(ctx,
		`SELECT integration_level, w_aiq, w_oss, w_gts FROM ml_model_weights WHERE is_active=TRUE`)
	if err != nil {
		return map[int]PillarWeights{}
	}
	defer rows.Close()
	result := map[int]PillarWeights{}
	for rows.Next() {
		var lvl int
		var w PillarWeights
		if err := rows.Scan(&lvl, &w.AIQ, &w.OSS, &w.GTS); err != nil {
			continue
		}
		result[lvl] = w
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./services/... -run TestResolveWeights -v 2>&1
```

Expected:
```
--- PASS: TestResolveWeights_FallsBackToV3WhenNoMLWeights
--- PASS: TestResolveWeights_UsesMLWeightsWhenPresent
--- PASS: TestResolveWeights_NoGTSRedistributesGTSToAIQ
--- PASS: TestResolveWeights_FallsBackForUnknownLevel
PASS
```

- [ ] **Step 5: Run full test suite**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... 2>&1
```

Expected: `ok` for all packages, 0 failures.

- [ ] **Step 6: Commit**

```bash
git add admin/services/kpi.go admin/services/kpi_test.go
git commit -m "feat(ml): PillarWeights + ResolveWeights + loadMLWeights — DB-backed weight loading with v3 fallback"
```

---

## Task 4: Wire ML into RunMonthlySnapshot

**Files:**
- Modify: `admin/services/kpi.go` (add `mlSvc` field to `KPIService`; call `loadMLWeights` and `ResolveWeights` in `RunMonthlySnapshot`; call `ExtractFeatures` at end)

No new tests needed — `ResolveWeights` is already covered; `ExtractFeatures` is already covered. Wiring is verified by the full test suite staying green.

- [ ] **Step 1: Add mlSvc field to KPIService**

In `admin/services/kpi.go`, change the `KPIService` struct and `NewKPIService`:

```go
type KPIService struct {
	pool   *pgxpool.Pool
	aiqSvc *AIQService
	mlSvc  *MLFeatureService
}

func NewKPIService(pool *pgxpool.Pool) *KPIService {
	return &KPIService{
		pool:   pool,
		aiqSvc: NewAIQService(pool),
		mlSvc:  NewMLFeatureService(pool),
	}
}
```

- [ ] **Step 2: Load ML weights at the start of RunMonthlySnapshot**

In `RunMonthlySnapshot`, immediately after the line `level := DetectIntegrationLevel(webhookCount)` (currently around line 315), add:

```go
	// Load ML-trained pillar weights (empty map → v3 fallback inside ResolveWeights).
	mlWeights := s.loadMLWeights(ctx)
```

- [ ] **Step 3: Replace AdaptiveWeights call with ResolveWeights**

In `RunMonthlySnapshot`, step 8 (ranking loop, around line 485), find:

```go
		aW, oW, gW := AdaptiveWeights(level, snap.HasGTS)
```

Replace with:

```go
		aW, oW, gW := ResolveWeights(level, snap.HasGTS, mlWeights)
```

- [ ] **Step 4: Call ExtractFeatures at the end of RunMonthlySnapshot**

`RunMonthlySnapshot` ends with `return nil` after the upsert loop. Before that final `return nil`, add:

```go
	// Write feature vectors to ml_feature_snapshots for future ML training.
	// Non-fatal: a failure here does not invalidate the snapshot.
	if err := s.mlSvc.ExtractFeatures(ctx, tenantID, yearMonth, rawMetrics); err != nil {
		_ = err // log in production; silently ignored here
	}

	return nil
```

- [ ] **Step 5: Run full test suite to confirm no regressions**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... -count=1 2>&1
```

Expected: `ok admin/api`, `ok admin/services`, 0 failures.

- [ ] **Step 6: Commit**

```bash
git add admin/services/kpi.go
git commit -m "feat(ml): wire loadMLWeights + ResolveWeights + ExtractFeatures into RunMonthlySnapshot"
```

---

## Task 5: Python training script

**Files:**
- Create: `ml/train.py`
- Create: `ml/requirements.txt`
- Create: `ml/test_train.py`

- [ ] **Step 1: Write the failing tests**

Create `ml/test_train.py`:

```python
import math
import numpy as np
import pytest
from train import normalize_weights, compute_rmse


def test_normalize_weights_sums_to_one():
    result = normalize_weights(np.array([0.5, 0.3, 0.2]))
    assert result is not None
    assert abs(result.sum() - 1.0) < 1e-9
    assert all(w > 0 for w in result)


def test_normalize_weights_clips_negative_coefficient():
    # One negative coef → clipped to 0, remaining two normalized
    result = normalize_weights(np.array([-0.1, 0.6, 0.5]))
    assert result is not None
    assert result[0] == pytest.approx(0.0)
    assert abs(result.sum() - 1.0) < 1e-9


def test_normalize_weights_all_negative_returns_none():
    result = normalize_weights(np.array([-0.5, -0.3, -0.2]))
    assert result is None


def test_normalize_weights_all_zero_returns_none():
    result = normalize_weights(np.array([0.0, 0.0, 0.0]))
    assert result is None


def test_compute_rmse_perfect_prediction():
    class FakeModel:
        def predict(self, X):
            return X[:, 0]  # return first feature as prediction
    X = np.array([[1.0], [2.0], [3.0]])
    y = np.array([1.0, 2.0, 3.0])
    assert compute_rmse(FakeModel(), X, y) == pytest.approx(0.0)


def test_compute_rmse_known_error():
    class FakeModel:
        def predict(self, X):
            return np.array([0.0, 0.0])
    X = np.zeros((2, 1))
    y = np.array([3.0, 4.0])  # errors: 3 and 4, MSE=12.5, RMSE≈3.536
    assert compute_rmse(FakeModel(), X, y) == pytest.approx(math.sqrt(12.5), rel=1e-6)
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /Users/sugac.275/ToTra && pip install numpy scikit-learn pytest --quiet 2>&1 | tail -3
cd /Users/sugac.275/ToTra/ml && python -m pytest test_train.py -v 2>&1 | head -20
```

Expected: ModuleNotFoundError (train.py does not exist yet)

- [ ] **Step 3: Create requirements.txt**

Create `ml/requirements.txt`:

```
psycopg2-binary>=2.9
scikit-learn>=1.4
numpy>=1.26
pytest>=8.0
```

- [ ] **Step 4: Implement train.py**

Create `ml/train.py`:

```python
#!/usr/bin/env python3
"""Offline Ridge regression trainer for ToTra KPI pillar weights.

Reads ml_feature_snapshots from Postgres, trains one Ridge model per
integration level (1/2/3), and writes trained pillar weights to
ml_model_weights. Skips any level with fewer than MIN_SAMPLES rows.

Usage (via Docker Compose):
    docker compose --profile ml run --rm ml-trainer

Usage (local dev, requires DATABASE_URL env):
    DATABASE_URL=postgres://... python train.py
"""
import math
import os
import sys

import numpy as np
import psycopg2
from sklearn.linear_model import Ridge

MIN_SAMPLES = 30


def load_features(conn, level: int):
    """Return (X, y) for a given integration_level.
    y prefers label_manager over label_composite when available.
    Returns (None, None) if no rows exist for this level.
    """
    with conn.cursor() as cur:
        cur.execute(
            """
            SELECT aiq_score, oss_score, gts_score,
                   COALESCE(label_manager, label_composite) AS label
            FROM ml_feature_snapshots
            WHERE integration_level = %s
            """,
            (level,),
        )
        rows = cur.fetchall()
    if not rows:
        return None, None
    arr = np.array(rows, dtype=float)
    return arr[:, :3], arr[:, 3]


def normalize_weights(coefs: np.ndarray):
    """Clip negative coefficients to 0 and normalize to sum 1.0.
    Returns None if all coefficients are zero after clipping.
    """
    clipped = np.clip(coefs, 0, None)
    total = clipped.sum()
    if total == 0:
        return None
    return clipped / total


def compute_rmse(model, X: np.ndarray, y: np.ndarray) -> float:
    preds = model.predict(X)
    return math.sqrt(float(((preds - y) ** 2).mean()))


def train_level(conn, level: int) -> bool:
    """Train Ridge model for one integration level. Returns True if weights were written."""
    X, y = load_features(conn, level)
    n = len(X) if X is not None else 0

    if X is None or n < MIN_SAMPLES:
        print(f"  Level {level}: {n} samples — skipping (need {MIN_SAMPLES})")
        return False

    model = Ridge(alpha=1.0)
    model.fit(X, y)

    original_coefs = model.coef_.copy()
    weights = normalize_weights(model.coef_)

    if weights is None:
        print(f"  Level {level}: all coefficients non-positive after clipping — skipping")
        return False

    if any(c < 0 for c in original_coefs):
        print(f"  Level {level}: WARNING — {sum(c < 0 for c in original_coefs)} negative coef(s) clipped to 0")

    rmse = compute_rmse(model, X, y)

    with conn.cursor() as cur:
        cur.execute(
            "UPDATE ml_model_weights SET is_active=FALSE WHERE integration_level=%s",
            (level,),
        )
        cur.execute(
            """
            INSERT INTO ml_model_weights
              (integration_level, w_aiq, w_oss, w_gts, n_samples, rmse, model_version, is_active)
            VALUES (%s, %s, %s, %s, %s, %s, 'ridge_v1', TRUE)
            """,
            (level, float(weights[0]), float(weights[1]), float(weights[2]), n, rmse),
        )

    print(
        f"  Level {level}: n={n}, RMSE={rmse:.2f}  "
        f"[AIQ={weights[0]:.3f}  OSS={weights[1]:.3f}  GTS={weights[2]:.3f}]"
    )
    return True


def main():
    db_url = os.environ.get("DATABASE_URL")
    if not db_url:
        print("ERROR: DATABASE_URL env var is required", file=sys.stderr)
        sys.exit(1)

    conn = psycopg2.connect(db_url)
    try:
        trained = 0
        for level in (1, 2, 3):
            print(f"Training level {level}...")
            if train_level(conn, level):
                trained += 1
        conn.commit()
        print(f"\nDone. Trained {trained}/3 levels.")
        if trained == 0:
            print("No levels trained — not enough data yet. v3 hardcoded weights remain active.")
    except Exception as exc:
        conn.rollback()
        print(f"Error: {exc}", file=sys.stderr)
        sys.exit(1)
    finally:
        conn.close()


if __name__ == "__main__":
    main()
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd /Users/sugac.275/ToTra/ml && python -m pytest test_train.py -v 2>&1
```

Expected:
```
PASSED test_train.py::test_normalize_weights_sums_to_one
PASSED test_train.py::test_normalize_weights_clips_negative_coefficient
PASSED test_train.py::test_normalize_weights_all_negative_returns_none
PASSED test_train.py::test_normalize_weights_all_zero_returns_none
PASSED test_train.py::test_compute_rmse_perfect_prediction
PASSED test_train.py::test_compute_rmse_known_error
6 passed
```

- [ ] **Step 6: Commit**

```bash
git add ml/train.py ml/requirements.txt ml/test_train.py
git commit -m "feat(ml): Python Ridge trainer — reads ml_feature_snapshots, writes pillar weights"
```

---

## Task 6: Dockerfile + docker-compose ml-trainer profile

**Files:**
- Create: `ml/Dockerfile`
- Modify: `docker-compose.yml` (add `ml-trainer` service)

- [ ] **Step 1: Create Dockerfile**

Create `ml/Dockerfile`:

```dockerfile
FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
# Approach A: run as one-off batch job
# Approach B upgrade: change CMD to ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8090"]
CMD ["python", "train.py"]
```

- [ ] **Step 2: Add ml-trainer service to docker-compose.yml**

In `docker-compose.yml`, add the `ml-trainer` service before the `volumes:` block at the bottom. Keep all existing services unchanged:

```yaml
  ml-trainer:
    build: ./ml
    profiles: ["ml"]
    environment:
      DATABASE_URL: postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB}
    depends_on:
      postgres:
        condition: service_healthy
```

> **Approach B upgrade note:** When upgrading to FastAPI microservice, remove `profiles: ["ml"]`, rename the service to `ml-service`, add `ports: ["8090:8090"]`, and change the CMD in Dockerfile to `uvicorn app:app --host 0.0.0.0 --port 8090`.

- [ ] **Step 3: Verify docker build succeeds**

```bash
cd /Users/sugac.275/ToTra && docker build -f ml/Dockerfile ml/ -t totra-ml-trainer:test 2>&1 | tail -5
```

Expected: `Successfully built <id>` or `=> exporting to image` with exit 0.

- [ ] **Step 4: Verify docker-compose config is valid**

```bash
cd /Users/sugac.275/ToTra && docker compose config --quiet 2>&1
```

Expected: no output (exit 0 = valid config).

- [ ] **Step 5: Run full Go test suite one final time**

```bash
cd /Users/sugac.275/ToTra/admin && go test ./... -count=1 2>&1
```

Expected: `ok admin/api`, `ok admin/services`, 0 failures.

- [ ] **Step 6: Final commit**

```bash
git add ml/Dockerfile docker-compose.yml
git commit -m "feat(ml): Dockerfile + docker-compose ml-trainer profile (Approach A)"
```

---

## Verification: end-to-end manual check

After all tasks complete, verify the data pipeline works as expected with an existing Postgres instance:

```bash
# 1. Start a fresh DB (if running locally)
docker compose up postgres -d

# 2. Confirm new tables exist
docker compose exec postgres psql -U totra -d totra -c "\dt ml_*"
# Expected: ml_feature_snapshots, ml_model_weights

# 3. Confirm ml_model_weights is empty (no trained weights yet — v3 weights active)
docker compose exec postgres psql -U totra -d totra -c "SELECT COUNT(*) FROM ml_model_weights;"
# Expected: 0

# 4. When data is ready, run trainer:
docker compose --profile ml run --rm ml-trainer
# Expected: "No levels trained — not enough data yet. v3 hardcoded weights remain active."
```
