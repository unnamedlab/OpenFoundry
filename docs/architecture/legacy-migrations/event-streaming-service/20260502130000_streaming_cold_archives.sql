CREATE TABLE IF NOT EXISTS streaming_cold_archives (
    id           UUID PRIMARY KEY,
    stream_id    UUID NOT NULL REFERENCES streaming_streams(id) ON DELETE CASCADE,
    snapshot_id  TEXT NOT NULL,
    last_offset  BIGINT NOT NULL,
    snapshot_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    dataset_id   UUID,
    parquet_path TEXT NOT NULL,
    row_count    BIGINT NOT NULL DEFAULT 0,
    bytes_on_disk BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_streaming_cold_archives_stream
    ON streaming_cold_archives (stream_id, snapshot_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_streaming_cold_archives_snapshot
    ON streaming_cold_archives (stream_id, snapshot_id);
