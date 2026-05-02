-- Bloque C — Checkpoints, semantics and stream profile.
--
-- C1 introduces the per-topology checkpoint bookkeeping table together
-- with the columns that drive checkpoint cadence, runtime selection
-- (builtin engine vs. Flink) and the consistency guarantee surfaced by
-- C4. C3 widens streaming_streams with the operator-facing
-- StreamProfile blob (high_throughput / compressed / partitions) which
-- maps onto Kafka producer tuning at runtime.

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

CREATE TABLE IF NOT EXISTS streaming_topology_checkpoints (
    id UUID PRIMARY KEY,
    topology_id UUID NOT NULL REFERENCES streaming_topologies(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'committed'
        CHECK (status IN ('pending', 'committed', 'failed', 'restored')),
    -- JSON map { "<stream_id>": <last_sequence_no:int8> } captured when
    -- the checkpoint barrier was injected. Used by reset to restore
    -- exactly the offsets that were durable at snapshot time.
    last_offsets JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Optional URI of the operator state blob materialised by the state
    -- backend (e.g. an S3 path of a RocksDB column-family snapshot).
    state_uri TEXT,
    -- Optional URI of an externally-owned savepoint, e.g. an Iceberg
    -- snapshot id or a Flink savepoint path. When set, takes precedence
    -- over `state_uri` for restore.
    savepoint_uri TEXT,
    trigger TEXT NOT NULL DEFAULT 'periodic'
        CHECK (trigger IN ('periodic', 'manual', 'pre-shutdown', 'on-failure')),
    duration_ms INTEGER NOT NULL DEFAULT 0 CHECK (duration_ms >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_streaming_topology_checkpoints_recent
    ON streaming_topology_checkpoints (topology_id, created_at DESC);
