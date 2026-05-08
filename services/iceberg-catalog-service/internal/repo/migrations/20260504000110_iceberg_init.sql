-- iceberg-catalog-service: Foundry Iceberg REST Catalog [Beta] schema.
--
-- The catalog persists Iceberg metadata in Postgres while the backing data
-- (Parquet, manifests, version-hint files) live in object storage via
-- libs/storage-abstraction. The schema mirrors the surface that the Apache
-- Iceberg REST Catalog OpenAPI spec exposes (see open-api/rest-catalog-open-api.yaml
-- on the Apache Iceberg repo): namespaces, tables, snapshots, branches and
-- metadata files (`v{N}.metadata.json`).

-- ─── Namespaces ────────────────────────────────────────────────────────────
-- A namespace is a hierarchical container for tables (multi-level via
-- parent_namespace_id). The Iceberg REST spec uses dot-separated path
-- components — Foundry maps each segment to a row.
CREATE TABLE IF NOT EXISTS iceberg_namespaces (
    id                    UUID PRIMARY KEY,
    project_rid           TEXT NOT NULL,
    name                  TEXT NOT NULL,
    parent_namespace_id   UUID NULL REFERENCES iceberg_namespaces(id) ON DELETE CASCADE,
    properties            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by            UUID NOT NULL,
    UNIQUE(project_rid, name)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_namespaces_project
    ON iceberg_namespaces(project_rid);
CREATE INDEX IF NOT EXISTS idx_iceberg_namespaces_parent
    ON iceberg_namespaces(parent_namespace_id);

-- ─── Tables ────────────────────────────────────────────────────────────────
-- Each Iceberg table is uniquely identified by its `table_uuid` (the
-- random UUID Iceberg writes inside `metadata.json`) and exposed publicly
-- via a Foundry-style RID generated from the row's primary key. Format
-- versions 1, 2 and 3 are accepted; v2 is the catalog default per the
-- spec (https://iceberg.apache.org/spec/#format-versioning).
CREATE TABLE IF NOT EXISTS iceberg_tables (
    id                          UUID PRIMARY KEY,
    rid                         TEXT UNIQUE GENERATED ALWAYS AS
                                ('ri.foundry.main.iceberg-table.' || id::text) STORED,
    namespace_id                UUID NOT NULL REFERENCES iceberg_namespaces(id) ON DELETE CASCADE,
    name                        TEXT NOT NULL,
    table_uuid                  TEXT NOT NULL UNIQUE,
    format_version              INT NOT NULL DEFAULT 2 CHECK (format_version IN (1, 2, 3)),
    location                    TEXT NOT NULL,
    current_snapshot_id         BIGINT NULL,
    current_metadata_location   TEXT NULL,
    last_sequence_number        BIGINT NOT NULL DEFAULT 0,
    partition_spec              JSONB NOT NULL DEFAULT '{}'::jsonb,
    schema_json                 JSONB NOT NULL,
    sort_order                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    properties                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- D1.1.8 P3: marking enforcement reads this column when arbitrating
    -- access through the bearer / OAuth2 paths.
    markings                    TEXT[] NOT NULL DEFAULT '{}',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(namespace_id, name)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_tables_namespace
    ON iceberg_tables(namespace_id);
CREATE INDEX IF NOT EXISTS idx_iceberg_tables_format
    ON iceberg_tables(format_version);
CREATE INDEX IF NOT EXISTS idx_iceberg_tables_markings
    ON iceberg_tables USING GIN(markings);

-- ─── Snapshots ─────────────────────────────────────────────────────────────
-- One row per Iceberg snapshot per table. `summary` mirrors the per-spec
-- key/value map (added-data-files, deleted-data-files, added-records, …)
-- so the UI can render the history view with no further joins.
CREATE TABLE IF NOT EXISTS iceberg_snapshots (
    id                      BIGSERIAL PRIMARY KEY,
    table_id                UUID NOT NULL REFERENCES iceberg_tables(id) ON DELETE CASCADE,
    snapshot_id             BIGINT NOT NULL,
    parent_snapshot_id      BIGINT NULL,
    sequence_number         BIGINT NOT NULL,
    operation               TEXT NOT NULL CHECK (operation IN ('append','overwrite','delete','replace')),
    manifest_list_location  TEXT NOT NULL,
    summary                 JSONB NOT NULL,
    schema_id               INT NOT NULL,
    timestamp_ms            BIGINT NOT NULL,
    UNIQUE(table_id, snapshot_id)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_snapshots_table
    ON iceberg_snapshots(table_id);
CREATE INDEX IF NOT EXISTS idx_iceberg_snapshots_parent
    ON iceberg_snapshots(parent_snapshot_id);
CREATE INDEX IF NOT EXISTS idx_iceberg_snapshots_timestamp
    ON iceberg_snapshots(timestamp_ms DESC);

-- ─── Branches (refs) ───────────────────────────────────────────────────────
-- Iceberg branches and tags both live in `iceberg_table_branches` with the
-- `kind` discriminator. Foundry treats `master` and `main` as equivalent
-- per docs/data-integration/iceberg-tables, so `name` accepts both.
CREATE TABLE IF NOT EXISTS iceberg_table_branches (
    id                      UUID PRIMARY KEY,
    table_id                UUID NOT NULL REFERENCES iceberg_tables(id) ON DELETE CASCADE,
    name                    TEXT NOT NULL,
    kind                    TEXT NOT NULL DEFAULT 'branch'
                            CHECK (kind IN ('branch', 'tag')),
    snapshot_id             BIGINT NOT NULL,
    max_ref_age_ms          BIGINT NULL,
    max_snapshot_age_ms     BIGINT NULL,
    min_snapshots_to_keep   INT NULL,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(table_id, name)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_branches_table
    ON iceberg_table_branches(table_id);

-- ─── Metadata files (history) ─────────────────────────────────────────────
-- Every CommitTable / CreateTable produces a new `v{N}.metadata.json` in
-- object storage. The catalog records the path so consumers can replay
-- history (and so the UI's "Metadata" tab can list past versions).
CREATE TABLE IF NOT EXISTS iceberg_table_metadata_files (
    id              UUID PRIMARY KEY,
    table_id        UUID NOT NULL REFERENCES iceberg_tables(id) ON DELETE CASCADE,
    version         INT NOT NULL,
    path            TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(table_id, version)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_metadata_files_table
    ON iceberg_table_metadata_files(table_id);

-- ─── API tokens ────────────────────────────────────────────────────────────
-- Long-lived bearer tokens issued via POST /v1/iceberg-clients/api-tokens.
-- We store the SHA-256 of the token (so we can validate the bearer header
-- without keeping the secret) plus a 4-char `token_hint` for the UI.
CREATE TABLE IF NOT EXISTS iceberg_api_tokens (
    id              UUID PRIMARY KEY,
    user_id         UUID NOT NULL,
    name            TEXT NOT NULL,
    token_hash      TEXT NOT NULL UNIQUE,
    token_hint      TEXT NOT NULL,
    scopes          TEXT[] NOT NULL DEFAULT '{api:iceberg-read,api:iceberg-write}'::TEXT[],
    expires_at      TIMESTAMPTZ NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at    TIMESTAMPTZ NULL,
    revoked_at      TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_iceberg_api_tokens_user
    ON iceberg_api_tokens(user_id);
