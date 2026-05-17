-- 0008: SG.17 - Audit delivery destinations and export files.
--
-- This is the local delivery registry for SIEM polling and governed
-- in-platform datasets. Audit events remain append-only in audit_events;
-- delivery files are immutable NDJSON snapshots over a date range and
-- schema version.

CREATE TABLE IF NOT EXISTS audit_delivery_destinations (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id          UUID NULL,
    name                     TEXT NOT NULL,
    destination_type         TEXT NOT NULL,
    schema_version           TEXT NOT NULL DEFAULT 'audit.3',
    endpoint_url             TEXT NULL,
    dataset_rid              TEXT NULL,
    enabled                  BOOLEAN NOT NULL DEFAULT TRUE,
    validation_status        TEXT NOT NULL DEFAULT 'pending',
    validation_message       TEXT NOT NULL DEFAULT '',
    last_validated_at        TIMESTAMPTZ NULL,
    last_backfill_status     TEXT NOT NULL DEFAULT 'idle',
    last_backfill_started_at TIMESTAMPTZ NULL,
    last_backfill_completed_at TIMESTAMPTZ NULL,
    metadata                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by               TEXT NOT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT audit_delivery_destination_type_check
        CHECK (destination_type IN ('siem_api', 'openfoundry_dataset')),
    CONSTRAINT audit_delivery_schema_version_check
        CHECK (schema_version IN ('audit.3')),
    CONSTRAINT audit_delivery_validation_status_check
        CHECK (validation_status IN ('pending', 'valid', 'invalid')),
    CONSTRAINT audit_delivery_backfill_status_check
        CHECK (last_backfill_status IN ('idle', 'running', 'completed', 'failed')),
    CONSTRAINT audit_delivery_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object')
);

CREATE INDEX IF NOT EXISTS audit_delivery_destinations_org_idx
    ON audit_delivery_destinations (organization_id, destination_type);

CREATE INDEX IF NOT EXISTS audit_delivery_destinations_enabled_idx
    ON audit_delivery_destinations (enabled, updated_at DESC);

CREATE TABLE IF NOT EXISTS audit_delivery_files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    destination_id  UUID NOT NULL REFERENCES audit_delivery_destinations(id) ON DELETE CASCADE,
    organization_id UUID NULL,
    schema_version  TEXT NOT NULL DEFAULT 'audit.3',
    content_format  TEXT NOT NULL DEFAULT 'application/x-ndjson',
    start_time      TIMESTAMPTZ NOT NULL,
    end_time        TIMESTAMPTZ NOT NULL,
    event_count     BIGINT NOT NULL DEFAULT 0,
    duplicate_count BIGINT NOT NULL DEFAULT 0,
    content_sha256  TEXT NOT NULL DEFAULT '',
    content_bytes   BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'available',
    error_message   TEXT NOT NULL DEFAULT '',
    content         TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT audit_delivery_files_time_check
        CHECK (end_time > start_time),
    CONSTRAINT audit_delivery_files_format_check
        CHECK (content_format IN ('application/x-ndjson', 'application/json')),
    CONSTRAINT audit_delivery_files_status_check
        CHECK (status IN ('available', 'failed'))
);

CREATE INDEX IF NOT EXISTS audit_delivery_files_destination_time_idx
    ON audit_delivery_files (destination_id, start_time DESC, end_time DESC);

CREATE INDEX IF NOT EXISTS audit_delivery_files_org_time_idx
    ON audit_delivery_files (organization_id, start_time DESC)
    WHERE organization_id IS NOT NULL;
