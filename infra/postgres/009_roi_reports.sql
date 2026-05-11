BEGIN;

CREATE TABLE IF NOT EXISTS industry_benchmarks (
    id         SERIAL PRIMARY KEY,
    label      TEXT NOT NULL DEFAULT 'software' UNIQUE,
    p25        FLOAT NOT NULL,
    p50        FLOAT NOT NULL,
    p75        FLOAT NOT NULL,
    p90        FLOAT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO industry_benchmarks (label, p25, p50, p75, p90)
VALUES ('software', 0.05, 0.12, 0.25, 0.45)
ON CONFLICT (label) DO NOTHING;

COMMIT;
