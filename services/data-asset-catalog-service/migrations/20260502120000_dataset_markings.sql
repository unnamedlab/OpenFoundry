-- T3.1 — dataset_markings
--
-- Records every marking that applies to a dataset, with the *reason* it
-- applies (`source`) and, for inherited markings, the immediate
-- upstream that propagated it (`inherited_from`).
--
-- Why this lives in `data-asset-catalog-service`:
--
--   The catalog is the single read-model that already serves
--   `marking_from_tags` to the rest of the platform via
--   `handlers::internal::get_dataset_metadata`. Co-locating
--   `dataset_markings` lets us atomically transition from
--   tag-derived markings (legacy) to first-class rows without an
--   extra cross-service join.
--
-- Inheritance rules (enforced in code, not SQL):
--
--   * `source = 'direct'`  ⇒ `inherited_from IS NULL`.
--   * `source = 'inherited_from_upstream'` ⇒ `inherited_from = '<upstream_rid>'`.
--   * The same `(dataset_rid, marking_id)` pair may appear multiple
--     times — once direct, plus once per upstream that contributes it.
--     The PK below uses `(dataset_rid, marking_id, COALESCE(inherited_from, ''))`
--     so the rows are unique without forcing the application to dedupe
--     across heterogeneous sources.

CREATE TABLE IF NOT EXISTS dataset_markings (
    dataset_rid     TEXT        NOT NULL,
    marking_id      UUID        NOT NULL,
    source          TEXT        NOT NULL,
    inherited_from  TEXT        NULL,
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_by      UUID        NULL,
    CONSTRAINT chk_dataset_markings_source
        CHECK (source IN ('direct', 'inherited_from_upstream')),
    CONSTRAINT chk_dataset_markings_source_inheritance
        CHECK (
            (source = 'direct' AND inherited_from IS NULL)
            OR (source = 'inherited_from_upstream' AND inherited_from IS NOT NULL)
        )
);

-- Unique row per (dataset, marking, contributing-upstream). For direct
-- markings `inherited_from` is NULL, which `COALESCE` collapses to ''.
CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_markings_dataset_marking_source
    ON dataset_markings (dataset_rid, marking_id, COALESCE(inherited_from, ''));

CREATE INDEX IF NOT EXISTS idx_dataset_markings_dataset
    ON dataset_markings (dataset_rid);

CREATE INDEX IF NOT EXISTS idx_dataset_markings_inherited_from
    ON dataset_markings (inherited_from)
    WHERE inherited_from IS NOT NULL;
