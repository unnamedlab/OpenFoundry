-- Foundry-parity Streaming Profiles.
--
-- Profiles map onto Flink runtime knobs (resource sizing, parallelism,
-- network/checkpointing tuning, advanced overrides). Built-ins ship
-- with the platform; operators may author additional profiles via
-- Control Panel → Enrollment Settings → Streaming profiles.
--
-- Project references mirror the Foundry administrative control: a
-- streaming profile must be imported into a project before any
-- pipeline in that project may attach it. Restricted (LARGE) profiles
-- can only be imported by Enrollment Resource Administrators.

CREATE TABLE IF NOT EXISTS streaming_profiles (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    category    TEXT NOT NULL CHECK (category IN (
        'TASKMANAGER_RESOURCES',
        'JOBMANAGER_RESOURCES',
        'PARALLELISM',
        'NETWORK',
        'CHECKPOINTING',
        'ADVANCED'
    )),
    size_class  TEXT NOT NULL CHECK (size_class IN ('SMALL', 'MEDIUM', 'LARGE')),
    -- Restricted profiles require role 'enrollment_resource_administrator'
    -- to be imported into a project. LARGE defaults to restricted; the
    -- handler enforces this rule on POST/PATCH but the column can be
    -- toggled by an admin if a LARGE profile is intentionally opened up.
    restricted  BOOLEAN NOT NULL DEFAULT false,
    -- JSON object whose keys must be members of the Flink whitelist —
    -- see `services/event-streaming-service/README.md`. Validated at
    -- write time by `handlers::profiles::validate_config_keys`.
    config_json JSONB NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_by  TEXT NOT NULL DEFAULT 'system',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_streaming_profiles_category
    ON streaming_profiles (category);
CREATE INDEX IF NOT EXISTS idx_streaming_profiles_size
    ON streaming_profiles (size_class);

-- Project references — one row per (project, profile) pair. The
-- `imported_order` column is a per-project monotonic counter that the
-- effective-config resolver uses to break ties: when two profiles set
-- the same Flink key, the most-recently imported wins.
CREATE TABLE IF NOT EXISTS streaming_profile_project_refs (
    project_rid     TEXT NOT NULL,
    profile_id      UUID NOT NULL REFERENCES streaming_profiles(id) ON DELETE CASCADE,
    imported_by     TEXT NOT NULL DEFAULT 'system',
    imported_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    imported_order  BIGSERIAL NOT NULL,
    PRIMARY KEY (project_rid, profile_id)
);

CREATE INDEX IF NOT EXISTS idx_streaming_profile_refs_profile
    ON streaming_profile_project_refs (profile_id);

-- Pipeline associations. Each row promotes a profile to a pipeline's
-- effective config. The handler enforces that the profile has a
-- project ref in the pipeline's project at insert time.
CREATE TABLE IF NOT EXISTS streaming_pipeline_profiles (
    pipeline_rid    TEXT NOT NULL,
    profile_id      UUID NOT NULL REFERENCES streaming_profiles(id) ON DELETE CASCADE,
    attached_by     TEXT NOT NULL DEFAULT 'system',
    attached_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    attached_order  BIGSERIAL NOT NULL,
    PRIMARY KEY (pipeline_rid, profile_id)
);

CREATE INDEX IF NOT EXISTS idx_streaming_pipeline_profiles_pipeline
    ON streaming_pipeline_profiles (pipeline_rid);

-- Bootstrap built-in profiles (idempotent: re-running the migration is
-- a no-op for existing rows). UUIDs are stable so they can be
-- referenced from documentation and tests.
INSERT INTO streaming_profiles (
    id, name, description, category, size_class, restricted, config_json, created_by
) VALUES
    (
        '01968040-0850-7920-9000-00000000aaa1',
        'Default',
        'Foundry-default Flink configuration. Suitable for most pipelines.',
        'TASKMANAGER_RESOURCES',
        'SMALL',
        false,
        jsonb_build_object(
            'taskmanager.memory.process.size', '2048m',
            'taskmanager.numberOfTaskSlots', '2',
            'parallelism.default', '2'
        ),
        'system'
    ),
    (
        '01968040-0850-7920-9000-00000000aaa2',
        'High Parallelism',
        'Higher parallelism for high-throughput pipelines.',
        'PARALLELISM',
        'MEDIUM',
        false,
        jsonb_build_object(
            'parallelism.default', '8',
            'taskmanager.numberOfTaskSlots', '4'
        ),
        'system'
    ),
    (
        '01968040-0850-7920-9000-00000000aaa3',
        'Large State',
        'Larger TaskManager + RocksDB state backend for pipelines with very large state.',
        'TASKMANAGER_RESOURCES',
        'LARGE',
        true,
        jsonb_build_object(
            'taskmanager.memory.process.size', '8192m',
            'state.backend.type', 'rocksdb',
            'state.backend.incremental', 'true'
        ),
        'system'
    ),
    (
        '01968040-0850-7920-9000-00000000aaa4',
        'Large Records',
        'Bumps network buffer fractions for pipelines that ship very large Flink records.',
        'NETWORK',
        'LARGE',
        true,
        jsonb_build_object(
            'taskmanager.network.memory.fraction', '0.2',
            'taskmanager.network.memory.min', '256mb',
            'taskmanager.network.memory.max', '2gb'
        ),
        'system'
    )
ON CONFLICT (name) DO NOTHING;
