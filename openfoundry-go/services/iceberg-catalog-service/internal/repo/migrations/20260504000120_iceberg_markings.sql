-- D1.1.8 P3 — Iceberg markings.
--
-- Markings on Iceberg resources mirror the dataset model (D1.1.4 P4):
--
--   * `iceberg_namespace_markings` — explicit markings applied to a
--     namespace. The set is the source for inheritance into newly
--     created tables.
--   * `iceberg_table_markings` — per-table markings. We keep TWO
--     classes:
--       - `inherited_from_namespace` (snapshotted at table creation,
--         survives later namespace mutations per Foundry doc snapshot
--         semantics).
--       - `explicit` (operator-managed, applied in addition to the
--         inherited set).
--     Cedar's `IcebergTable.markings` reads the union — the catalog
--     never evaluates an "inherited at request time" view.
--
-- The original `iceberg_tables.markings` text array (P1) is kept for
-- backward compatibility but is now a *cached* projection of the
-- effective set; the trigger below keeps it in sync so existing
-- handlers that read it directly still see the right value.

CREATE TABLE IF NOT EXISTS iceberg_namespace_markings (
    namespace_id  UUID NOT NULL REFERENCES iceberg_namespaces(id) ON DELETE CASCADE,
    marking_id    UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by    UUID NOT NULL,
    PRIMARY KEY (namespace_id, marking_id)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_namespace_markings_namespace
    ON iceberg_namespace_markings(namespace_id);

CREATE TABLE IF NOT EXISTS iceberg_table_markings (
    table_id            UUID NOT NULL REFERENCES iceberg_tables(id) ON DELETE CASCADE,
    marking_id          UUID NOT NULL,
    -- Marking class:
    --   'inherited' — copied from the namespace at table-creation time.
    --   'explicit'  — set / removed by `manage_markings`.
    -- A single marking_id may only appear ONCE per (table_id, source).
    source              TEXT NOT NULL CHECK (source IN ('inherited', 'explicit')),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by          UUID NOT NULL,
    PRIMARY KEY (table_id, marking_id, source)
);

CREATE INDEX IF NOT EXISTS idx_iceberg_table_markings_table
    ON iceberg_table_markings(table_id);
CREATE INDEX IF NOT EXISTS idx_iceberg_table_markings_marking
    ON iceberg_table_markings(marking_id);

-- ─── Marking name catalog ─────────────────────────────────────────────
-- The catalog needs to project marking_id → human name when serialising
-- responses. We keep it local rather than calling into
-- security-governance-service so the GET .../markings endpoint stays a
-- single SQL hop. The names are seeded by an out-of-band sync job (left
-- to D1.1.8 P5); the table is intentionally permissive here so the
-- service still boots without security-governance-service reachable.
CREATE TABLE IF NOT EXISTS iceberg_marking_names (
    marking_id  UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default ladder so dev clusters work without an external sync.
INSERT INTO iceberg_marking_names (marking_id, name, description)
VALUES
    ('00000000-0000-0000-0000-00000000000a', 'public',       'Public'),
    ('00000000-0000-0000-0000-00000000000b', 'confidential', 'Confidential'),
    ('00000000-0000-0000-0000-00000000000c', 'pii',          'Personally Identifiable Information'),
    ('00000000-0000-0000-0000-00000000000d', 'restricted',   'Restricted'),
    ('00000000-0000-0000-0000-00000000000e', 'secret',       'Secret')
ON CONFLICT (marking_id) DO NOTHING;
