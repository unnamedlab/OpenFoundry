-- entity-resolution-service / fusion_base — match rules + merge strategies foundation.
--
-- The Rust binary is `fn main(){}` and ships no migrations directory;
-- the Go port is canonical and owns this schema.

CREATE TABLE IF NOT EXISTS fusion_match_rules (
    id                    UUID PRIMARY KEY,
    name                  TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'active',
    entity_type           TEXT NOT NULL DEFAULT 'person',
    blocking_strategy     JSONB NOT NULL,
    conditions            JSONB NOT NULL,
    review_threshold      REAL NOT NULL DEFAULT 0.76,
    auto_merge_threshold  REAL NOT NULL DEFAULT 0.9,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fusion_match_rules_updated_at
  ON fusion_match_rules (updated_at DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS fusion_merge_strategies (
    id                UUID PRIMARY KEY,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'active',
    entity_type       TEXT NOT NULL DEFAULT 'person',
    default_strategy  TEXT NOT NULL DEFAULT 'longest_non_empty',
    rules             JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_fusion_merge_strategies_updated_at
  ON fusion_merge_strategies (updated_at DESC, created_at DESC);
