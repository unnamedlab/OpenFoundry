# `identity-federation-service` — Kubernetes binding

This service runs as part of the umbrella Helm chart at
`infra/k8s/helm/open-foundry/`. The only state owned by this bounded
context is the **identity-federation-service** Postgres schema
(migrations under `../migrations/`), which is provisioned by a
dedicated CloudNativePG (CNPG, `postgresql.cnpg.io`) `Cluster`.

## Backing CNPG `Cluster`

| Field          | Value                                                                |
|----------------|----------------------------------------------------------------------|
| Manifest       | [`infra/k8s/platform/manifests/cnpg/clusters/identity-federation-pg.yaml`](../../../infra/k8s/platform/manifests/cnpg/clusters/identity-federation-pg.yaml) |
| `kind`         | `Cluster` (`postgresql.cnpg.io/v1`, CloudNativePG / cloudnative-pg)  |
| Cluster name   | `identity-federation-pg`                                             |
| Namespace      | `openfoundry`                                                        |
| Read/write DNS | `identity-federation-pg-rw.openfoundry.svc.cluster.local:5432`       |
| Read-only DNS  | `identity-federation-pg-ro.openfoundry.svc.cluster.local:5432`       |
| Database       | `identity_federation`                                                |
| App secret     | `identity-federation-pg-app` (CNPG-managed; carries `username`, `password`, `uri`) |
| Backup secret  | `identity-federation-pg-backup` (S3 credentials for barman-cloud)    |

## DSN injection

`AppConfig.database_url` (`src/config.rs`) is loaded by
`config::Environment` from the `DATABASE_URL` environment variable.
The umbrella chart projects that variable directly from the
CNPG-managed `<cluster>-app` Secret via
`services.identity-federation-service.envSecrets.DATABASE_URL`
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
