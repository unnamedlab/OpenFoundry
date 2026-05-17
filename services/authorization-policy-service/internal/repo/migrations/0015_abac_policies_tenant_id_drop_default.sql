-- Drop the temporary DEFAULT introduced by 0014. The zero-UUID default
-- existed only to satisfy NOT NULL for pre-existing rows during the
-- migration; new inserts must pass tenant_id explicitly so a bug at
-- the application layer can't silently fall back to the placeholder.
--
-- Idempotent: DROP DEFAULT is a no-op when no default is set.

ALTER TABLE abac_policies ALTER COLUMN tenant_id DROP DEFAULT;
