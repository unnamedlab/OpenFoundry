-- Foundry-parity Stream Monitoring (Bloque P4).
--
-- Three tables form the monitor surface the docs describe:
--
--   * `monitoring_views` group monitor rules. Each Foundry "Monitoring
--     View" lives in a project and is the unit of discovery in the
--     Data Health UI.
--   * `monitor_rules` are the typed alert rules: for a given
--     resource (streaming dataset, pipeline, time-series sync,
--     geotemporal observation dataset) and a given monitor kind
--     (INGEST_RECORDS, OUTPUT_RECORDS, CHECKPOINT_LIVENESS,
--     LAST_CHECKPOINT_DURATION, CHECKPOINT_TRIGGER_FAILURES,
--     CONSECUTIVE_CHECKPOINT_FAILURES, TOTAL_LAG, TOTAL_THROUGHPUT,
--     UTILIZATION, POINTS_WRITTEN_TO_TS, GEOTEMPORAL_OBS_SENT) the
--     evaluator compares the observed value against `threshold` over
--     `window_seconds`.
--   * `monitor_evaluations` is the audit trail: every scheduler tick
--     persists the observed value and whether the rule fired.
--     Dedup keys off the most-recent firing in the same window.

CREATE TABLE IF NOT EXISTS monitoring_views (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    project_rid TEXT NOT NULL,
    created_by  TEXT NOT NULL DEFAULT 'system',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_rid, name)
);

CREATE INDEX IF NOT EXISTS idx_monitoring_views_project
    ON monitoring_views (project_rid);

CREATE TABLE IF NOT EXISTS monitor_rules (
    id              UUID PRIMARY KEY,
    view_id         UUID NOT NULL REFERENCES monitoring_views(id) ON DELETE CASCADE,
    name            TEXT NOT NULL DEFAULT '',
    resource_type   TEXT NOT NULL CHECK (resource_type IN (
        'STREAMING_DATASET',
        'STREAMING_PIPELINE',
        'TIME_SERIES_SYNC',
        'GEOTEMPORAL_OBSERVATIONS'
    )),
    resource_rid    TEXT NOT NULL,
    monitor_kind    TEXT NOT NULL CHECK (monitor_kind IN (
        'INGEST_RECORDS',
        'OUTPUT_RECORDS',
        'CHECKPOINT_LIVENESS',
        'LAST_CHECKPOINT_DURATION',
        'CHECKPOINT_TRIGGER_FAILURES',
        'CONSECUTIVE_CHECKPOINT_FAILURES',
        'TOTAL_LAG',
        'TOTAL_THROUGHPUT',
        'UTILIZATION',
        'POINTS_WRITTEN_TO_TS',
        'GEOTEMPORAL_OBS_SENT'
    )),
    window_seconds  INTEGER NOT NULL CHECK (window_seconds BETWEEN 60 AND 86400),
    comparator      TEXT NOT NULL CHECK (comparator IN ('LT', 'LTE', 'GT', 'GTE', 'EQ')),
    threshold       DOUBLE PRECISION NOT NULL,
    severity        TEXT NOT NULL DEFAULT 'WARN'
        CHECK (severity IN ('INFO', 'WARN', 'CRITICAL')),
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_by      TEXT NOT NULL DEFAULT 'system',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_monitor_rules_view ON monitor_rules (view_id);
CREATE INDEX IF NOT EXISTS idx_monitor_rules_resource
    ON monitor_rules (resource_type, resource_rid);
CREATE INDEX IF NOT EXISTS idx_monitor_rules_enabled
    ON monitor_rules (enabled) WHERE enabled = true;

CREATE TABLE IF NOT EXISTS monitor_evaluations (
    id             UUID PRIMARY KEY,
    rule_id        UUID NOT NULL REFERENCES monitor_rules(id) ON DELETE CASCADE,
    evaluated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    observed_value DOUBLE PRECISION NOT NULL,
    fired          BOOLEAN NOT NULL,
    -- When `fired = true` and a notification was issued, this points
    -- at the notification id surfaced by `notification-alerting-service`
    -- so operators can correlate UI badges with the audit trail.
    alert_id       UUID
);

CREATE INDEX IF NOT EXISTS idx_monitor_evaluations_rule_time
    ON monitor_evaluations (rule_id, evaluated_at DESC);
CREATE INDEX IF NOT EXISTS idx_monitor_evaluations_fired
    ON monitor_evaluations (rule_id, evaluated_at DESC) WHERE fired = true;
