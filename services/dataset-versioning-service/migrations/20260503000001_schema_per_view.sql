-- T6.x — schema-per-view (Foundry parity).
--
-- Foundry stores the schema as metadata on a *dataset view*, not on the
-- dataset itself. That makes schemas version with every commit: a
-- transaction that adds a column produces a new view that has its own
-- schema row. Today we kept the type-level info on `datasets.format`,
-- which can't represent schema evolution. This migration introduces
-- `dataset_view_schemas`, soft-deprecates `datasets.format` to a
-- "default format for new views", and wires a trigger that promotes a
-- transaction-level `metadata->'schema'` payload into a view-level row
-- on commit.
--
-- See `docs_original_palantir_foundry/foundry-docs/.../Datasets.md`
-- § "Schemas" and § "File formats".

CREATE TABLE IF NOT EXISTS dataset_view_schemas (
    -- 1:1 with dataset_views: each materialised view carries at most
    -- one schema row at a time. Updates overwrite (idempotent by
    -- content_hash at the API layer).
    view_id         UUID PRIMARY KEY
                         REFERENCES dataset_views(id) ON DELETE CASCADE,
    -- Full Foundry schema serialised as JSON. The shape mirrors the
    -- Rust `DatasetSchema` struct in
    -- `services/dataset-versioning-service/src/models/schema.rs`.
    schema_json     JSONB NOT NULL,
    -- Storage format ("PARQUET" | "AVRO" | "TEXT"). Drives interpretation
    -- of `custom_metadata` (only TEXT consumes CSV options).
    file_format     TEXT  NOT NULL DEFAULT 'PARQUET'
                         CHECK (file_format IN ('PARQUET','AVRO','TEXT')),
    -- Optional per-format options (CSV parsing for TEXT, etc.).
    custom_metadata JSONB,
    -- Stable hash of the canonical schema_json so the API can decide
    -- between 200 (no-op) and 201 (created/updated) without re-parsing.
    content_hash    TEXT  NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dataset_view_schemas_hash
    ON dataset_view_schemas(content_hash);

-- Backfill: every existing view gets an empty-fields row whose
-- `file_format` is inherited from the parent dataset's `format`
-- (uppercased to match the new constraint set). Rows where the
-- catalog format isn't one of {parquet,avro,text} fall back to
-- PARQUET — that matches how preview/runtime treat unknown formats.
INSERT INTO dataset_view_schemas (view_id, schema_json, file_format, custom_metadata, content_hash, created_at)
SELECT
    v.id AS view_id,
    '{"fields": []}'::jsonb AS schema_json,
    CASE upper(d.format)
        WHEN 'PARQUET' THEN 'PARQUET'
        WHEN 'AVRO'    THEN 'AVRO'
        WHEN 'TEXT'    THEN 'TEXT'
        WHEN 'CSV'     THEN 'TEXT'
        WHEN 'TSV'     THEN 'TEXT'
        WHEN 'JSON'    THEN 'TEXT'
        ELSE 'PARQUET'
    END AS file_format,
    NULL AS custom_metadata,
    md5('{"fields": []}') AS content_hash,
    NOW() AS created_at
  FROM dataset_views v
  JOIN datasets d ON d.id = v.dataset_id
 WHERE NOT EXISTS (
     SELECT 1 FROM dataset_view_schemas s WHERE s.view_id = v.id
 );

-- Trigger: when a transaction transitions OPEN→COMMITTED and its
-- metadata carries a `schema` payload, materialise (or upsert) a
-- `dataset_views` row for the committed transaction and persist its
-- schema into `dataset_view_schemas`.
--
-- The view materialisation here is a *placeholder*: it satisfies the
-- FK and surfaces a view_id immediately. The full file-list cache is
-- still populated lazily by `compute_view_at` the first time a reader
-- asks for the view; that call updates `computed_at`/`file_count`
-- without touching the schema row.
CREATE OR REPLACE FUNCTION fn_dataset_view_schemas_from_txn() RETURNS trigger AS $$
DECLARE
    v_view_id UUID;
    v_schema  JSONB;
    v_format  TEXT;
    v_meta    JSONB;
    v_hash    TEXT;
BEGIN
    -- Only on OPEN → COMMITTED transitions. ABORTED never carries a
    -- schema we want to honour.
    IF NEW.status <> 'COMMITTED' OR OLD.status = 'COMMITTED' THEN
        RETURN NEW;
    END IF;

    v_schema := NEW.metadata -> 'schema';
    IF v_schema IS NULL OR jsonb_typeof(v_schema) <> 'object' THEN
        RETURN NEW;
    END IF;

    -- Normalise file_format. Accept lower-case aliases coming from API
    -- callers and map CSV/TSV/JSON onto TEXT.
    v_format := upper(COALESCE(v_schema ->> 'file_format', 'PARQUET'));
    IF v_format IN ('CSV','TSV','JSON') THEN
        v_format := 'TEXT';
    END IF;
    IF v_format NOT IN ('PARQUET','AVRO','TEXT') THEN
        v_format := 'PARQUET';
    END IF;

    v_meta := v_schema -> 'custom_metadata';
    v_hash := md5(v_schema::text);

    -- Materialise a placeholder view row keyed by the committed txn so
    -- the schema FK has a target. ON CONFLICT keeps lazy materialisation
    -- (compute_view_at) idempotent.
    INSERT INTO dataset_views
            (id, dataset_id, branch_id, head_transaction_id,
             computed_at, file_count, size_bytes)
    VALUES  (gen_random_uuid(), NEW.dataset_id, NEW.branch_id, NEW.id,
             NOW(), 0, 0)
    ON CONFLICT (dataset_id, branch_id, head_transaction_id) DO UPDATE
        SET computed_at = NOW()
    RETURNING id INTO v_view_id;

    INSERT INTO dataset_view_schemas
            (view_id, schema_json, file_format, custom_metadata, content_hash, created_at)
    VALUES  (v_view_id, v_schema, v_format, v_meta, v_hash, NOW())
    ON CONFLICT (view_id) DO UPDATE
        SET schema_json     = EXCLUDED.schema_json,
            file_format     = EXCLUDED.file_format,
            custom_metadata = EXCLUDED.custom_metadata,
            content_hash    = EXCLUDED.content_hash;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_dataset_view_schemas_from_txn
    ON dataset_transactions;

CREATE TRIGGER trg_dataset_view_schemas_from_txn
    AFTER UPDATE OF status ON dataset_transactions
    FOR EACH ROW
    WHEN (NEW.status = 'COMMITTED' AND OLD.status IS DISTINCT FROM 'COMMITTED')
    EXECUTE FUNCTION fn_dataset_view_schemas_from_txn();

COMMENT ON COLUMN datasets.format IS
    'Soft-deprecated. Default file format inherited by new dataset views; '
    'effective per-view schema lives in dataset_view_schemas.file_format.';
