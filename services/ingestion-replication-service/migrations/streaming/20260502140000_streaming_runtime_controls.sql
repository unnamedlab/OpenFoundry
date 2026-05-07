-- Bloque C — Checkpoints, semantics and stream profile.
--
-- The legacy `streaming_topology_checkpoints` table DDL now lives under
-- `docs/architecture/legacy-migrations/event-streaming-service/`
-- (`20260502140000_streaming_topology_checkpoints.sql`). The active
-- Postgres chain only keeps the topology/runtime knobs that still belong
-- to the control plane.
--
-- C1 adds the columns that drive checkpoint cadence, runtime selection
-- (builtin engine vs. Flink) and the consistency guarantee surfaced by
-- C4. C3 widens streaming_streams with the operator-facing StreamProfile
-- blob (high_throughput / compressed / partitions) which maps onto
-- Kafka producer tuning at runtime.

ALTER TABLE streaming_topologies
    ADD COLUMN IF NOT EXISTS checkpoint_interval_ms INTEGER NOT NULL DEFAULT 60000
        CHECK (checkpoint_interval_ms BETWEEN 1000 AND 86400000),
    ADD COLUMN IF NOT EXISTS runtime_kind TEXT NOT NULL DEFAULT 'builtin'
        CHECK (runtime_kind IN ('builtin', 'flink')),
    ADD COLUMN IF NOT EXISTS flink_job_name TEXT,
    ADD COLUMN IF NOT EXISTS consistency_guarantee TEXT NOT NULL DEFAULT 'at-least-once'
        CHECK (consistency_guarantee IN ('at-most-once', 'at-least-once', 'exactly-once'));

ALTER TABLE streaming_streams
    ADD COLUMN IF NOT EXISTS stream_profile JSONB NOT NULL
        DEFAULT '{"high_throughput": false, "compressed": false, "partitions": 3}'::jsonb;
