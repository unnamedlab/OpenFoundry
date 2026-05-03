-- Bloque P6 — Foundry-parity streaming compute usage.
--
-- Mirrors the docs' "Streaming compute usage" page: every checkpoint
-- closes a window and bills compute = wall_time * task_slots, plus
-- the records processed in that window. Roll-up queries summarise the
-- table by hour or day for the Usage tab and the GET /usage endpoint.

CREATE TABLE IF NOT EXISTS stream_compute_usage (
    id                 UUID PRIMARY KEY,
    ts                 TIMESTAMPTZ NOT NULL DEFAULT now(),
    stream_rid         TEXT NOT NULL,
    topology_rid       TEXT,
    compute_seconds    DOUBLE PRECISION NOT NULL CHECK (compute_seconds >= 0),
    records_processed  BIGINT NOT NULL DEFAULT 0 CHECK (records_processed >= 0),
    partition          INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_stream_compute_usage_stream_ts
    ON stream_compute_usage (stream_rid, ts DESC);

CREATE INDEX IF NOT EXISTS idx_stream_compute_usage_topology_ts
    ON stream_compute_usage (topology_rid, ts DESC)
    WHERE topology_rid IS NOT NULL;
