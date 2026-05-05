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
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cdc_streams_source
    ON cdc_streams (source_kind, source_ref);

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
