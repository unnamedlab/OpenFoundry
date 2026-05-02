-- T1.2 — Invariantes de transacción.
--
-- Refuerza el contrato Foundry sobre transacciones de dataset:
--
--   1) Una sola transacción OPEN por (dataset, rama).
--      Funcionalmente equivalente a UNIQUE(dataset_rid, branch_name)
--      WHERE state='OPEN', porque dataset_branches impone
--      UNIQUE(dataset_id, name) y branch_id se deriva de (dataset_id, name).
--      El índice ya existe en `20260501000001_versioning_init.sql` con el
--      nombre `uq_dataset_transactions_one_open_per_branch`. Se añade aquí
--      idempotentemente por explicitud.
--
--   2) Soporte para flags de dataset (e.g. `incremental_friendly = false`
--      tras un UPDATE). Se modela como columna JSONB `metadata` para no
--      filtrar semántica de un solo flag al esquema relacional.
--
--   3) Soporte para marcar transacciones previas como “históricas” tras
--      un SNAPSHOT (vía `dataset_transactions.metadata.historical = true`).
--      `dataset_transactions.metadata` ya existe en la migración inicial,
--      así que no se requiere DDL adicional para (3).

ALTER TABLE datasets
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX IF NOT EXISTS idx_datasets_metadata
    ON datasets USING GIN (metadata);

-- Re-declaración idempotente del invariante "una OPEN por rama".
CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_transactions_one_open_per_branch
    ON dataset_transactions(branch_id)
    WHERE status = 'OPEN';
