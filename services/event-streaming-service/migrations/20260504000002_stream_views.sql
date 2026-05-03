-- Foundry-parity Reset Stream support: rotated viewRid + history.
--
-- Foundry distinguishes a stream's stable RID from a rotating viewRid.
-- A "Reset stream" workflow clears the existing records and produces a
-- new viewRid; downstream push consumers must update their POST URL to
-- the new viewRid. Pipelines that consume the stream are expected to
-- replay against the new view.
--
-- The control plane stores every view that ever backed a stream so that
--   * the UI can render a "History" timeline (see streaming/[id]/+page),
--   * audits can prove which view absorbed which events,
--   * the push proxy can answer 404 PUSH_VIEW_RETIRED when a stale
--     POST URL hits the gateway.

ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'INGEST'
        CHECK (kind IN ('INGEST', 'DERIVED'));

CREATE TABLE IF NOT EXISTS streaming_stream_views (
    id          UUID PRIMARY KEY,
    stream_rid  TEXT NOT NULL,
    view_rid    TEXT NOT NULL UNIQUE,
    schema_json JSONB,
    config_json JSONB,
    generation  INTEGER NOT NULL,
    active      BOOLEAN NOT NULL DEFAULT true,
    created_by  TEXT NOT NULL DEFAULT 'system',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    retired_at  TIMESTAMPTZ,
    UNIQUE (stream_rid, generation),
    CHECK (generation >= 1),
    CHECK (
        (active = true  AND retired_at IS NULL) OR
        (active = false AND retired_at IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_streaming_stream_views_stream_rid
    ON streaming_stream_views (stream_rid);

CREATE INDEX IF NOT EXISTS idx_streaming_stream_views_active
    ON streaming_stream_views (stream_rid, active)
    WHERE active = true;

-- Backfill: every existing stream gets a generation-1 view that points
-- at the stream's own UUID. The push proxy will accept this view_rid
-- until the operator triggers a reset.
INSERT INTO streaming_stream_views (id, stream_rid, view_rid, schema_json, config_json, generation, active, created_by)
SELECT
    s.id,
    'ri.streams.main.stream.' || s.id::text,
    'ri.streams.main.view.'   || s.id::text,
    s.schema,
    jsonb_build_object(
        'stream_type',            s.stream_type,
        'compression',            s.compression,
        'partitions',             s.partitions,
        'retention_hours',        s.retention_hours,
        'ingest_consistency',     s.ingest_consistency,
        'pipeline_consistency',   s.pipeline_consistency,
        'checkpoint_interval_ms', s.checkpoint_interval_ms
    ),
    1,
    true,
    'migration'
FROM streaming_streams s
ON CONFLICT (view_rid) DO NOTHING;
