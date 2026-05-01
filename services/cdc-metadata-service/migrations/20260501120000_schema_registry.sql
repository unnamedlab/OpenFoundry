-- Schema Registry tables (Confluent-compatible naming).
--
-- `schema_subjects` is the addressable name (one per topic / serde
-- contract). `schema_versions` is the immutable history (1..N versions per
-- subject) holding the canonical schema text and a deterministic
-- fingerprint. `schema_references` lets a schema reference other
-- registered subjects (Avro IDL imports / Protobuf imports). The audit
-- table records every compatibility check so we can prove governance.

CREATE TABLE IF NOT EXISTS schema_subjects (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    -- Confluent levels: NONE, BACKWARD, BACKWARD_TRANSITIVE, FORWARD,
    -- FORWARD_TRANSITIVE, FULL, FULL_TRANSITIVE.
    compatibility_mode TEXT NOT NULL DEFAULT 'BACKWARD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS schema_versions (
    id UUID PRIMARY KEY,
    subject_id UUID NOT NULL REFERENCES schema_subjects(id) ON DELETE CASCADE,
    -- Monotonic per-subject version (1, 2, 3, ...).
    version INT NOT NULL,
    -- Restricted to the three serde families supported by the validators
    -- in `event-bus-control::schema_registry`.
    schema_type TEXT NOT NULL CHECK (schema_type IN ('avro', 'protobuf', 'json')),
    schema_text TEXT NOT NULL,
    -- SHA-256 of the canonicalised schema text. Used to short-circuit
    -- duplicate registrations (Confluent semantics: a duplicate POST
    -- returns the existing version instead of creating a new row).
    fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deprecated_at TIMESTAMPTZ NULL,
    UNIQUE (subject_id, version),
    UNIQUE (subject_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_schema_versions_subject
    ON schema_versions (subject_id, version DESC);

CREATE TABLE IF NOT EXISTS schema_references (
    version_id UUID NOT NULL REFERENCES schema_versions(id) ON DELETE CASCADE,
    -- Confluent reference: { name, subject, version }. We store the
    -- canonical (subject, version) pointer; the optional `name` (alias
    -- inside the schema text) is denormalised into the schema text itself
    -- so it does not need a separate column.
    ref_subject TEXT NOT NULL,
    ref_version INT NOT NULL,
    PRIMARY KEY (version_id, ref_subject, ref_version)
);

CREATE TABLE IF NOT EXISTS schema_compatibility_audit (
    id UUID PRIMARY KEY,
    subject_id UUID NULL REFERENCES schema_subjects(id) ON DELETE SET NULL,
    -- Snapshot of subject name at audit time so deletions do not lose
    -- the audit trail.
    subject_name TEXT NOT NULL,
    candidate_fingerprint TEXT NOT NULL,
    previous_version INT NULL,
    compatibility_mode TEXT NOT NULL,
    -- 'compatible', 'incompatible', 'error'
    outcome TEXT NOT NULL,
    detail TEXT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_schema_compat_audit_subject
    ON schema_compatibility_audit (subject_name, checked_at DESC);
