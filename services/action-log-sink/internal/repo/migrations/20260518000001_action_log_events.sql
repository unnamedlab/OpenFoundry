-- Append-only hot store for ActionEnvelope records consumed from
-- ontology.actions.applied.v1. Iceberg (`lakekeeper.default.action_log`)
-- remains the cold analytic tier; this table is the queryable surface
-- served by the action-log-sink HTTP API (`/api/v1/action-log/events*`).
--
-- event_id is the immutable identifier emitted by the publisher
-- (libs/ontology-kernel/handlers/actions/side_effects.go::publishActionAuditToKafka).
-- A replay after a crash collapses to ON CONFLICT DO NOTHING.

CREATE TABLE IF NOT EXISTS action_log_events (
    event_id              TEXT PRIMARY KEY,
    action_type_id        TEXT NOT NULL,
    action_name           TEXT NOT NULL,
    object_type_id        TEXT NOT NULL,
    object_id             TEXT NOT NULL DEFAULT '',
    tenant                TEXT NOT NULL,
    actor_sub             TEXT NOT NULL,
    actor_email           TEXT NOT NULL DEFAULT '',
    organization_id       TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL,
    parameters            JSONB,
    previous_state        JSONB,
    new_state             JSONB,
    target_classification TEXT NOT NULL DEFAULT '',
    applied_at_ms         BIGINT NOT NULL,
    kafka_ts              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_action_log_events_tenant_applied_at
    ON action_log_events (tenant, applied_at_ms DESC);

CREATE INDEX IF NOT EXISTS idx_action_log_events_actor_applied_at
    ON action_log_events (actor_sub, applied_at_ms DESC);

CREATE INDEX IF NOT EXISTS idx_action_log_events_object_type_applied_at
    ON action_log_events (object_type_id, applied_at_ms DESC);

CREATE INDEX IF NOT EXISTS idx_action_log_events_action_applied_at
    ON action_log_events (action_name, applied_at_ms DESC);

CREATE INDEX IF NOT EXISTS idx_action_log_events_applied_at_event_id
    ON action_log_events (applied_at_ms DESC, event_id DESC);
