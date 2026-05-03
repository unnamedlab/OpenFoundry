-- D1.1.2 Bloque B2 — archive control-plane knobs.
--
-- The legacy `streaming_cold_archives` table DDL now lives under
-- `docs/architecture/legacy-migrations/event-streaming-service/`
-- (`20260502130000_streaming_cold_archives.sql`). The active Postgres
-- chain only keeps the per-stream knobs that still belong to the control
-- plane.
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
