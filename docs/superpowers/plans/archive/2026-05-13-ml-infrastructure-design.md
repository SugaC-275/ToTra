# ML Infrastructure Design

> **For agentic workers:** Use `superpowers:subagent-driven-development` to implement this plan.

**Goal:** Build ML infrastructure that accumulates behavioral data from AI-native workers and progressively replaces the hand-tuned KPI weights (v3) with learned weights.

**Architecture:** Offline batch training (Approach A) is implemented now. Approach B (FastAPI microservice) is the documented upgrade path — all interfaces are designed to make the migration mechanical.

**Tech Stack:** Go 1.26 + Fiber v2, Python 3.12 + scikit-learn + psycopg2-binary, Postgres 16, Docker Compose

**Competitive moat:** The feature store (`ml_feature_snapshots`) accumulates proprietary cross-company behavioral data over time. Once trained on enough tenants, the model captures industry-wide patterns no competitor can replicate without the same data flywheel.

---

## Context

The KPI algorithm (v3) uses hand-tuned pillar weights:

| Level | AIQ | OSS | GTS |
|-------|-----|-----|-----|
| L1    | 55% | 20% | 25% |
| L2    | 40% | 40% | 20% |
| L3    | 35% | 50% | 15% |

These weights are educated guesses. ML replaces them with weights learned from real usage patterns. Until real labels exist, the v3 composite score itself serves as a pseudo-label (self-supervised bootstrap). When manager ratings or business-outcome labels become available, they replace the pseudo-label automatically via the `label_source` field.

The Go service always falls back to v3 hardcoded weights if no trained model exists — zero disruption during rollout.

---

## Approach A — Offline Batch (Current Implementation)

### Data Flow

```
Monthly cron (1st, 01:00)
       │
       ▼
[Go: MLFeatureService.ExtractFeatures()]
  reads efficiency_snapshots + raw KPI metrics
       │
       ▼
ml_feature_snapshots  ← feature store (Postgres)
       │
       ▼
[docker compose --profile ml run ml-trainer]
  python ml/train.py
       │
       ▼
ml_model_weights  ← trained weights (Postgres)
       │
       ▼
[Go: KPIService.loadMLWeights()]
  called at RunMonthlySnapshot start
  fallback to v3 hardcoded if table empty
```

### Database: Migration 014

```sql
-- Feature store: one row per user per month
-- aiq_* raw dimensions stored for future deep-model training (phase 2);
-- phase 1 training uses only aiq_score/oss_score/gts_score.
CREATE TABLE ml_feature_snapshots (
    id              BIGSERIAL PRIMARY KEY,
    user_id         UUID        NOT NULL,
    tenant_id       UUID        NOT NULL,
    year_month      VARCHAR(7)  NOT NULL,
    integration_level INT       NOT NULL DEFAULT 1,
    -- Pillar scores (0-100, from efficiency_snapshots)
    aiq_score  FLOAT NOT NULL DEFAULT 0,
    oss_score  FLOAT NOT NULL DEFAULT 0,
    gts_score  FLOAT NOT NULL DEFAULT 0,
    -- AIQ raw dimensions stored for future use (phase 2 model)
    aiq_output_density    FLOAT NOT NULL DEFAULT 0,
    aiq_usage_consistency FLOAT NOT NULL DEFAULT 0,
    aiq_task_depth        FLOAT NOT NULL DEFAULT 0,
    aiq_cost_efficiency   FLOAT NOT NULL DEFAULT 0,
    -- Labels
    label_composite FLOAT       NOT NULL DEFAULT 0,  -- v3 composite score (pseudo-label)
    label_source    VARCHAR(20) NOT NULL DEFAULT 'auto_v3',  -- 'auto_v3' | 'manager'
    label_manager   FLOAT,  -- NULL until a manager rating is provided
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, year_month)
);

-- Trained pillar weights: one active row per integration level.
-- Phase 1 learns only w_aiq/w_oss/w_gts; AIQ dimension weights remain v3 hardcoded.
-- Phase 2 can add dimension weight columns here without breaking phase 1 consumers.
CREATE TABLE ml_model_weights (
    id                BIGSERIAL PRIMARY KEY,
    integration_level INT     NOT NULL,
    -- Pillar weights (sum to 1.0)
    w_aiq  FLOAT NOT NULL,
    w_oss  FLOAT NOT NULL,
    w_gts  FLOAT NOT NULL,
    -- Metadata
    trained_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    n_samples      INT         NOT NULL,
    rmse           FLOAT       NOT NULL,
    model_version  VARCHAR(20) NOT NULL DEFAULT 'ridge_v1',
    is_active      BOOLEAN     NOT NULL DEFAULT TRUE
);
-- Only one active set of weights per level
CREATE UNIQUE INDEX ON ml_model_weights(integration_level) WHERE is_active = TRUE;
```

