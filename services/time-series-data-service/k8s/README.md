# `time-series-data-service` — Kubernetes binding

This service runs as part of the umbrella Helm chart at
`infra/k8s/helm/open-foundry/`. The only state owned by this bounded
context is the **time-series-data-service** Postgres schema (migrations under
`../migrations/`), which is provisioned by a dedicated
CloudNativePG (CNPG, `postgresql.cnpg.io`) `Cluster`.

## Backing CNPG `Cluster`

| Field          | Value                                                              |
|----------------|--------------------------------------------------------------------|
| Manifest       | [`infra/k8s/platform/manifests/cnpg/clusters/time-series-data-pg.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/time-series-data-pg.yaml) |
| `kind`         | `Cluster` (`postgresql.cnpg.io/v1`, CloudNativePG / cloudnative-pg)|
| Cluster name   | `time-series-data-pg`                                                   |
| Namespace      | `openfoundry`                                                      |
| Read/write DNS | `time-series-data-pg-rw.openfoundry.svc.cluster.local:5432`             |
| Read-only DNS  | `time-series-data-pg-ro.openfoundry.svc.cluster.local:5432`             |
| Database       | `time_series_data`                                                       |
| App secret     | `time-series-data-pg-app` (CNPG-managed; carries `username`, `password`, `uri`) |
| Backup secret  | `time-series-data-pg-backup` (S3 credentials for barman-cloud)          |

## DSN injection

`AppConfig.database_url` (`src/config.rs`) is loaded by
`config::Environment` from the `DATABASE_URL` environment variable.
The umbrella chart projects that variable directly from the
CNPG-managed `<cluster>-app` Secret via
`services.time-series-data-service.envSecrets.DATABASE_URL`
in `infra/k8s/helm/open-foundry/values.yaml`. The DSN is **not**
hardcoded in `config/default.toml` or `config/prod.toml`.

The same `DATABASE_URL` is consumed by `sqlx migrate run` inside the
container's entrypoint, so migrations always target the per-bounded-context
CNPG cluster — never a shared instance.

## Operational

* Manual failover, backup location, and create-a-cluster recipe live in
  [`infra/runbooks/cnpg.md`](../../../infra/runbooks/cnpg.md).
* Architectural decision: [ADR-0010 — CloudNativePG as the single
  Postgres operator](../../../docs/architecture/adr/ADR-0010-cnpg-postgres-operator.md).
