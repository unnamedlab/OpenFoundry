-- D1.1.2 Bloque B2 — cold-tier archive bookkeeping.
--
-- A row in `streaming_cold_archives` represents a single Parquet snapshot
-- that the archiver flushed from the hot buffer into Iceberg + the
-- data-asset-catalog. The archiver advances `last_offset` so the next
-- tick knows where to resume from.
--
-- Two columns are also added to `streaming_streams` so each stream can
-- override the global archiver cadence:
--   * `archive_interval_seconds` — how often the archiver task wakes up.
--   * `target_file_size_mb`      — soft target for Parquet file size.
ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS archive_interval_seconds INTEGER NOT NULL DEFAULT 120
        CHECK (archive_interval_seconds BETWEEN 5 AND 86400),
    ADD COLUMN IF NOT EXISTS target_file_size_mb INTEGER NOT NULL DEFAULT 128
        CHECK (target_file_size_mb BETWEEN 1 AND 4096);

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
