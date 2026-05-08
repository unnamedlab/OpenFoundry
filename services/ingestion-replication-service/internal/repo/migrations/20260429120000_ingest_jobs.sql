-- Control-plane: persisted IngestJob records. The actual Kafka/Flink
-- workloads live as Strimzi/Flink-Operator custom resources in the cluster;
-- this table is the source of truth for "what jobs has the operator been
-- asked to materialise" and is used by the reconcile loop.
CREATE TABLE IF NOT EXISTS ingest_jobs (
    id                      UUID PRIMARY KEY,
    name                    TEXT NOT NULL,
    namespace               TEXT NOT NULL,
    spec                    JSONB NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'pending',
    kafka_connector_name    TEXT,
    flink_deployment_name   TEXT,
    error                   TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ingest_jobs_namespace_name
    ON ingest_jobs(namespace, name);

CREATE INDEX IF NOT EXISTS idx_ingest_jobs_status
    ON ingest_jobs(status);
