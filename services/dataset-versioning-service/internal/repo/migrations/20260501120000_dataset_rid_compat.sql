-- Compatibility for Go deployments that applied the pre-versioning
-- initial dataset schema before 20260501000001_versioning_init.sql.
-- Rust's versioning schema owns a public dataset RID; add/backfill it
-- before branch v2 migrations start depending on datasets.rid.

ALTER TABLE datasets
    ADD COLUMN IF NOT EXISTS rid TEXT;

UPDATE datasets
   SET rid = 'ri.foundry.main.dataset.' || id::text
 WHERE rid IS NULL OR rid = '';

ALTER TABLE datasets
    ALTER COLUMN rid SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS uq_datasets_rid
    ON datasets(rid);
