-- DF.19 Iceberg metadata bridge.
-- Iceberg snapshots are table metadata snapshots and remain distinct from
-- Foundry-style dataset_transactions.tx_type = 'SNAPSHOT'.

CREATE TABLE IF NOT EXISTS dataset_iceberg_metadata (
  dataset_id UUID PRIMARY KEY REFERENCES datasets(id) ON DELETE CASCADE,
  table_rid TEXT NOT NULL DEFAULT '',
  namespace TEXT NOT NULL DEFAULT '',
  table_name TEXT NOT NULL DEFAULT '',
  table_uuid TEXT NOT NULL DEFAULT '',
  format_version INTEGER NOT NULL DEFAULT 2,
  current_iceberg_snapshot_id TEXT NOT NULL DEFAULT '',
  current_metadata_location TEXT NOT NULL DEFAULT '',
  previous_metadata_location TEXT NOT NULL DEFAULT '',
  schema_json JSONB NOT NULL DEFAULT '{}'::jsonb,
  branch_schema_behavior TEXT NOT NULL DEFAULT 'shared',
  last_sequence_number BIGINT NOT NULL DEFAULT 0,
  last_operation TEXT NOT NULL DEFAULT '',
  last_operation_at TIMESTAMPTZ,
  replace_snapshot_count INTEGER NOT NULL DEFAULT 0,
  compaction_count INTEGER NOT NULL DEFAULT 0,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  feature_gaps JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT dataset_iceberg_format_version_chk CHECK (format_version IN (1, 2)),
  CONSTRAINT dataset_iceberg_branch_schema_behavior_chk CHECK (branch_schema_behavior IN ('shared', 'per_branch', 'inherit_current'))
);

CREATE INDEX IF NOT EXISTS idx_dataset_iceberg_metadata_table_rid
  ON dataset_iceberg_metadata(table_rid)
  WHERE table_rid <> '';

CREATE INDEX IF NOT EXISTS idx_dataset_iceberg_metadata_snapshot
  ON dataset_iceberg_metadata(current_iceberg_snapshot_id)
  WHERE current_iceberg_snapshot_id <> '';
