-- SG.37 — Retention execution history and mark/sweep recovery windows.

CREATE TABLE IF NOT EXISTS retention_execution_runs (
    id UUID PRIMARY KEY,
    org_id UUID NULL,
    dataset_rid TEXT NOT NULL,
    status TEXT NOT NULL,
    dry_run BOOLEAN NOT NULL DEFAULT FALSE,
    marked_transaction_count INTEGER NOT NULL DEFAULT 0,
    swept_transaction_count INTEGER NOT NULL DEFAULT 0,
    delete_transaction_count INTEGER NOT NULL DEFAULT 0,
    recovery_window_days INTEGER NOT NULL DEFAULT 7,
    remediation_deadline TIMESTAMPTZ NULL,
    irreversible_after TIMESTAMPTZ NULL,
    warnings JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS idx_retention_execution_runs_dataset_created
    ON retention_execution_runs(dataset_rid, created_at DESC);

CREATE TABLE IF NOT EXISTS retention_execution_items (
    id UUID PRIMARY KEY,
    run_id UUID NOT NULL REFERENCES retention_execution_runs(id) ON DELETE CASCADE,
    policy_id UUID NULL,
    transaction_id UUID NOT NULL,
    action TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    marked_at TIMESTAMPTZ NULL,
    recoverable_until TIMESTAMPTZ NULL,
    swept_at TIMESTAMPTZ NULL,
    requires_delete_transaction BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_retention_execution_items_run
    ON retention_execution_items(run_id);
CREATE INDEX IF NOT EXISTS idx_retention_execution_items_transaction
    ON retention_execution_items(transaction_id);
