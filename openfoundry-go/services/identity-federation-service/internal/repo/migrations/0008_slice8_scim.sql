-- Slice 8 — SCIM 2.0 provisioning columns. Mirrors the Rust
-- src/handlers/scim.rs INSERT shapes for users + groups, which
-- assume a `scim_external_id` column on each side.
--
-- The Rust workspace materialised these in
-- libs/cassandra-kernel-internal-pg-schema, but the Go port keeps
-- the identity database schema co-located with the service module
-- so a fresh `Migrate(ctx, pool)` brings the entire SCIM surface up
-- without an external schema dependency.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS scim_external_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_scim_external_id
    ON users (scim_external_id)
    WHERE scim_external_id IS NOT NULL;

ALTER TABLE groups
    ADD COLUMN IF NOT EXISTS scim_external_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_groups_scim_external_id
    ON groups (scim_external_id)
    WHERE scim_external_id IS NOT NULL;
