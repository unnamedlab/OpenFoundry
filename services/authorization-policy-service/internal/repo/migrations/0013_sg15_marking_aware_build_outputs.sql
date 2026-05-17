-- 0013: SG.15 - Marking-aware builds and output diffs.
--
-- Build/output publication records capture the security diff produced
-- when lineage inputs are attached to output resources. These records
-- are the local transaction/build view for marking additions,
-- removals, and blocked declassification attempts.

CREATE TABLE IF NOT EXISTS resource_marking_build_events (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NULL,
    build_id             TEXT NOT NULL DEFAULT '',
    transaction_id       TEXT NOT NULL DEFAULT '',
    output_resource_kind TEXT NOT NULL,
    output_resource_id   TEXT NOT NULL,
    actor_id             UUID NOT NULL,
    status               TEXT NOT NULL,
    reason               TEXT NOT NULL DEFAULT '',
    input_resources      JSONB NOT NULL DEFAULT '[]'::jsonb,
    before_state         JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_state          JSONB NOT NULL DEFAULT '{}'::jsonb,
    diff                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT resource_marking_build_status_check
        CHECK (status IN ('applied', 'blocked', 'dry_run')),
    CONSTRAINT resource_marking_build_inputs_array_check
        CHECK (jsonb_typeof(input_resources) = 'array'),
    CONSTRAINT resource_marking_build_before_object_check
        CHECK (jsonb_typeof(before_state) = 'object'),
    CONSTRAINT resource_marking_build_after_object_check
        CHECK (jsonb_typeof(after_state) = 'object'),
    CONSTRAINT resource_marking_build_diff_object_check
        CHECK (jsonb_typeof(diff) = 'object'),
    CONSTRAINT resource_marking_build_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS resource_marking_build_events_output_idx
    ON resource_marking_build_events (output_resource_kind, output_resource_id, created_at DESC);

CREATE INDEX IF NOT EXISTS resource_marking_build_events_build_idx
    ON resource_marking_build_events (build_id, created_at DESC)
    WHERE build_id <> '';

CREATE INDEX IF NOT EXISTS resource_marking_build_events_transaction_idx
    ON resource_marking_build_events (transaction_id, created_at DESC)
    WHERE transaction_id <> '';
