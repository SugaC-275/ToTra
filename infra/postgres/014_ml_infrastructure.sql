-- infra/postgres/014_ml_infrastructure.sql
-- Feature store: one row per user per month.
-- Phase 1 training uses aiq_score/oss_score/gts_score (pillar scores).
-- aiq_* raw dimensions are stored for future phase 2 model (dimension weight learning).
CREATE TABLE ml_feature_snapshots (
    id                    BIGSERIAL    PRIMARY KEY,
    user_id               UUID         NOT NULL,
    tenant_id             UUID         NOT NULL,
    year_month            CHAR(7)      NOT NULL,
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
    UNIQUE(tenant_id, user_id, year_month)
);

CREATE INDEX idx_ml_feature_snapshots_level ON ml_feature_snapshots (integration_level);
CREATE INDEX idx_ml_feature_snapshots_tenant_month ON ml_feature_snapshots (tenant_id, year_month);

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
    model_version     TEXT         NOT NULL DEFAULT 'ridge_v1',
    is_active         BOOLEAN      NOT NULL DEFAULT TRUE
);

-- Partial unique index: only one active weight row per level.
CREATE UNIQUE INDEX idx_ml_model_weights_active_level ON ml_model_weights(integration_level) WHERE is_active = TRUE;
