-- Drop KPI algorithm tables — direction pivoted to compliance + cost optimization.
-- fuel_transactions must be dropped before efficiency_snapshots (FK ref_snapshot_id).
DROP TABLE IF EXISTS fuel_transactions;
DROP TABLE IF EXISTS fuel_settings;
DROP TABLE IF EXISTS efficiency_snapshots;
DROP TABLE IF EXISTS ml_feature_snapshots;
DROP TABLE IF EXISTS ml_model_weights;
DROP TABLE IF EXISTS industry_benchmarks;
