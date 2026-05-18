-- Append-only hot store for AiEventEnvelope records consumed from
-- ai.events.v1. Iceberg (`of_ai.{prompts,responses,evaluations,traces}`)
-- remains the cold analytic tier; this table is the queryable surface
-- served by the ai-sink HTTP API.
--
-- event_id is the deterministic UUID emitted by upstream AI producers;
-- a replay after a crash collapses to ON CONFLICT DO NOTHING. All four
-- envelope kinds live in one table — the `kind` column discriminates,
-- mirroring audit_events' single-table design.

CREATE TABLE IF NOT EXISTS ai_events (
    event_id        UUID PRIMARY KEY,
    at              TIMESTAMPTZ NOT NULL,
    kind            TEXT NOT NULL,
    run_id          UUID,
    trace_id        TEXT,
    producer        TEXT NOT NULL DEFAULT '',
    schema_version  INTEGER NOT NULL DEFAULT 0,
    payload         JSONB NOT NULL,
    envelope        JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ai_events_kind_at
    ON ai_events (kind, at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_events_run_at
    ON ai_events (run_id, at DESC) WHERE run_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ai_events_trace_at
    ON ai_events (trace_id, at DESC) WHERE trace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ai_events_producer_at
    ON ai_events (producer, at DESC);

CREATE INDEX IF NOT EXISTS idx_ai_events_created_at
    ON ai_events (created_at DESC, event_id);
