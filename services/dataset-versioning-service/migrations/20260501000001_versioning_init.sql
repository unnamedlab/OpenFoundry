-- dataset-versioning-service: schema espejo de data-asset-catalog-service
-- ampliado con la semántica Foundry de transacciones y branching.
--
-- Convención RID: el catálogo expone datasets identificados por RID textual
-- (ri.foundry.main.dataset.<uuid>); aquí mantenemos el `id UUID` como clave
-- interna y `rid TEXT UNIQUE` como identificador público estable.

CREATE TABLE IF NOT EXISTS datasets (
    id              UUID PRIMARY KEY,
    rid             TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    format          TEXT NOT NULL DEFAULT 'parquet',
    storage_path    TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    row_count       BIGINT NOT NULL DEFAULT 0,
    owner_id        UUID NOT NULL,
    tags            TEXT[] NOT NULL DEFAULT '{}',
    current_version INT NOT NULL DEFAULT 1,
    active_branch   TEXT NOT NULL DEFAULT 'master',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_datasets_owner ON datasets(owner_id);
CREATE INDEX IF NOT EXISTS idx_datasets_name  ON datasets(name);
CREATE INDEX IF NOT EXISTS idx_datasets_tags  ON datasets USING GIN(tags);

-- Branches Foundry-style: rama raíz (parent NULL) o rama hija. El puntero
-- `head_transaction_id` es análogo al HEAD de una rama Git.
CREATE TABLE IF NOT EXISTS dataset_branches (
    id                  UUID PRIMARY KEY,
    dataset_id          UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,
    parent_branch_id    UUID REFERENCES dataset_branches(id) ON DELETE SET NULL,
    head_transaction_id UUID,
    -- Compat con el esquema legacy (usado por handlers/branches.rs):
    version             INT  NOT NULL DEFAULT 1,
    base_version        INT  NOT NULL DEFAULT 1,
    description         TEXT NOT NULL DEFAULT '',
    is_default          BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(dataset_id, name)
);

CREATE INDEX IF NOT EXISTS idx_dataset_branches_dataset
    ON dataset_branches(dataset_id);
CREATE INDEX IF NOT EXISTS idx_dataset_branches_parent
    ON dataset_branches(parent_branch_id);

-- Versions retro-compatible con el catálogo. `transaction_id` ata cada
-- versión a la transacción que la produjo.
CREATE TABLE IF NOT EXISTS dataset_versions (
    id              UUID PRIMARY KEY,
    dataset_id      UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    version         INT  NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    row_count       BIGINT NOT NULL DEFAULT 0,
    storage_path    TEXT NOT NULL,
    transaction_id  UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(dataset_id, version)
);

CREATE INDEX IF NOT EXISTS idx_versions_dataset ON dataset_versions(dataset_id);

-- Transacciones Foundry-style.
--   tx_type:   SNAPSHOT | APPEND | UPDATE | DELETE
--   status:    OPEN     | COMMITTED | ABORTED
-- Invariante crítica: una sola transacción OPEN por (dataset, branch).
-- Se enforce con un índice parcial UNIQUE.
CREATE TABLE IF NOT EXISTS dataset_transactions (
    id              UUID PRIMARY KEY,
    dataset_id      UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    branch_id       UUID NOT NULL REFERENCES dataset_branches(id) ON DELETE CASCADE,
    branch_name     TEXT NOT NULL,
    tx_type         TEXT NOT NULL CHECK (tx_type IN ('SNAPSHOT','APPEND','UPDATE','DELETE')),
    status          TEXT NOT NULL DEFAULT 'OPEN'
                        CHECK (status IN ('OPEN','COMMITTED','ABORTED')),
    -- Compat con `record_committed_transaction` que usa `operation`/`view_id`:
    operation       TEXT NOT NULL DEFAULT '',
    view_id         UUID,
    summary         TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    providence      JSONB NOT NULL DEFAULT '{}',
    started_by      UUID,
    started_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    committed_at    TIMESTAMPTZ,
    aborted_at      TIMESTAMPTZ,
    -- Mantener el nombre legacy `created_at` por compatibilidad con queries existentes:
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_dataset_transactions_dataset_started
    ON dataset_transactions(dataset_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_dataset_transactions_branch
    ON dataset_transactions(branch_id, started_at DESC);

-- Una sola transacción OPEN por rama (cumple el "open transaction guarantee"
-- del documento Foundry "Datasets" / "Branching").
CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_transactions_one_open_per_branch
    ON dataset_transactions(branch_id)
    WHERE status = 'OPEN';

-- Vistas de dataset (snapshot lógico de archivos visibles en un punto).
CREATE TABLE IF NOT EXISTS dataset_views (
    id                  UUID PRIMARY KEY,
    dataset_id          UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    branch_id           UUID NOT NULL REFERENCES dataset_branches(id) ON DELETE CASCADE,
    name                TEXT NOT NULL DEFAULT '',
    head_transaction_id UUID NOT NULL REFERENCES dataset_transactions(id) ON DELETE CASCADE,
    computed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    file_count          INT  NOT NULL DEFAULT 0,
    size_bytes          BIGINT NOT NULL DEFAULT 0,
    metadata            JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(dataset_id, branch_id, head_transaction_id)
);

CREATE INDEX IF NOT EXISTS idx_dataset_views_dataset_branch
    ON dataset_views(dataset_id, branch_id, computed_at DESC);

-- Archivos efectivos de cada vista (resultado del algoritmo
-- SNAPSHOT/APPEND/UPDATE/DELETE descrito en Datasets.md).
CREATE TABLE IF NOT EXISTS dataset_view_files (
    view_id         UUID NOT NULL REFERENCES dataset_views(id) ON DELETE CASCADE,
    logical_path    TEXT NOT NULL,
    physical_path   TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    introduced_by   UUID REFERENCES dataset_transactions(id) ON DELETE SET NULL,
    PRIMARY KEY (view_id, logical_path)
);

-- Archivos por transacción (entrada bruta para el cálculo de vistas).
CREATE TABLE IF NOT EXISTS dataset_transaction_files (
    transaction_id  UUID NOT NULL REFERENCES dataset_transactions(id) ON DELETE CASCADE,
    logical_path    TEXT NOT NULL,
    physical_path   TEXT NOT NULL DEFAULT '',
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    op              TEXT NOT NULL DEFAULT 'ADD'
                        CHECK (op IN ('ADD','REPLACE','REMOVE')),
    PRIMARY KEY (transaction_id, logical_path)
);
