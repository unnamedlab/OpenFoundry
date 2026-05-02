-- Bloque E2 — Avro schema persistence + history.
--
-- We keep the existing JSONB `schema` column (legacy structural
-- representation used by `push_events`) and add a parallel `schema_avro`
-- column that holds the canonical Avro JSON document. When present the
-- push path validates against the Avro schema using the shared
-- `event-bus-control::schema_registry` helpers.
--
-- `streaming_stream_schema_history` records every accepted schema
-- evolution so operators can audit changes and run the
-- `_validate` endpoint against a specific historical version.

ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS schema_avro JSONB,
    ADD COLUMN IF NOT EXISTS schema_fingerprint TEXT,
    ADD COLUMN IF NOT EXISTS schema_compatibility_mode TEXT NOT NULL DEFAULT 'BACKWARD';

CREATE TABLE IF NOT EXISTS streaming_stream_schema_history (
    id              UUID PRIMARY KEY,
    stream_id       UUID NOT NULL REFERENCES streaming_streams(id) ON DELETE CASCADE,
    version         INTEGER NOT NULL,
    schema_avro     JSONB NOT NULL,
    fingerprint     TEXT NOT NULL,
    compatibility   TEXT NOT NULL DEFAULT 'BACKWARD',
    created_by      TEXT NOT NULL DEFAULT 'system',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (stream_id, version)
);

CREATE INDEX IF NOT EXISTS idx_streaming_stream_schema_history_stream
    ON streaming_stream_schema_history (stream_id, version DESC);
