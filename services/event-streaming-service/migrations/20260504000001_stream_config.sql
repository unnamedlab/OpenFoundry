-- Foundry-parity stream config columns.
--
-- Backs `proto/streaming/streams.proto::StreamConfig` so the control
-- plane can persist Stream type / compression / partitions /
-- ingest+pipeline consistency / checkpoint cadence directly on the
-- `streaming_streams` row instead of stuffing them into
-- `stream_profile` JSON.
--
-- Constraints follow the documented Foundry behaviour:
--   * partitions limited to 1..=50 (matches the docs throughput slider
--     and the ~5 MB/s per partition heuristic).
--   * `ingest_consistency` is restricted to AT_LEAST_ONCE — Foundry
--     streaming sources do not support EXACTLY_ONCE for extracts and
--     exports. Pipelines, however, do, hence the open enum on
--     `pipeline_consistency`.
ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS stream_type TEXT NOT NULL DEFAULT 'STANDARD'
        CHECK (stream_type IN (
            'STANDARD',
            'HIGH_THROUGHPUT',
            'COMPRESSED',
            'HIGH_THROUGHPUT_COMPRESSED'
        )),
    ADD COLUMN IF NOT EXISTS compression BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS ingest_consistency TEXT NOT NULL DEFAULT 'AT_LEAST_ONCE'
        CHECK (ingest_consistency IN ('AT_LEAST_ONCE')),
    ADD COLUMN IF NOT EXISTS pipeline_consistency TEXT NOT NULL DEFAULT 'AT_LEAST_ONCE'
        CHECK (pipeline_consistency IN ('AT_LEAST_ONCE', 'EXACTLY_ONCE')),
    ADD COLUMN IF NOT EXISTS checkpoint_interval_ms INTEGER NOT NULL DEFAULT 2000
        CHECK (checkpoint_interval_ms BETWEEN 100 AND 86400000);

-- The legacy `partitions` column was created in
-- `20260502120000_streaming_hot_buffer.sql` with `BETWEEN 1 AND 1024`.
-- Tighten it to the Foundry-documented range (1..=50) since Kafka
-- topics for stream hot buffers should not grow unbounded — operators
-- who really need >50 partitions can lift this constraint with a follow
-- up migration.
ALTER TABLE streaming_streams
    DROP CONSTRAINT IF EXISTS streaming_streams_partitions_check;
ALTER TABLE streaming_streams
    ADD CONSTRAINT streaming_streams_partitions_check
        CHECK (partitions BETWEEN 1 AND 50);

-- Default new streams to the Foundry-recommended 3 partitions but
-- clamp existing rows above 50 so the new constraint does not fail.
UPDATE streaming_streams
   SET partitions = 50
 WHERE partitions > 50;
