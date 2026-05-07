-- ADR-0037 — Foundry-pattern orchestration / Tarea 1.3.
--
-- Time-based triggers ("Foundry Schedule"): a single K8s CronJob runs
-- every minute, calls `Scheduler::tick(Utc::now())`, and emits one
-- Kafka event per schedule whose `next_run_at` has elapsed.
--
-- Concurrency-safety: two CronJob pods can race when a tick takes
-- longer than its period. The runner uses
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
    -- IANA time zone, e.g. 'UTC' or 'America/New_York'. Cron evaluates
    -- against wall clock in this zone (Foundry trigger semantics).
    time_zone         text        NOT NULL DEFAULT 'UTC',
    enabled           boolean     NOT NULL DEFAULT true,
    -- Kafka topic to publish to. The runner does NOT auto-create
    -- topics — provisioning is handled by the platform's topic
    -- registry, mirroring the rest of `event-bus-data`.
    topic             text        NOT NULL,
    -- Verbatim JSON payload published as the Kafka record body.
    payload_template  jsonb       NOT NULL,
    -- UTC instant at which this schedule next fires. Bootstrap by
    -- inserting `now()` (fire on the next tick) or a future instant
    -- (delay the first fire). The runner advances this column to
    -- `next_fire_after(<row>, last_scheduled_for)` after each fire.
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
