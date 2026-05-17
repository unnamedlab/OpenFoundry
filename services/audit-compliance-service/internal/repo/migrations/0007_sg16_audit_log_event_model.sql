-- 0007: SG.16 - Audit.3-style event model.
--
-- The original audit_events table remains append-only and hash-chained.
-- These columns add the normalized security-investigation fields that are
-- promoted in Palantir's audit.3 schema: event/log-entry identifiers,
-- actor/session/service-account context, categories, entities, origins,
-- trace correlation, outcomes, and error/request/result metadata.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS event_id UUID,
    ADD COLUMN IF NOT EXISTS log_entry_id UUID,
    ADD COLUMN IF NOT EXISTS sequence_id UUID NULL,
    ADD COLUMN IF NOT EXISTS product TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS product_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS producer_type TEXT NOT NULL DEFAULT 'SERVER',
    ADD COLUMN IF NOT EXISTS actor_id TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS actor_type TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS session_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS service_account_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS token_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS categories TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS entities JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS origins TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS origin TEXT NULL,
    ADD COLUMN IF NOT EXISTS source_origin TEXT NULL,
    ADD COLUMN IF NOT EXISTS trace_id TEXT NULL,
    ADD COLUMN IF NOT EXISTS outcome TEXT NOT NULL DEFAULT 'success',
    ADD COLUMN IF NOT EXISTS error_metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS request_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS result_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS parent_event_id UUID NULL,
    ADD COLUMN IF NOT EXISTS initiator_type TEXT NOT NULL DEFAULT 'user',
    ADD COLUMN IF NOT EXISTS audit_access_tier TEXT NOT NULL DEFAULT 'security_sensitive';

UPDATE audit_events
   SET event_id = COALESCE(event_id, id),
       log_entry_id = COALESCE(log_entry_id, id),
       product = COALESCE(NULLIF(product, ''), source_service),
       actor_id = COALESCE(NULLIF(actor_id, ''), actor),
       actor_type = CASE
           WHEN actor_type <> '' THEN actor_type
           WHEN actor LIKE 'service:%' OR actor LIKE 'system:%' THEN 'service'
           ELSE 'user'
       END,
       categories = CASE
           WHEN categories <> '{}'::text[] THEN categories
           WHEN labels ? 'export' THEN ARRAY['dataExport']
           WHEN labels ? 'contains-sensitive-data' THEN ARRAY['dataLoad']
           ELSE ARRAY[]::text[]
       END,
       entities = CASE
           WHEN jsonb_typeof(entities) = 'array' AND entities <> '[]'::jsonb THEN entities
           ELSE jsonb_build_array(jsonb_build_object(
               'kind', resource_type,
               'id', resource_id,
               'rid', resource_id
           ))
       END,
       origins = CASE
           WHEN origins <> '{}'::text[] THEN origins
           WHEN ip_address IS NOT NULL AND ip_address <> '' THEN ARRAY[ip_address]
           ELSE ARRAY[]::text[]
       END,
       origin = COALESCE(origin, ip_address),
       source_origin = COALESCE(source_origin, ip_address),
       outcome = CASE status
           WHEN 'success' THEN 'success'
           WHEN 'denied' THEN 'unauthorized'
           ELSE 'error'
       END,
       request_fields = CASE
           WHEN request_fields <> '{}'::jsonb THEN request_fields
           ELSE metadata
       END,
       initiator_type = CASE
           WHEN origins <> '{}'::text[] OR ip_address IS NOT NULL THEN 'user'
           WHEN actor LIKE 'service:%' OR actor LIKE 'system:%' THEN 'service'
           ELSE initiator_type
       END
 WHERE event_id IS NULL
    OR log_entry_id IS NULL
    OR product = ''
    OR actor_id = ''
    OR categories = '{}'::text[]
    OR entities = '[]'::jsonb
    OR outcome = 'success';

ALTER TABLE audit_events
    ALTER COLUMN event_id SET NOT NULL,
    ALTER COLUMN event_id SET DEFAULT gen_random_uuid(),
    ALTER COLUMN log_entry_id SET NOT NULL,
    ALTER COLUMN log_entry_id SET DEFAULT gen_random_uuid();

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'audit_events_entities_array_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_entities_array_check
            CHECK (jsonb_typeof(entities) = 'array');
    END IF;
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'audit_events_error_metadata_object_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_error_metadata_object_check
            CHECK (jsonb_typeof(error_metadata) = 'object');
    END IF;
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'audit_events_request_fields_object_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_request_fields_object_check
            CHECK (jsonb_typeof(request_fields) = 'object');
    END IF;
    IF NOT EXISTS (
        SELECT 1
          FROM pg_constraint
         WHERE conname = 'audit_events_result_fields_object_check'
    ) THEN
        ALTER TABLE audit_events
            ADD CONSTRAINT audit_events_result_fields_object_check
            CHECK (jsonb_typeof(result_fields) = 'object');
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS idx_audit_events_log_entry_id
    ON audit_events (log_entry_id);

CREATE INDEX IF NOT EXISTS idx_audit_events_event_id_sequence
    ON audit_events (event_id, sequence);

CREATE INDEX IF NOT EXISTS idx_audit_events_trace_id
    ON audit_events (trace_id)
    WHERE trace_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_audit_events_categories_gin
    ON audit_events USING GIN (categories);

CREATE INDEX IF NOT EXISTS idx_audit_events_entities_gin
    ON audit_events USING GIN (entities);

CREATE INDEX IF NOT EXISTS idx_audit_events_retention_until
    ON audit_events (retention_until);

INSERT INTO retention_policies (
    id, name, scope, target_kind, retention_days, legal_hold, purge_mode,
    rules, updated_by, active, is_system, selector, criteria,
    grace_period_minutes
)
VALUES (
    '01968522-2f75-7f3a-bb5d-000000000616',
    'AUDIT_LOG_SECURITY_RETENTION',
    'security.audit',
    'audit_log',
    730,
    TRUE,
    'redact-then-retain-hash',
    '["audit_logs_are_sensitive","retain_event_ids_and_hash_chain","restrict_to_audit_viewers"]'::jsonb,
    'security-governance',
    TRUE,
    TRUE,
    '{"target_kind": "audit_log"}'::jsonb,
    '{"classification": ["confidential", "pii"], "legal_hold_supported": true}'::jsonb,
    60
)
ON CONFLICT (id) DO UPDATE SET
    retention_days = EXCLUDED.retention_days,
    legal_hold = EXCLUDED.legal_hold,
    purge_mode = EXCLUDED.purge_mode,
    rules = EXCLUDED.rules,
    active = EXCLUDED.active,
    is_system = EXCLUDED.is_system,
    selector = EXCLUDED.selector,
    criteria = EXCLUDED.criteria,
    updated_at = NOW();
