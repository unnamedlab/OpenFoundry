-- Foundry-parity Schedules persistence (P1 of pipeline-schedule-service
-- triggers redesign). Owns the declarative source of truth for every
-- schedule. The legacy `workflows.trigger_type='cron'` rows are migrated
-- into `schedules` in-place at the bottom of this file; the `workflows`
-- table itself stays as compat read-only.
--
-- See:
--   docs_original_palantir_foundry/.../Core concepts/Schedules.md
--   docs_original_palantir_foundry/.../Scheduling/Trigger types reference.md

-- ---------------------------------------------------------------------------
-- schedules: one row per schedule definition. RID format mirrors the
-- rest of Foundry: `ri.foundry.main.schedule.<uuid>`.
-- `trigger_json` and `target_json` serialise the corresponding proto
-- messages defined in `proto/pipeline/schedules.proto`. Storing them as
-- JSONB keeps the column count stable while the trigger model evolves.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schedules (
    id            UUID PRIMARY KEY,
    rid           TEXT UNIQUE GENERATED ALWAYS AS
                     ('ri.foundry.main.schedule.' || id::text) STORED,
    project_rid   TEXT NOT NULL,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    trigger_json  JSONB NOT NULL,
    target_json   JSONB NOT NULL,
    paused        BOOLEAN NOT NULL DEFAULT FALSE,
    version       INT NOT NULL DEFAULT 1,
    created_by    TEXT NOT NULL DEFAULT 'system',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_run_at   TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_schedules_project ON schedules(project_rid);
CREATE INDEX IF NOT EXISTS idx_schedules_paused  ON schedules(paused);
CREATE INDEX IF NOT EXISTS idx_schedules_name    ON schedules(name);

-- ---------------------------------------------------------------------------
-- schedule_versions: append-only history of every (trigger | target |
-- name | description) edit, surfaced by the view-modify-schedules UI.
-- A trigger snapshots the previous state immediately before an UPDATE.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schedule_versions (
    id           UUID PRIMARY KEY,
    schedule_id  UUID NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    version      INT NOT NULL,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL,
    trigger_json JSONB NOT NULL,
    target_json  JSONB NOT NULL,
    edited_by    TEXT NOT NULL DEFAULT 'system',
    edited_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    comment      TEXT NOT NULL DEFAULT '',
    UNIQUE (schedule_id, version)
);

CREATE INDEX IF NOT EXISTS idx_schedule_versions_schedule
    ON schedule_versions(schedule_id, version DESC);

-- The trigger fires BEFORE UPDATE on schedules and records the previous
-- state if any of (name, description, trigger_json, target_json)
-- actually changed. The new row's `version` is bumped by the caller as
-- part of the same statement.
CREATE OR REPLACE FUNCTION schedules_snapshot_previous_version()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF (NEW.name        IS DISTINCT FROM OLD.name)
    OR (NEW.description IS DISTINCT FROM OLD.description)
    OR (NEW.trigger_json IS DISTINCT FROM OLD.trigger_json)
    OR (NEW.target_json  IS DISTINCT FROM OLD.target_json) THEN
        INSERT INTO schedule_versions (
            id, schedule_id, version,
            name, description, trigger_json, target_json,
            edited_by, edited_at, comment
        ) VALUES (
            gen_random_uuid(), OLD.id, OLD.version,
            OLD.name, OLD.description, OLD.trigger_json, OLD.target_json,
            COALESCE(current_setting('app.editor', true), 'system'),
            NOW(),
            COALESCE(current_setting('app.change_comment', true), '')
        );
        NEW.updated_at := NOW();
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS schedules_version_snapshot ON schedules;
CREATE TRIGGER schedules_version_snapshot
    BEFORE UPDATE ON schedules
    FOR EACH ROW
    EXECUTE FUNCTION schedules_snapshot_previous_version();

-- ---------------------------------------------------------------------------
-- schedule_event_observations: state of every Event/Compound trigger
-- between runs. The trigger evaluator inserts on observation and the
-- run dispatcher deletes (per the Foundry doc: "an event trigger
-- remains satisfied … until the entire trigger is satisfied and the
-- schedule is run").
--
-- `trigger_path` distinguishes which leaf of a CompoundTrigger the
-- observation belongs to (e.g. "compound[0].event" or just "event").
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schedule_event_observations (
    schedule_id          UUID NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    trigger_path         TEXT NOT NULL,
    observed_event_type  TEXT NOT NULL,
    observed_target_rid  TEXT NOT NULL,
    observed_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (schedule_id, trigger_path, observed_target_rid, observed_at)
);

CREATE INDEX IF NOT EXISTS idx_schedule_event_observations_schedule
    ON schedule_event_observations(schedule_id);
CREATE INDEX IF NOT EXISTS idx_schedule_event_observations_target
    ON schedule_event_observations(observed_target_rid, observed_event_type);

-- ---------------------------------------------------------------------------
-- Legacy migration: copy every `workflows` row whose trigger_type is
-- 'cron' into `schedules`, mapping its cron expression into a Time
-- trigger and its target into a PipelineBuildTarget. The `workflows`
-- table itself is left intact for read-only compat with old clients;
-- writes from this point on go through the new contract.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'workflows'
    ) THEN
        INSERT INTO schedules (
            id, project_rid, name, description,
            trigger_json, target_json, paused,
            created_by, created_at, updated_at
        )
        SELECT
            COALESCE(w.id, gen_random_uuid()),
            'ri.foundry.main.project.legacy-cron',
            w.name,
            COALESCE(w.description, ''),
            jsonb_build_object(
                'kind',
                jsonb_build_object(
                    'time',
                    jsonb_build_object(
                        'cron',     COALESCE(w.trigger_config ->> 'cron', '0 * * * *'),
                        'time_zone', COALESCE(w.trigger_config ->> 'time_zone', 'UTC'),
                        'flavor',    'UNIX_5'
                    )
                )
            ),
            jsonb_build_object(
                'kind',
                jsonb_build_object(
                    'pipeline_build',
                    jsonb_build_object(
                        'pipeline_rid', 'ri.foundry.main.pipeline.' || w.id::text,
                        'build_branch', 'master'
                    )
                )
            ),
            COALESCE(w.status = 'paused', FALSE),
            'legacy-migration',
            COALESCE(w.created_at, NOW()),
            COALESCE(w.updated_at, NOW())
        FROM workflows w
        WHERE w.trigger_type = 'cron'
        ON CONFLICT (id) DO NOTHING;
    END IF;
END $$;
