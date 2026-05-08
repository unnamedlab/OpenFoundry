-- entity-resolution-service / fusion_base — jobs + clusters + review queue
-- + golden records. Mirrors what the Rust handlers expect on PostgreSQL.

CREATE TABLE IF NOT EXISTS fusion_jobs (
    id                  UUID PRIMARY KEY,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'draft',
    entity_type         TEXT NOT NULL DEFAULT 'person',
    match_rule_id       UUID NOT NULL,
    merge_strategy_id   UUID NOT NULL,
    config              JSONB NOT NULL,
    metrics             JSONB NOT NULL,
    last_run_summary    TEXT NOT NULL DEFAULT 'Not run yet',
    last_run_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fusion_jobs_updated_at
  ON fusion_jobs (updated_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_fusion_jobs_status
  ON fusion_jobs (status);

CREATE TABLE IF NOT EXISTS fusion_clusters (
    id                            UUID PRIMARY KEY,
    job_id                        UUID NOT NULL,
    cluster_key                   TEXT NOT NULL,
    status                        TEXT NOT NULL,
    records                       JSONB NOT NULL,
    evidence                      JSONB NOT NULL,
    confidence_score              REAL NOT NULL,
    requires_review               BOOLEAN NOT NULL DEFAULT FALSE,
    suggested_golden_record_id    UUID,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fusion_clusters_updated_at
  ON fusion_clusters (updated_at DESC, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_fusion_clusters_job_id
  ON fusion_clusters (job_id);

CREATE INDEX IF NOT EXISTS idx_fusion_clusters_status
  ON fusion_clusters (status);

CREATE TABLE IF NOT EXISTS fusion_review_queue (
    id                  UUID PRIMARY KEY,
    cluster_id          UUID NOT NULL,
    status              TEXT NOT NULL DEFAULT 'pending',
    severity            TEXT NOT NULL DEFAULT 'medium',
    recommended_action  TEXT NOT NULL DEFAULT 'manual_review',
    rationale           JSONB NOT NULL,
    assigned_to         TEXT,
    reviewed_by         TEXT,
    notes               TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fusion_review_queue_cluster_id
  ON fusion_review_queue (cluster_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_fusion_review_queue_status
  ON fusion_review_queue (status);

CREATE TABLE IF NOT EXISTS fusion_golden_records (
    id                   UUID PRIMARY KEY,
    cluster_id           UUID NOT NULL,
    title                TEXT NOT NULL,
    canonical_values     JSONB NOT NULL,
    provenance           JSONB NOT NULL,
    completeness_score   REAL NOT NULL,
    confidence_score     REAL NOT NULL,
    status               TEXT NOT NULL DEFAULT 'active',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fusion_golden_records_cluster_id
  ON fusion_golden_records (cluster_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_fusion_golden_records_updated_at
  ON fusion_golden_records (updated_at DESC, created_at DESC);