### Go: Feature Extraction Service

**New file:** `admin/services/ml_features.go`

```go
type MLFeatureService struct { pool *pgxpool.Pool }

// ExtractFeatures pulls the just-completed snapshot data for yearMonth
// and upserts one row per eligible user into ml_feature_snapshots.
// Called from RunMonthlySnapshot after snapshotting is complete.
func (s *MLFeatureService) ExtractFeatures(ctx context.Context, tenantID, yearMonth string) error
```

Features are read directly from `efficiency_snapshots` (already computed) and the raw AIQ metrics from `usage_records` (same query used in `GetRawMetrics`). The pseudo-label is the composite KPI score already stored in the snapshot.

### Go: Weight Loading

**Modified:** `admin/services/kpi.go`

`RunMonthlySnapshot` calls `loadMLWeights(ctx, level)` before computing scores:

```go
func (s *KPIService) loadMLWeights(ctx context.Context, level int) *adaptiveWeightSet {
    // SELECT w_aiq, w_oss, w_gts, w_output_density, ... FROM ml_model_weights
    // WHERE integration_level=$1 AND is_active=TRUE
    // Returns nil if no row found → caller falls back to hardcoded v3 weights
}
```

`AdaptiveWeights(level int)` signature is unchanged from the caller's perspective. Internally it checks the cached result of `loadMLWeights` first.

### Python: Training Script

**New files:** `ml/train.py`, `ml/requirements.txt`, `ml/Dockerfile`

`ml/requirements.txt`:
```
psycopg2-binary>=2.9
scikit-learn>=1.4
numpy>=1.26
```

`ml/train.py` logic (per integration level):
1. Read `ml_feature_snapshots` where `label_manager IS NOT NULL` (prefer manager labels), else use `label_composite`
2. Feature matrix `X`: `[aiq_score, oss_score, gts_score]` — three pillar scores only
3. Label vector `y`: chosen label (0–100)
4. Skip level if `n_samples < 30` (not enough data — keep hardcoded v3 weights for that level)
5. Train `Ridge(alpha=1.0)` — no StandardScaler needed since all features are already on the 0–100 scale
6. Extract 3 coefficients → clip negative to 0 (pillars must contribute positively) → normalize to sum 1.0
7. If any coefficient was clipped to 0, log a warning and skip that level (keeps v3 weights)
8. Deactivate old weights (`UPDATE ... SET is_active=FALSE WHERE integration_level=$1`)
9. Insert new row with `is_active=TRUE`, `n_samples`, `rmse`

**Why only pillar weights?** The AIQ algorithm has a two-level weight structure: AIQ-internal dimension weights (4 values) and pillar weights (3 values). A single Ridge regression over all 7 raw inputs would collapse this hierarchy, making coefficients uninterpretable. Phase 1 learns only the 3 pillar weights; AIQ dimension weights stay at v3 hardcoded values (0.30/0.30/0.25/0.15). Phase 2 can run a second inner regression on `[aiq_output_density … aiq_cost_efficiency]` against `aiq_score` to also learn dimension weights — the raw dimensions are stored in `ml_feature_snapshots` for this purpose.

**Why Ridge?** Produces smooth, interpretable weights even with small samples (< 200 rows).

### Docker Compose: ml-trainer

```yaml
# docker-compose.yml addition
ml-trainer:
  build: ./ml
  profiles: ["ml"]
  environment:
    DATABASE_URL: postgres://totra:totra@postgres:5432/totra
  command: python train.py
  depends_on:
    postgres:
      condition: service_healthy
```

Run manually after enough data has accumulated:
```bash
docker compose --profile ml run --rm ml-trainer
```

Or add to host crontab: `0 1 1 * * cd /path/to/totra && docker compose --profile ml run --rm ml-trainer`

---

## Approach B — FastAPI Microservice (Future Upgrade)

> Upgrade when: user count > 500, or daily weight refresh is needed, or an admin "Retrain Now" button is required.

