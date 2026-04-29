# Deployment Model

OpenFoundry currently supports two complementary deployment modes in-repository:

- local developer runtime with Docker-backed infrastructure and host-run services
- Kubernetes-oriented delivery through the Helm chart under `infra/k8s/helm/open-foundry`

## Local Infrastructure

The Compose stack defines:

- PostgreSQL
- Valkey (Redis-protocol compatible; OSS BSD-3 image `valkey/valkey:8-alpine`)
- NATS
- MinIO
- Meilisearch
- pgvector (extensión sobre PostgreSQL)
- Apache Polaris (Iceberg REST Catalog, Apache-2.0)

> Qdrant se retira por restricción de licencia OSS; sustituto futuro: Vespa
> (Apache-2.0). Por ahora pgvector cubre el caso embebido.

Development overrides live in `infra/docker-compose.dev.yml`.

## Kubernetes Packaging

The Helm chart lives in:

```text
infra/k8s/helm/open-foundry
```

Important templates include:

- `deployment.yaml`
- `service.yaml`
- `ingress.yaml`
- `networkpolicy.yaml`
- `hpa.yaml`
- `scaledobject.yaml`
- `platform-profile-configmap.yaml`
- `apollo-cronjob.yaml`
- `poddisruptionbudget.yaml`

## Environment Overlays

The chart ships with multiple value overlays:

- `values.yaml`
- `values-dev.yaml`
- `values-staging.yaml`
- `values-prod.yaml`
- `values-airgap.yaml`
- `values-apollo.yaml`
- `values-multicloud.yaml`
- `values-sovereign-eu.yaml`

This layout signals that the repository is designed to support more than one operational profile instead of a single one-size-fits-all manifest.

## Local Commands

Common local deployment and runtime entry points are exposed in `justfile`:

```bash
just infra-up
just infra-down
just infra-up-full
just dev-stack
just dev-stack-fast
just smoke
```

## Chart Validation

The repository includes a `helm-check` recipe that:

- lints the base chart
- renders the base chart
- renders staging and production overlays

That gives maintainers a quick pre-merge validation path for deployment changes.

## Iceberg REST Catalog (Apache Polaris)

OpenFoundry ships an [Apache Polaris](https://polaris.apache.org/) deployment
as the Iceberg REST Catalog used by the data-plane services (Tasks 1.1–1.4).
Polaris is Apache-2.0 licensed and runs with a `relational-jdbc` persistence
backend on PostgreSQL — there is no embedded EclipseLink/H2 single point of
failure.

### Local (Docker Compose)

Compose declares two services:

- `iceberg-catalog-bootstrap` — runs the `apache/polaris-admin-tool` image
  once to create the realm schema and the root client credentials. It depends
  on `postgres` becoming healthy and exits 0 on success.
- `iceberg-catalog` — runs `apache/polaris`, exposes the REST API on host
  port `8181` (`/api/catalog/v1/...`) and the Quarkus management endpoint on
  `8182` (`/q/health/*`, `/q/metrics`). Persistence points at the
  `openfoundry_iceberg_catalog` database created by
  `infra/init-db/01-create-databases.sh` from the
  `POSTGRES_MULTIPLE_DATABASES` environment variable.

Bring it up standalone with:

```bash
docker compose -f infra/docker-compose.yml up iceberg-catalog
# then
curl -s http://localhost:8181/api/catalog/v1/config?warehouse=openfoundry
```

The integration tests for Task 1.4 should target
`http://iceberg-catalog:8181/api/catalog` from inside the compose network.

### Environment variables

The following variables are read by the Compose stack (defaults shown):

| Variable | Default | Purpose |
| --- | --- | --- |
| `OPENFOUNDRY_POLARIS_IMAGE` | `apache/polaris:1.4.0` | Polaris server image |
| `OPENFOUNDRY_POLARIS_ADMIN_IMAGE` | `apache/polaris-admin-tool:1.4.0` | Bootstrap image |
| `OPENFOUNDRY_ICEBERG_CATALOG_HOST_PORT` | `8181` | REST API host port |
| `OPENFOUNDRY_ICEBERG_CATALOG_MGMT_HOST_PORT` | `8182` | Management host port |
| `OPENFOUNDRY_ICEBERG_CATALOG_DB` | `openfoundry_iceberg_catalog` | Backing database |
| `OPENFOUNDRY_ICEBERG_CATALOG_REALM` | `openfoundry` | Polaris realm name |
| `OPENFOUNDRY_ICEBERG_CATALOG_CLIENT_ID` | `root` | Root client identifier |
| `OPENFOUNDRY_ICEBERG_CATALOG_CLIENT_SECRET` | `s3cr3t` | Root client secret (rotate in non-dev) |
| `OPENFOUNDRY_POSTGRES_EXTRA_DATABASES` | `openfoundry_iceberg_catalog` | Extra DBs created on first Postgres boot |

### Kubernetes (Helm subchart)

Polaris is packaged as a subchart at
`infra/k8s/helm/open-foundry/charts/iceberg-catalog`. It is wired into the
parent chart as a conditional dependency and is **disabled by default**:

```yaml
# values.yaml
icebergCatalog:
  enabled: false
```

Enable it in any overlay (or via `--set icebergCatalog.enabled=true`). The
subchart renders, with HA defaults:

- `Deployment` with `replicaCount: 3`, hostname topology spread, and
  pod anti-affinity (preferred).
- `PodDisruptionBudget` with `minAvailable: 2` so a node drain or rolling
  upgrade never reduces the catalog below quorum.
- `Service` exposing `8181` (catalog) and `8182` (management).
- `Job` (Helm `pre-install,pre-upgrade` hook) that runs
  `apache/polaris-admin-tool bootstrap` against the configured database.
- Optional `postgresql.cnpg.io/v1 Cluster` resource provisioned through the
  CloudNativePG operator (`postgres.cnpg.enabled: true`, default `instances: 3`).
  When CNPG is disabled the chart points Polaris at an existing PostgreSQL
  via `postgres.external.host`/`port` (or `postgres.external.jdbcUrl`).

Render and review the manifests with:

```bash
helm dependency update infra/k8s/helm/open-foundry
helm template open-foundry infra/k8s/helm/open-foundry \
  --namespace openfoundry \
  -f infra/k8s/helm/open-foundry/values.yaml \
  --set icebergCatalog.enabled=true
```

In production set `icebergCatalog.realm.existingSecret` and
`icebergCatalog.postgres.existingSecret` so credentials are sourced from
externally-managed Secrets instead of generated by the chart.
