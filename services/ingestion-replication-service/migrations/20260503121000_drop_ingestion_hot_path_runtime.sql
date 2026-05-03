-- S6.6: CDC checkpoints are runtime recovery state and are now persisted by
-- the CDC worker outside Postgres (`OPENFOUNDRY_INGESTION_RUNTIME_DIR`).
-- The only SQL table left in this schema is low-frequency IngestJob desired
-- state plus the names of materialised Kubernetes resources.
DROP TABLE IF EXISTS ingestion_checkpoints;
