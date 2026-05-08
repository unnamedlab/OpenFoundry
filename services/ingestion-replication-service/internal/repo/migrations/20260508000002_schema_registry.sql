-- Confluent-compatible schema registry subset used by cdc_metadata routes.

CREATE TABLE IF NOT EXISTS schema_subjects (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    compatibility_mode TEXT NOT NULL DEFAULT 'BACKWARD',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS schema_versions (
    id UUID PRIMARY KEY,
    subject_id UUID NOT NULL REFERENCES schema_subjects(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    schema_type TEXT NOT NULL DEFAULT 'AVRO',
    schema_text TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deprecated_at TIMESTAMPTZ NULL,
    UNIQUE (subject_id, version),
    UNIQUE (subject_id, fingerprint)
);

CREATE INDEX IF NOT EXISTS idx_schema_versions_subject
    ON schema_versions(subject_id, version DESC);

CREATE TABLE IF NOT EXISTS schema_references (
    version_id UUID NOT NULL REFERENCES schema_versions(id) ON DELETE CASCADE,
    ref_name TEXT NOT NULL DEFAULT '',
    ref_subject TEXT NOT NULL,
    ref_version INTEGER NOT NULL,
    PRIMARY KEY (version_id, ref_name, ref_subject, ref_version)
);

CREATE TABLE IF NOT EXISTS schema_compatibility_audit (
    id UUID PRIMARY KEY,
    subject_id UUID REFERENCES schema_subjects(id) ON DELETE SET NULL,
    subject_name TEXT NOT NULL,
    candidate_fingerprint TEXT NOT NULL,
    previous_version INTEGER NULL,
    compatibility_mode TEXT NOT NULL,
    outcome TEXT NOT NULL,
    detail TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
