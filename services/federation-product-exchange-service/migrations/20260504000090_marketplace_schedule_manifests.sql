-- P3 — Marketplace product schedules (Foundry doc § "Add schedule
-- to a Marketplace product"). Each manifest is a JSONB-shaped
-- ScheduleManifest persisted against a specific product version, so
-- newer versions can ship updated manifests without touching old ones.

CREATE TABLE IF NOT EXISTS marketplace_schedule_manifests (
    id                  UUID PRIMARY KEY,
    product_version_id  UUID NOT NULL REFERENCES marketplace_package_versions(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    manifest_json       JSONB NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (product_version_id, name)
);

CREATE INDEX IF NOT EXISTS marketplace_schedule_manifests_version_idx
    ON marketplace_schedule_manifests(product_version_id);
