ALTER TABLE datasets
    ADD COLUMN IF NOT EXISTS active_branch TEXT NOT NULL DEFAULT 'main';

-- Ownership note (dataset versioning / Iceberg consolidation):
-- `dataset_branches` is runtime-owned by `dataset-versioning-service`.
-- The catalog keeps this DDL only as a temporary compatibility bridge;
-- no new branch lifecycle logic should be added here.
CREATE TABLE IF NOT EXISTS dataset_branches (
    id          UUID PRIMARY KEY,
    dataset_id  UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    version     INT NOT NULL DEFAULT 1,
    description TEXT NOT NULL DEFAULT '',
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(dataset_id, name)
);

CREATE INDEX IF NOT EXISTS idx_dataset_branches_dataset ON dataset_branches(dataset_id);
