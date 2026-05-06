-- P3 — Foundry "Backing filesystem" mapping.
--
-- Per `docs_original_palantir_foundry/foundry-docs/.../Datasets.md`
-- § "Backing filesystem":
--
--   > a mapping is maintained between a file's logical path in Foundry
--   > and its physical path in a backing file system.
--
-- This migration introduces `dataset_files` as the public mapping and
-- backfills it from the existing per-transaction file rows
-- (`dataset_transaction_files`). Going forward `dataset_transaction_files`
-- stays as the staging table inside an OPEN transaction; the canonical
-- post-commit projection lives in `dataset_files`.
--
-- The `physical_uri` column matches the format produced by
-- `storage_abstraction::backing_fs::PhysicalLocation::uri()`:
--   * local      → local:///{base_dir}/{relative_path}
--   * s3         → s3://{bucket}/{base_dir}/{relative_path}
--   * hdfs       → hdfs://{namenode}/{base_dir}/{relative_path}

CREATE TABLE IF NOT EXISTS dataset_files (
    id              UUID PRIMARY KEY,
    dataset_id      UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    transaction_id  UUID NOT NULL REFERENCES dataset_transactions(id) ON DELETE CASCADE,
    -- Logical, dataset-relative path visible to readers
    -- (e.g. `transactions/{txn}/file.parquet`).
    logical_path    TEXT NOT NULL,
    -- Canonical PhysicalLocation::uri() string. Persisted as-is so
    -- callers don't have to re-derive scheme/bucket/base.
    physical_uri    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    -- SHA-256 hex digest of the bytes, recorded at upload time.
    -- Optional because legacy rows may have been ingested without it.
    sha256          TEXT,
    -- Soft delete: aligns with the doc's "DELETE transactions remove
    -- the file *reference* from the view, not the underlying file"
    -- contract. Retention policies (P4) are responsible for removing
    -- the physical object once `deleted_at` is older than the policy.
    deleted_at      TIMESTAMPTZ NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (dataset_id, transaction_id, logical_path)
);

CREATE INDEX IF NOT EXISTS idx_dataset_files_dataset
    ON dataset_files(dataset_id);
CREATE INDEX IF NOT EXISTS idx_dataset_files_dataset_active
    ON dataset_files(dataset_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_dataset_files_transaction
    ON dataset_files(transaction_id);
CREATE INDEX IF NOT EXISTS idx_dataset_files_logical
    ON dataset_files(dataset_id, logical_path);

-- Backfill from `dataset_transaction_files`. Rows whose owning
-- transaction is COMMITTED land in `dataset_files`; ABORTED ones are
-- skipped. We synthesise a `local://` physical URI from the existing
-- `physical_path` column (which historically already stored a
-- backend-rooted key). When `physical_path` is empty we fall back to
-- the dataset's `storage_path` + logical_path.
INSERT INTO dataset_files (
        id, dataset_id, transaction_id, logical_path,
        physical_uri, size_bytes, sha256, deleted_at, created_at)
SELECT
    gen_random_uuid()                                  AS id,
    t.dataset_id                                       AS dataset_id,
    f.transaction_id                                   AS transaction_id,
    f.logical_path                                     AS logical_path,
    CASE
        WHEN COALESCE(f.physical_path, '') <> ''
            THEN 'local:///' || trim(both '/' from f.physical_path)
        ELSE 'local:///' || trim(both '/' from coalesce(d.storage_path, ''))
                                || '/' || trim(both '/' from f.logical_path)
    END                                                AS physical_uri,
    f.size_bytes                                       AS size_bytes,
    NULL                                               AS sha256,
    -- DELETE transactions that staged a REMOVE op: surface the
    -- deletion timestamp so the Files tab can render the soft-deleted
    -- badge. The other ops keep deleted_at = NULL.
    CASE
        WHEN f.op = 'REMOVE' THEN COALESCE(t.committed_at, t.aborted_at, NOW())
        ELSE NULL
    END                                                AS deleted_at,
    COALESCE(t.committed_at, t.started_at, NOW())      AS created_at
  FROM dataset_transaction_files f
  JOIN dataset_transactions t ON t.id = f.transaction_id
  JOIN datasets d              ON d.id = t.dataset_id
 WHERE t.status = 'COMMITTED'
   AND NOT EXISTS (
        SELECT 1 FROM dataset_files df
         WHERE df.dataset_id = t.dataset_id
           AND df.transaction_id = f.transaction_id
           AND df.logical_path = f.logical_path
   );

-- Trigger: keep `dataset_files` in sync with subsequent commits.
-- Whenever a transaction transitions OPEN → COMMITTED we copy its
-- staged files into `dataset_files` (REMOVE ops land as `deleted_at`
-- rows so the per-view algorithm can surface them).
CREATE OR REPLACE FUNCTION fn_dataset_files_from_committed_txn() RETURNS trigger AS $$
BEGIN
    IF NEW.status <> 'COMMITTED' OR OLD.status = 'COMMITTED' THEN
        RETURN NEW;
    END IF;

    INSERT INTO dataset_files (
            id, dataset_id, transaction_id, logical_path,
            physical_uri, size_bytes, sha256, deleted_at, created_at)
    SELECT
        gen_random_uuid(),
        NEW.dataset_id,
        NEW.id,
        f.logical_path,
        CASE
            WHEN COALESCE(f.physical_path, '') <> ''
                THEN 'local:///' || trim(both '/' from f.physical_path)
            ELSE 'local:///' || NEW.id::text || '/' || trim(both '/' from f.logical_path)
        END,
        f.size_bytes,
        NULL,
        CASE WHEN f.op = 'REMOVE' THEN NOW() ELSE NULL END,
        NOW()
      FROM dataset_transaction_files f
     WHERE f.transaction_id = NEW.id
    ON CONFLICT (dataset_id, transaction_id, logical_path) DO NOTHING;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_dataset_files_from_committed_txn
    ON dataset_transactions;

CREATE TRIGGER trg_dataset_files_from_committed_txn
    AFTER UPDATE OF status ON dataset_transactions
    FOR EACH ROW
    WHEN (NEW.status = 'COMMITTED' AND OLD.status IS DISTINCT FROM 'COMMITTED')
    EXECUTE FUNCTION fn_dataset_files_from_committed_txn();
