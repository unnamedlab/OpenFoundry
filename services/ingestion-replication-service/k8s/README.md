# `ingestion-replication-service` — Kubernetes binding

This service now targets the consolidated CNPG topology. Its SQL ownership is
the `ingestion_replication` schema inside `pg-runtime-config`, but only for
low-traffic desired-state metadata. `ingest_jobs` stores the submitted
`IngestJobSpec` and references to materialised Kubernetes resources; runtime
state is hydrated from ConfigMaps/CR state and CDC checkpoints live outside
Postgres. It must not own `connections`, `sync_jobs`, retries, attempt history
or operational checkpoint tables.

## Backing CNPG cluster

| Field | Value |
| --- | --- |
| Manifest | [`infra/k8s/platform/manifests/cnpg/clusters/pg-runtime-config.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-runtime-config.yaml) |
| `kind` | `Cluster` (`postgresql.cnpg.io/v1`, CloudNativePG / cloudnative-pg) |
| Cluster name | `pg-runtime-config` |
| Namespace | `openfoundry` |
| Writer DNS | `pg-runtime-config-pooler-rw.openfoundry.svc.cluster.local:5432` |
| Reader DNS | `pg-runtime-config-ro.openfoundry.svc.cluster.local:5432` |
| Database | `of_runtime_config` |
| Schema | `ingestion_replication` |
| Service role | `svc_ingestion_replication` |

## DSN injection

`AppConfig.database_url` (`src/app_config.rs`) is loaded from `DATABASE_URL`.
Deployments should consume the consolidated `<bc>-db-dsn` Secret contract, not
the retired per-service CNPG Secret:

* `DATABASE_URL` → `ingestion-replication-db-dsn.writer_url`
* `DATABASE_READ_URL` → `ingestion-replication-db-dsn.reader_url`

Both URLs must set `search_path=ingestion_replication`; see
[`infra/k8s/helm/open-foundry/DATABASE_URL.md`](../../../infra/k8s/helm/open-foundry/DATABASE_URL.md).

## Operational

* Manual failover and cluster operations live in
  [`infra/runbooks/cnpg.md`](../../../infra/runbooks/cnpg.md).
* Consolidation rationale lives in
  [ADR-0024](../../../docs/architecture/adr/ADR-0024-postgres-consolidation.md).
