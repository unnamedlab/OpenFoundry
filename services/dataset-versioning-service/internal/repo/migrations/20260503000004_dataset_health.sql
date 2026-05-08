-- P6 — Foundry "Health checks" + "Data Health" surface.
--
-- The dataset-quality-service owns this aggregate table; the
-- compute path is invoked by:
--   * `POST /v1/datasets/{rid}/quality/profile`        (existing manual)
--   * `compute_health()` triggered by transaction commits
--     (Kafka `dataset.transaction.committed.v1` — TODO subscriber)
--   * polling fallback used by tests and the health-check sub-tab
--
-- Per-dataset row keyed on the textual RID so the UI can join with
-- the dataset row even when DVS lives in another database.

CREATE TABLE IF NOT EXISTS dataset_health (
    dataset_rid          TEXT PRIMARY KEY,
    dataset_id           UUID,
    -- Aggregate metrics. Re-computed end-to-end on each refresh; the
    -- UI rows hit this single SELECT.
    row_count            BIGINT NOT NULL DEFAULT 0,
    col_count            INTEGER NOT NULL DEFAULT 0,
    -- `null_pct` per column, as a JSONB map { column_name: 0.0..1.0 }.
    null_pct_by_column   JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- `now() - last_commit_at`. Refreshed on every compute_health()
    -- call so the UI badge can colour against the SLA.
    freshness_seconds    BIGINT NOT NULL DEFAULT 0,
    last_commit_at       TIMESTAMPTZ,
    -- Stored as DOUBLE PRECISION (instead of NUMERIC) so the handler
    -- can sqlx-bind a plain f64 without pulling the bigdecimal
    -- feature into the workspace.
    txn_failure_rate_24h DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_build_status    TEXT NOT NULL DEFAULT 'unknown'
                          CHECK (last_build_status IN ('success','failed','stale','unknown')),
    schema_drift_flag    BOOLEAN NOT NULL DEFAULT FALSE,
    -- Free-form payload for the dashboard cards (sparkline points,
    -- per-type failure breakdowns, …). The shape is documented in
    -- `services/dataset-quality-service/src/domain/health.rs`.
    extras               JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_computed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dataset_health_last_computed
    ON dataset_health(last_computed_at DESC);
CREATE INDEX IF NOT EXISTS idx_dataset_health_freshness
    ON dataset_health(freshness_seconds DESC);

-- Health-check policy table. Each policy targets a dataset by RID
-- (or all_datasets=true) and carries a kind + threshold. The runner
-- evaluates them on every `compute_health()` and emits an event when
-- a policy trips.
CREATE TABLE IF NOT EXISTS dataset_health_policies (
    id                  UUID PRIMARY KEY,
    name                TEXT NOT NULL,
    dataset_rid         TEXT,
    all_datasets        BOOLEAN NOT NULL DEFAULT FALSE,
    -- 'freshness' | 'txn_failure_rate' | 'schema_drift' | 'row_drop'
    check_kind          TEXT NOT NULL,
    -- Threshold expressed as JSON so each kind picks its own shape:
    --   freshness         → { "max_seconds": 3600 }
    --   txn_failure_rate  → { "max_rate":   0.05 }
    --   schema_drift      → { "block": true }
    --   row_drop          → { "max_drop_pct": 0.20 }
    threshold           JSONB NOT NULL DEFAULT '{}'::jsonb,
    severity            TEXT NOT NULL DEFAULT 'warning'
                          CHECK (severity IN ('info','warning','critical')),
    active              BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dataset_health_policies_dataset
    ON dataset_health_policies(dataset_rid);
CREATE INDEX IF NOT EXISTS idx_dataset_health_policies_active
    ON dataset_health_policies(active);
