-- IRF-9: schema-registry persistence for streaming streams.
--
-- Mirrors services/ingestion-replication-service/migrations/streaming/
-- 20260502190000_streaming_schema_avro.sql. Adds the Avro projection
-- columns to streaming_streams and the streaming_stream_schema_history
-- audit table read by GET /streams/{id}/schema/history.

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
