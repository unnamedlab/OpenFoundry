# `connector-management-service` — Kubernetes binding

This service now targets the consolidated CNPG topology. Its SQL ownership is
the `connector_management` schema inside `pg-runtime-config`, used only for
connector/source definitions and other low-frequency control-plane metadata.
It also writes transactional outbox rows into the shared `outbox` schema of
the same database so Debezium can publish connector lifecycle changes without
2PC. High-frequency ingestion runtime state such as `sync_jobs`, retries,
attempt history and execution status must not be duplicated here.

Runtime dispatch uses `INGESTION_REPLICATION_GRPC_URL` and calls
`ingestion-replication-service`'s `IngestionControlPlane/CreateIngestJob`
surface. The retired HTTP sync endpoint and the local `sync_jobs` model are no
longer part of this service contract.

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
| Schema | `connector_management` |
| Service role | `svc_connector_management` |
| Debezium | [`infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-runtime-config.yaml`](../../../infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-runtime-config.yaml) |

## DSN injection

`AppConfig.database_url` (`src/config.rs`) is loaded from `DATABASE_URL`.
Deployments should consume the consolidated `<bc>-db-dsn` Secret contract, not
the old CNPG-managed per-cluster app Secret:

* `DATABASE_URL` → `connector-management-db-dsn.writer_url`
* `DATABASE_READ_URL` → `connector-management-db-dsn.reader_url`

Both URLs must set `search_path=connector_management`; see
[`infra/k8s/helm/open-foundry/DATABASE_URL.md`](../../../infra/k8s/helm/open-foundry/DATABASE_URL.md).

## Operational

* Manual failover and cluster operations live in
  [`infra/runbooks/cnpg.md`](../../../infra/runbooks/cnpg.md).
* Consolidation rationale lives in
  [ADR-0024](../../../docs/architecture/adr/ADR-0024-postgres-consolidation.md).
