-- Object-type ↔ dataset bindings.
--
-- A binding declares that an ObjectType is materialised from one or more
-- physical datasets. It is the Foundry "Models in the Ontology" primitive:
-- one or more dataset rows projected into ObjectType instances using a
-- property mapping and a primary-key column.
--
-- sync_mode semantics:
--   snapshot    – every materialisation truncates + reinserts (full reload).
--   incremental – upsert by primary key (insert new, update existing).
--   view        – do not write rows; the binding is metadata-only and the
--                 read-side serves data lazily from the dataset.
CREATE TABLE IF NOT EXISTS object_type_bindings (
    id                    UUID         PRIMARY KEY,
    object_type_id        UUID         NOT NULL REFERENCES object_types(id) ON DELETE CASCADE,
    dataset_id            UUID         NOT NULL,
    dataset_branch        TEXT,
    dataset_version       INTEGER,
    primary_key_column    TEXT         NOT NULL,
    property_mapping      JSONB        NOT NULL DEFAULT '[]',
    sync_mode             TEXT         NOT NULL CHECK (sync_mode IN ('snapshot', 'incremental', 'view')),
    default_marking       TEXT         NOT NULL DEFAULT 'public',
    preview_limit         INTEGER      NOT NULL DEFAULT 1000 CHECK (preview_limit BETWEEN 1 AND 100000),
    owner_id              UUID         NOT NULL,
    last_materialized_at  TIMESTAMPTZ,
    last_run_status       TEXT,
    last_run_summary      JSONB,
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_object_type_bindings_object_type
    ON object_type_bindings (object_type_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_object_type_bindings_dataset
    ON object_type_bindings (dataset_id);

-- Prevent duplicate bindings for the same (type, dataset, branch) tuple.
-- Branch may be NULL so we coalesce to the empty string for the unique key.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_object_type_bindings_triplet
    ON object_type_bindings (object_type_id, dataset_id, COALESCE(dataset_branch, ''));
