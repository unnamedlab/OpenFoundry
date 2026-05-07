-- FASE 4 / Tarea 4.2 — `reindex_coordinator` bounded context.
--
-- Owns the resume-cursor row that replaces the Temporal workflow
-- `ResumeToken` from the legacy Go `workers-go/reindex` worker
-- (ADR-0021 §Wire format, see
-- `docs/architecture/refactor/reindex-worker-inventory.md` §5).
--
-- One row per `(tenant_id, type_id?)` in-flight or terminal job.
-- A new `ontology.reindex.requested.v1` Kafka event always
-- INSERTs (idempotently — see `event_id`); the coordinator UPDATEs
-- `resume_token` after each successfully published batch and flips
-- `status` to `completed` / `failed` / `cancelled` on the terminal
-- transition.
--
-- The `resume_token` column is the same opaque base64-encoded
-- gocql `PageState` that the Go worker carried in workflow state
-- (`workers-go/reindex/activities/activities.go::encodePageState`),
-- so this row is binary-compatible across the cut-over.

CREATE TABLE IF NOT EXISTS reindex_coordinator.reindex_jobs (
    -- UUID v5 derived from `tenant_id || type_id`. Stable across
    -- producer redeliveries on `ontology.reindex.requested.v1`,
    -- so `INSERT … ON CONFLICT DO NOTHING RETURNING id` makes job
    -- creation idempotent.
    id              uuid        PRIMARY KEY,
    tenant_id       text        NOT NULL,
    -- Empty string ⇒ all-types scan (the `ALLOW FILTERING` path in
    -- `objects_by_type`). Stored as empty string rather than NULL
    -- so the `(tenant_id, type_id)` unique index treats the
    -- "all-types per tenant" job as a single row.
    type_id         text        NOT NULL DEFAULT '',
    status          text        NOT NULL CHECK (status IN (
                        'queued', 'running', 'completed', 'failed', 'cancelled'
                    )),
    -- Opaque base64 of the Cassandra `PageState`. NULL ⇒ start
    -- from the beginning (or job already complete).
    resume_token    text                 ,
    page_size       integer     NOT NULL DEFAULT 1000
                    CHECK (page_size > 0 AND page_size <= 10000),
    -- Cumulative counters across the whole job (every batch).
    scanned         bigint      NOT NULL DEFAULT 0 CHECK (scanned   >= 0),
    published       bigint      NOT NULL DEFAULT 0 CHECK (published >= 0),
    error           text                 ,
    started_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz          ,
    UNIQUE (tenant_id, type_id)
);

-- Restart-time recovery query: "give me every job that was running
-- when we crashed so we can resume". The coordinator drives the
-- catch-up loop off this index instead of replaying Kafka.
CREATE INDEX IF NOT EXISTS reindex_jobs_status_idx
    ON reindex_coordinator.reindex_jobs (status)
    WHERE status IN ('queued', 'running');

-- Per-batch idempotency. Same shape as `idem.processed_events`
-- (see `libs/idempotency/migrations/0001_processed_events.sql`)
-- but lives in this service's bounded-context schema so the
-- `svc_reindex_coordinator` role does not need cross-schema
-- privileges. The coordinator records here BEFORE producing each
-- batch with a deterministic `event_id = uuid_v5(tenant||type||token)`,
-- so a crash between "produce batch" and "update resume_token"
-- replays safely without double-publishing.
CREATE TABLE IF NOT EXISTS reindex_coordinator.processed_events (
    event_id     uuid        PRIMARY KEY,
    processed_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS processed_events_processed_at_idx
    ON reindex_coordinator.processed_events (processed_at);
