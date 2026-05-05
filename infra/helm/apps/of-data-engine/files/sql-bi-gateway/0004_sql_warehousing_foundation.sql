CREATE TABLE IF NOT EXISTS warehouse_jobs (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL,
    sql_text TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued',
    source_datasets JSONB NOT NULL DEFAULT '[]'::jsonb,
    target_dataset_id UUID NULL,
    target_storage_id UUID NULL,
    submitted_by UUID NULL,
    error_message TEXT NULL,
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_warehouse_jobs_status
    ON warehouse_jobs (status, created_at DESC);

CREATE TABLE IF NOT EXISTS warehouse_transformations (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    description TEXT NULL,
    sql_template TEXT NOT NULL,
    bindings JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS warehouse_storage_artifacts (
    id UUID PRIMARY KEY,
    job_id UUID NULL REFERENCES warehouse_jobs(id) ON DELETE SET NULL,
    slug TEXT NOT NULL,
    artifact_kind TEXT NOT NULL,
    storage_uri TEXT NOT NULL,
    byte_size BIGINT NULL,
    row_count BIGINT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_warehouse_storage_artifacts_job
    ON warehouse_storage_artifacts (job_id);
