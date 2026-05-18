-- SG.34: network egress policy persistence.
--
-- One row per egress policy. The rich nested payload (approval tasks,
-- audit events, workload usages, sharing grants, decorations) is stored
-- in `policy JSONB` so the existing handler state machine remains the
-- single source of truth for mutations: read row → mutate in Go → write
-- row, under a SELECT ... FOR UPDATE lock.
--
-- The plain columns mirror the JSONB so the common filter paths
-- (state, kind, created_by, ordering) can use B-tree indexes.

CREATE TABLE IF NOT EXISTS network_egress_policies (
    id            UUID PRIMARY KEY,
    name          TEXT NOT NULL,
    kind          TEXT NOT NULL,
    state         TEXT NOT NULL,
    created_by    UUID NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version       BIGINT NOT NULL DEFAULT 1,
    policy        JSONB NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_network_egress_policies_state
    ON network_egress_policies (state, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_network_egress_policies_kind
    ON network_egress_policies (kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_network_egress_policies_created_by
    ON network_egress_policies (created_by, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_network_egress_policies_approval_tasks
    ON network_egress_policies USING GIN ((policy -> 'approval_tasks'));
