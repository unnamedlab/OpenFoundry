-- Minimal Go vertical for streaming source CRUD/config plus CDC metadata
-- projection. Heavy Kafka/Flink runtime is intentionally outside Postgres
-- and driven via application interfaces.

CREATE TABLE IF NOT EXISTS streaming_streams (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active',
    schema JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_binding JSONB NOT NULL DEFAULT '{}'::jsonb,
    retention_hours INTEGER NOT NULL DEFAULT 72,
    partitions INTEGER NOT NULL DEFAULT 3,
    consistency_guarantee TEXT NOT NULL DEFAULT 'at-least-once',
    stream_type TEXT NOT NULL DEFAULT 'STANDARD'
        CHECK (stream_type IN ('STANDARD','HIGH_THROUGHPUT','COMPRESSED','HIGH_THROUGHPUT_COMPRESSED')),
    compression BOOLEAN NOT NULL DEFAULT FALSE,
    ingest_consistency TEXT NOT NULL DEFAULT 'AT_LEAST_ONCE'
        CHECK (ingest_consistency IN ('AT_LEAST_ONCE','EXACTLY_ONCE')),
    pipeline_consistency TEXT NOT NULL DEFAULT 'AT_LEAST_ONCE'
        CHECK (pipeline_consistency IN ('AT_LEAST_ONCE','EXACTLY_ONCE')),
    checkpoint_interval_ms INTEGER NOT NULL DEFAULT 2000,
    kind TEXT NOT NULL DEFAULT 'INGEST',
    owner_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_streaming_streams_owner
    ON streaming_streams(owner_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_streaming_streams_status
    ON streaming_streams(status);

CREATE TABLE IF NOT EXISTS cdc_streams (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    source_kind TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    upstream_topic TEXT NULL,
    primary_keys JSONB NOT NULL DEFAULT '[]'::jsonb,
    watermark_column TEXT NULL,
    incremental_mode TEXT NOT NULL DEFAULT 'log_based',
    status TEXT NOT NULL DEFAULT 'registered',
    owner_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cdc_streams_owner
    ON cdc_streams(owner_id, slug);
CREATE INDEX IF NOT EXISTS idx_cdc_streams_source
    ON cdc_streams(source_kind, source_ref);

CREATE TABLE IF NOT EXISTS cdc_incremental_checkpoints (
    stream_id UUID PRIMARY KEY REFERENCES cdc_streams(id) ON DELETE CASCADE,
    last_offset TEXT NULL,
    last_lsn TEXT NULL,
    last_event_at TIMESTAMPTZ NULL,
    records_observed BIGINT NOT NULL DEFAULT 0,
    records_applied BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS cdc_resolution_state (
    stream_id UUID PRIMARY KEY REFERENCES cdc_streams(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'lagging',
    watermark TIMESTAMPTZ NULL,
    conflict_count BIGINT NOT NULL DEFAULT 0,
    pending_resolutions BIGINT NOT NULL DEFAULT 0,
    notes TEXT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