### What changes from A → B

| Component | Approach A | Approach B |
|-----------|-----------|-----------|
| ML runs as | one-off Docker job (`profiles: ["ml"]`) | always-on container (`ml-service`) |
| Weights delivered via | Go reads Postgres | Go calls `GET /weights/{level}` |
| Trigger | manual / host cron | `POST /train` API + cron |
| Dashboard | none | "Retrain Model" button + training status |

### New files (B only)

**`ml/app.py`** — FastAPI wrapper:
```python
from fastapi import FastAPI
app = FastAPI()

@app.get("/health")
def health(): return {"ok": True}

@app.get("/weights/{level}")
def get_weights(level: int): ...  # reads ml_model_weights from Postgres

@app.post("/train")
def trigger_train(): ...  # runs train.py logic inline, returns {"rmse": ..., "n_samples": ...}
```

**`ml/requirements.txt`** additions:
```
fastapi>=0.111
uvicorn>=0.29
```

**`ml/Dockerfile`** CMD change:
```dockerfile
# A: CMD ["python", "train.py"]
# B:
CMD ["uvicorn", "ml.app:app", "--host", "0.0.0.0", "--port", "8090"]
```

### docker-compose.yml changes (B)

```yaml
ml-service:
  build: ./ml
  # Remove profiles: ["ml"]  ← always-on
  ports: ["8090:8090"]
  environment:
    DATABASE_URL: postgres://totra:totra@postgres:5432/totra
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:8090/health"]
    interval: 30s
```

### Go changes (B)

`admin/services/kpi.go`: `loadMLWeights` changes from Postgres query to HTTP call:

```go
func (s *KPIService) loadMLWeights(ctx context.Context, level int) *adaptiveWeightSet {
    // GET http://$ML_SERVICE_URL/weights/{level}
    // Unmarshal JSON → adaptiveWeightSet
    // On error → return nil (fallback to v3)
}
```

New env var: `ML_SERVICE_URL` (e.g., `http://ml-service:8090`). If unset, falls back to Postgres query (A-mode), allowing zero-downtime migration.

New admin API: `POST /api/admin/ml/train` — proxies to ML service `/train`. Admin-only.

New dashboard page: `MLStatusPage` — shows last trained date, n_samples, RMSE per level, "Retrain Now" button.

### Migration A → B checklist

- [ ] Add `fastapi`, `uvicorn` to `ml/requirements.txt`
- [ ] Create `ml/app.py` wrapping train logic
- [ ] Change `ml/Dockerfile` CMD to `uvicorn`
- [ ] Remove `profiles: ["ml"]` from docker-compose
- [ ] Set `ML_SERVICE_URL` env var in admin service
- [ ] `loadMLWeights` reads env var: if set → HTTP, else → Postgres
- [ ] Add `POST /api/admin/ml/train` endpoint
- [ ] Add `MLStatusPage` to dashboard

---

## Rollout Sequence

1. **Now (no users):** Implement A. Feature store is empty. Go falls back to v3 weights — no behavior change.
2. **First users (~10–50):** Data accumulates in `ml_feature_snapshots`. Still using v3 weights.
3. **After 3+ months, 30+ rows per level:** Run `ml-trainer` manually. Inspect RMSE. If reasonable, weights go live on next snapshot.
4. **500+ users, need daily updates:** Upgrade to B following the checklist above.
5. **Manager ratings available:** Update `label_source = 'manager'` rows. Retrain. Model shifts from self-supervised to supervised.

---

## Known Limitations

- **Phase 1 only learns pillar weights.** AIQ dimension weights (0.30/0.30/0.25/0.15) remain v3 hardcoded. Phase 2 adds a second inner Ridge over raw AIQ dimensions — raw values are already stored in `ml_feature_snapshots.aiq_*`.
- **Ridge assumes linearity.** Non-linear interactions (e.g., OSS × GTS) require gradient boosting (XGBoost/LightGBM). The `model_version` field supports this upgrade without schema changes.
- **`n_samples < 30` per level keeps v3 weights.** Small tenants won't benefit from ML individually — this is resolved by cross-tenant pooling (treated as a phase 2 enhancement; requires careful tenant anonymization).
- **Pseudo-label ceiling.** With `label_composite` as the training target, ML initially learns to reproduce v3, not to predict true productivity. Real improvement begins when `label_manager` rows are populated.
