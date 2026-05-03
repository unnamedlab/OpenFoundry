-- P5 — Foundry "Open in Marketplace" + dataset packaging.
--
-- Foundry's `Marketplace/` doc set lets users export a dataset (and
-- its surrounding metadata: schema, branches, retention, schedules)
-- as a versionable Product that can be re-installed in another
-- project / stack. The existing `marketplace_listings` flow targets
-- app templates / plug-ins; datasets get their own pair of tables so
-- the listing surface and the dataset-product surface evolve
-- independently.
--
-- `marketplace_dataset_products` stores the published manifest;
-- `marketplace_dataset_product_installs` records each install on a
-- target project so we can show a "currently installed in N projects"
-- counter on the Marketplace UI.

CREATE TABLE IF NOT EXISTS marketplace_dataset_products (
    id                      UUID PRIMARY KEY,
    name                    TEXT NOT NULL,
    -- Source dataset RID (`ri.foundry.main.dataset.<uuid>`). Kept as
    -- TEXT because the catalog tables live in another database in
    -- production deployments.
    source_dataset_rid      TEXT NOT NULL,
    -- Always 'dataset' for now; reserved column so future entity
    -- types (object set, ontology slice, …) reuse the same table.
    entity_type             TEXT NOT NULL DEFAULT 'dataset',
    version                 TEXT NOT NULL DEFAULT '1.0.0',
    project_id              UUID,
    published_by            UUID,

    -- Per-spec export toggles. Defaults match Foundry's
    -- "schema-only" preset: schema yes, raw bytes no.
    export_includes_data    BOOLEAN NOT NULL DEFAULT FALSE,
    include_schema          BOOLEAN NOT NULL DEFAULT TRUE,
    include_branches        BOOLEAN NOT NULL DEFAULT FALSE,
    include_retention       BOOLEAN NOT NULL DEFAULT FALSE,
    include_schedules       BOOLEAN NOT NULL DEFAULT FALSE,

    -- Frozen manifest applied at install time. Shape is documented in
    -- `services/marketplace-service/src/models/dataset_product.rs::DatasetProductManifest`.
    manifest                JSONB NOT NULL,
    -- 'schema-only' (default) recreates the dataset row + schema,
    -- 'with-snapshot' additionally copies the current view via the
    -- backing-fs API (P3).
    bootstrap_mode          TEXT NOT NULL DEFAULT 'schema-only'
                                CHECK (bootstrap_mode IN ('schema-only','with-snapshot')),

    published_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_dataset_rid, version)
);

CREATE INDEX IF NOT EXISTS idx_marketplace_dataset_products_rid
    ON marketplace_dataset_products(source_dataset_rid);
CREATE INDEX IF NOT EXISTS idx_marketplace_dataset_products_project
    ON marketplace_dataset_products(project_id);

CREATE TABLE IF NOT EXISTS marketplace_dataset_product_installs (
    id                      UUID PRIMARY KEY,
    product_id              UUID NOT NULL
                                REFERENCES marketplace_dataset_products(id) ON DELETE CASCADE,
    target_project_id       UUID NOT NULL,
    target_dataset_rid      TEXT NOT NULL,
    -- Mirror of the product's `bootstrap_mode` at install time so the
    -- install row remains self-contained even if the product is
    -- republished with a different default.
    bootstrap_mode          TEXT NOT NULL
                                CHECK (bootstrap_mode IN ('schema-only','with-snapshot')),
    status                  TEXT NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','ready','failed')),
    -- Free-form details (e.g. files copied count, error messages)
    -- written by the runner.
    details                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    installed_by            UUID,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at            TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_marketplace_dataset_product_installs_product
    ON marketplace_dataset_product_installs(product_id);
CREATE INDEX IF NOT EXISTS idx_marketplace_dataset_product_installs_target
    ON marketplace_dataset_product_installs(target_project_id, target_dataset_rid);
