-- P4 — Parameterized pipeline support in dataset-versioning-service.
-- Foundry doc § "Parameterized pipelines" describes a single union
-- view dataset whose preview is `UNION ALL` over the per-deployment
-- transactions, augmented with a `_deployment_key` column carrying
-- the run's deployment value.
--
-- Two new columns on `dataset_transactions` capture which
-- parameterized pipeline + deployment a given transaction belongs to.
-- Both are nullable so non-parameterized transactions are unaffected.

ALTER TABLE dataset_transactions
    ADD COLUMN IF NOT EXISTS deployment_key             TEXT NULL,
    ADD COLUMN IF NOT EXISTS parameterized_pipeline_id  UUID NULL;

CREATE INDEX IF NOT EXISTS dataset_transactions_parameterized_idx
    ON dataset_transactions(parameterized_pipeline_id, deployment_key)
    WHERE parameterized_pipeline_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- parameterized_union_views — registry of "Views" datasets that
-- materialise the union of a parameterized pipeline's outputs. The
-- `output_dataset_rids` column mirrors the per-pipeline list owned by
-- pipeline-authoring-service so this service can render previews
-- without an out-of-band join.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS parameterized_union_views (
    union_view_dataset_rid     TEXT PRIMARY KEY,
    parameterized_pipeline_id  UUID NOT NULL,
    output_dataset_rids        TEXT[] NOT NULL,
    deployment_key_param       TEXT NOT NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS parameterized_union_views_pipeline_idx
    ON parameterized_union_views(parameterized_pipeline_id);
