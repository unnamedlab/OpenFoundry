-- Bloque E1 — Stream branching.
--
-- Branches let operators isolate experimental writes from the main
-- timeline of a stream. Each branch has its own monotonic sequence
-- counter (`head_sequence_no`) and an optional pointer back to a
-- dataset-versioning branch for cold-storage materialisation.

CREATE TABLE IF NOT EXISTS streaming_stream_branches (
    id                  UUID PRIMARY KEY,
    stream_id           UUID NOT NULL REFERENCES streaming_streams(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    parent_branch_id    UUID REFERENCES streaming_stream_branches(id) ON DELETE SET NULL,
    status              TEXT NOT NULL DEFAULT 'active',
    head_sequence_no    BIGINT NOT NULL DEFAULT 0,
    dataset_branch_id   TEXT,
    description         TEXT NOT NULL DEFAULT '',
    created_by          TEXT NOT NULL DEFAULT 'system',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at         TIMESTAMPTZ,
    UNIQUE (stream_id, name)
);

CREATE INDEX IF NOT EXISTS idx_streaming_stream_branches_stream
    ON streaming_stream_branches (stream_id);
CREATE INDEX IF NOT EXISTS idx_streaming_stream_branches_status
    ON streaming_stream_branches (status)
    WHERE status <> 'archived';
