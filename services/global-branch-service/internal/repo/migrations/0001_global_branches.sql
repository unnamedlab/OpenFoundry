-- GB Milestone A — global branch lifecycle + per-service participations.
--
-- Two tables:
--
--   global_branches              one row per cross-application branch
--   global_branch_participations one row per (branch, service) enrolment
--
-- The participations table has a UNIQUE (global_branch_id, service_name)
-- constraint so domain.ErrParticipationExists can be returned without
-- a SELECT-then-INSERT race.
--
-- The outbox schema lives in libs/outbox; this service ships its own
-- local outbox.events table so libs/audit-trail.EmitToOutbox can append
-- inside the same pgx.Tx as the state mutation (ADR-0022).

CREATE TABLE IF NOT EXISTS global_branches (
    id              uuid        PRIMARY KEY,
    tenant_id       uuid        NOT NULL,
    name            text        NOT NULL,
    base_ref        text        NOT NULL,
    status          text        NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'merging', 'merged', 'abandoned', 'stale')),
    description     text        NOT NULL DEFAULT '',
    created_by      uuid        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    merged_at       timestamptz,
    merged_by       uuid,
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_global_branches_tenant_status
    ON global_branches (tenant_id, status);

CREATE TABLE IF NOT EXISTS global_branch_participations (
    global_branch_id  uuid        NOT NULL REFERENCES global_branches(id) ON DELETE CASCADE,
    service_name      text        NOT NULL,
    local_branch_ref  text        NOT NULL,
    status            text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'active', 'merged', 'conflict')),
    last_synced_at    timestamptz,
    PRIMARY KEY (global_branch_id, service_name)
);

CREATE INDEX IF NOT EXISTS idx_global_branch_participations_service
    ON global_branch_participations (service_name);

-- Local outbox substrate so libs/audit-trail.EmitToOutbox can land
-- inside the same transaction as the state write (ADR-0022). Mirrors
-- the canonical layout owned by libs/outbox.
CREATE SCHEMA IF NOT EXISTS outbox;

CREATE TABLE IF NOT EXISTS outbox.events (
    event_id     uuid PRIMARY KEY,
    aggregate    text NOT NULL,
    aggregate_id text NOT NULL,
    payload      jsonb NOT NULL,
    headers      jsonb NOT NULL DEFAULT '{}'::jsonb,
    topic        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

DO $$ BEGIN ALTER TABLE outbox.events REPLICA IDENTITY FULL; EXCEPTION WHEN insufficient_privilege THEN NULL; END $$;
