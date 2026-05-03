-- P4 — Parameterized pipelines (Foundry doc § "Parameterized pipelines").
--
-- A parameterized pipeline runs the same DAG with different parameter
-- values, one *deployment* per value-set. Per the doc: "Automated
-- triggers are not yet supported", so every run is a manual dispatch
-- from a deployment. Outputs are unioned via a "Views" dataset
-- annotated with the deployment_key column the build pass writes.

CREATE TABLE IF NOT EXISTS parameterized_pipelines (
    id                       UUID PRIMARY KEY,
    pipeline_rid             TEXT NOT NULL UNIQUE,
    -- Name of the parameter that distinguishes deployments. Surfaced
    -- in the union view as the `_deployment_key` column.
    deployment_key_param     TEXT NOT NULL,
    -- The output datasets the parameterized run writes (each
    -- transaction is tagged with the deployment_key).
    output_dataset_rids      TEXT[] NOT NULL,
    -- Foundry "Views" dataset that aggregates every output across
    -- every deployment via a UNION ALL. Owned by
    -- dataset-versioning-service; the link is JSONB-free so the
    -- migration in that service can add foreign-key validation later.
    union_view_dataset_rid   TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS parameterized_pipelines_pipeline_idx
    ON parameterized_pipelines(pipeline_rid);

-- Per-deployment value-set. `deployment_key` is the value of
-- `deployment_key_param`; `parameter_values` is the full kwargs map
-- the build pass injects into JobExecutionContext.
CREATE TABLE IF NOT EXISTS pipeline_deployments (
    id                          UUID PRIMARY KEY,
    parameterized_pipeline_id   UUID NOT NULL REFERENCES parameterized_pipelines(id) ON DELETE CASCADE,
    deployment_key              TEXT NOT NULL,
    parameter_values            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by                  TEXT NOT NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (parameterized_pipeline_id, deployment_key)
);

CREATE INDEX IF NOT EXISTS pipeline_deployments_parameterized_idx
    ON pipeline_deployments(parameterized_pipeline_id);
