# `event-streaming-service` — Kubernetes binding

This service now targets the consolidated CNPG topology. Its SQL
ownership is the `event_streaming` schema inside `pg-schemas`, and it
also emits transactional outbox rows into the shared `outbox` schema in
the same database so Debezium can publish control-plane lifecycle
events.

## Backing CNPG `Cluster`

| Field          | Value                                                              |
|----------------|--------------------------------------------------------------------|
| Manifest       | [`infra/k8s/platform/manifests/cnpg/clusters/pg-schemas.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/pg-schemas.yaml) |
| `kind`         | `Cluster` (`postgresql.cnpg.io/v1`, CloudNativePG / cloudnative-pg) |
| Cluster name   | `pg-schemas` |
| Namespace      | `openfoundry` |
| Writer DNS     | `pg-schemas-pooler-rw.openfoundry.svc.cluster.local:5432` |
| Reader DNS     | `pg-schemas-ro.openfoundry.svc.cluster.local:5432` |
| Database       | `app` |
| Schema         | `event_streaming` |
| Service role   | `svc_event_streaming` |
| Debezium       | [`infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-schemas.yaml`](../../../infra/k8s/platform/manifests/debezium/kafka-connector-outbox-pg-schemas.yaml) |

## DSN injection

`AppConfig.database_url` (`src/config.rs`) is loaded by
`config::Environment` from the `DATABASE_URL` environment variable.
Deployments should consume the consolidated `<bc>-db-dsn` Secret
contract, with `search_path=event_streaming`. The same `DATABASE_URL`
is consumed by `sqlx migrate run`, so the service migration now
provisions both `event_streaming.*` and the shared `outbox.*` substrate
when absent.

## Operational

* Manual failover, backup location, and create-a-cluster recipe live in
  [`infra/runbooks/cnpg.md`](../../../infra/runbooks/cnpg.md).
* Consolidation rationale lives in
  [ADR-0024](../../../docs/architecture/adr/ADR-0024-postgres-consolidation.md).
