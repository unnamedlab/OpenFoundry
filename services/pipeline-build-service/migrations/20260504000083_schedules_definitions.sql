-- Tarea 3.5 — Foundry-pattern orchestration.
--
-- Mirror of `libs/event-scheduler/migrations/0001_schedules_definitions.sql`
-- so that the pg-runtime-config `pipeline_authoring` database carries the
-- table the `schedules-tick` CronJob (binary from `libs/event-scheduler`)
-- reads on every minute. The schedule-service handlers in
-- `handlers/schedules_v2.rs` upsert one row per cron clause owned by a
-- pipeline schedule via `domain::cron_registrar`.
--
-- Concurrency-safety: two CronJob pods can race when a tick takes longer
-- than its period. The runner uses
-- `SELECT … FOR UPDATE SKIP LOCKED` over `enabled = true AND
-- next_run_at <= now()` so an overlapping run only sees the rows the
-- first one hasn't already claimed.

CREATE SCHEMA IF NOT EXISTS schedules;

CREATE TABLE IF NOT EXISTS schedules.definitions (
    id                uuid        PRIMARY KEY,
    name              text        NOT NULL UNIQUE,
    -- Raw cron expression, parsed with `scheduling-cron::parse_cron`.
    cron_expr         text        NOT NULL,
    -- 'unix5' (5-field) or 'quartz6' (6-field, seconds-prefixed).
    cron_flavor       text        NOT NULL DEFAULT 'unix5',
    -- IANA time zone, e.g. 'UTC' or 'Europe/Madrid'. Cron evaluates
    -- against wall clock in this zone (Foundry trigger semantics).
    time_zone         text        NOT NULL DEFAULT 'UTC',
    enabled           boolean     NOT NULL DEFAULT true,
    -- Kafka topic to publish to. The runner does NOT auto-create
    -- topics — provisioning is handled by the platform's topic
    -- registry (see `infra/helm/infra/kafka-cluster/values.yaml`).
    topic             text        NOT NULL,
    -- Verbatim JSON payload published as the Kafka record body.
    payload_template  jsonb       NOT NULL,
    -- UTC instant at which this schedule next fires. The runner
    -- advances this column to `next_fire_after(<row>, last_scheduled_for)`
    -- after each fire.
    next_run_at       timestamptz NOT NULL,
    -- Most recent UTC instant for which an event was emitted. NULL
    -- before the first fire.
    last_run_at       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- Hot path: every tick scans `enabled AND next_run_at <= now()`.
CREATE INDEX IF NOT EXISTS schedules_definitions_due_idx
    ON schedules.definitions (next_run_at)
    WHERE enabled;
