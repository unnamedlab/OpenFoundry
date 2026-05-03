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
