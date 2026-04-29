# Deployment Model

OpenFoundry currently supports two complementary deployment modes in-repository:

- local developer runtime with Docker-backed infrastructure and host-run services
- Kubernetes-oriented delivery through the Helm chart under `infra/k8s/helm/open-foundry`

## Local Infrastructure

The Compose stack defines:

- PostgreSQL
- Valkey (Redis-protocol compatible; OSS BSD-3 image `valkey/valkey:8-alpine`)
- NATS
- RustFS (S3-compatible, Apache-2.0; replaces MinIO for development)
- Vespa Lite (single-node `vespaengine/vespa`, Apache-2.0) for hybrid
  BM25 + vector + filter + ranking search; same engine as production
  (see [ADR-0007](../architecture/adr/ADR-0007-search-engine-choice.md)
  and `infra/runbooks/vespa.md`)
- pgvector (extensión sobre PostgreSQL)
- Apache Polaris (Iceberg REST Catalog, Apache-2.0) — sólo en el stack
  Compose local; en Kubernetes ha sido reemplazado por Lakekeeper (ver
  [ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md)).

> Meilisearch ya **no** forma parte del stack DX por defecto. Sigue
> disponible como demostración de "first-run" bajo el perfil opcional
> `--profile demo` de `infra/docker-compose.dev.yml`; ningún servicio ni
> test depende de él (consolidación 2026-04 en
> [ADR-0007](../architecture/adr/ADR-0007-search-engine-choice.md)).

> Qdrant se retira por restricción de licencia OSS; sustituto futuro: Vespa
> (Apache-2.0). Por ahora pgvector cubre el caso embebido. La búsqueda
> lexical + vectorial + ranking en producción se concentra en Vespa, no en
> OpenSearch — ver
> [ADR-0007](../architecture/adr/ADR-0007-search-engine-choice.md).

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

## Object Storage Backend

OpenFoundry talks to its object store exclusively through `libs/storage-abstraction`,
which speaks the S3 API. Switching backends is therefore a configuration-only
change driven by three environment variables resolved from the platform secret
referenced by `global.existingSecret`:

| Variable                  | Description                                           |
|---------------------------|-------------------------------------------------------|
| `OBJECT_STORE_ENDPOINT`   | Base URL of the S3-compatible endpoint                |
| `OBJECT_STORE_ACCESS_KEY` | Access key id                                         |
| `OBJECT_STORE_SECRET_KEY` | Secret access key                                     |

### Development (RustFS)

In dev we run **RustFS** (Apache-2.0, S3-compatible) — not MinIO. The Helm
overlay `values-dev.yaml` already pins `objectStore.backend: rustfs` and
endpoint `http://rustfs:9000`. Credentials live in the dev secret
`open-foundry-dev-env`.

### Production (Ceph RGW via Rook)

In prod the backend is **Ceph RGW** operated by **Rook** (Apache-2.0). The
Helm overlay `values-prod.yaml` sets:

```yaml
objectStore:
  backend: ceph
  endpoint: http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80
```

To deploy / re-point production at Ceph:

1. Apply the Rook stack — either via the Terraform module
   `infra/terraform/modules/ceph` (recommended) or by `kubectl apply -f
   infra/k8s/rook/`. See `infra/runbooks/ceph.md`.
2. Wait until the `ObjectBucketClaim`s for `openfoundry-datasets`,
   `openfoundry-models`, and `openfoundry-iceberg` reach `Bound`.
3. Project the OBC credentials and the RGW endpoint into the platform
   secret `open-foundry-prod-env`:

   ```bash
   ACCESS_KEY=$(kubectl -n openfoundry get secret openfoundry-datasets \
     -o jsonpath='{.data.AWS_ACCESS_KEY_ID}' | base64 -d)
   SECRET_KEY=$(kubectl -n openfoundry get secret openfoundry-datasets \
     -o jsonpath='{.data.AWS_SECRET_ACCESS_KEY}' | base64 -d)

   kubectl -n openfoundry create secret generic open-foundry-prod-env \
     --from-literal=OBJECT_STORE_ENDPOINT=http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80 \
     --from-literal=OBJECT_STORE_ACCESS_KEY="${ACCESS_KEY}" \
     --from-literal=OBJECT_STORE_SECRET_KEY="${SECRET_KEY}" \
     --dry-run=client -o yaml | kubectl apply -f -
   ```

4. Roll the workloads to pick up the new secret:

   ```bash
   helm upgrade open-foundry infra/k8s/helm/open-foundry \
     -n openfoundry \
     -f infra/k8s/helm/open-foundry/values.yaml \
     -f infra/k8s/helm/open-foundry/values-prod.yaml
   ```

The full E2E procedure (OBC creation, credential extraction, smoke test,
expansion, disaster recovery) lives in `infra/runbooks/ceph.md`.
## Iceberg REST Catalog

OpenFoundry usa **Lakekeeper** como Iceberg REST Catalog en Kubernetes, por
[ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md).
El antiguo subchart `charts/iceberg-catalog` (Apache Polaris) fue retirado:
ya no forma parte del Helm chart `infra/k8s/helm/open-foundry`. La URL del
catálogo REST en el clúster se publica como `icebergRestCatalog.url` en
`infra/k8s/helm/open-foundry/values.yaml` y los manifiestos vivos están bajo
`infra/k8s/lakekeeper/` (ver `infra/k8s/lakekeeper/README.md`).

### Local (Docker Compose)

El stack Compose para desarrollo todavía levanta una instancia de
[Apache Polaris](https://polaris.apache.org/) como Iceberg REST Catalog DX
(`apache/polaris`, Apache-2.0) con persistencia `relational-jdbc` sobre
PostgreSQL. Compose declara dos servicios:

- `iceberg-catalog-bootstrap` — corre `apache/polaris-admin-tool` una vez
  para crear el realm y las credenciales de cliente raíz. Depende de
  `postgres` y termina con código 0 cuando concluye.
- `iceberg-catalog` — corre `apache/polaris`, expone la API REST en el
  puerto host `8181` (`/api/catalog/v1/...`) y el endpoint de management
  Quarkus en `8182` (`/q/health/*`, `/q/metrics`). La persistencia apunta
  a la base `openfoundry_iceberg_catalog` creada por
  `infra/init-db/01-create-databases.sh` a partir de
  `POSTGRES_MULTIPLE_DATABASES`.

Levantarlo aislado:

```bash
docker compose -f infra/docker-compose.yml up iceberg-catalog
# luego
curl -s http://localhost:8181/api/catalog/v1/config?warehouse=openfoundry
```

Esta capa es **sólo para DX**: en Kubernetes el catálogo de referencia es
Lakekeeper.

### Variables de entorno (Compose)

Las siguientes variables son leídas por el stack Compose (defaults):

| Variable | Default | Propósito |
| --- | --- | --- |
| `OPENFOUNDRY_POLARIS_IMAGE` | `apache/polaris:1.4.0` | Imagen del servidor Polaris |
| `OPENFOUNDRY_POLARIS_ADMIN_IMAGE` | `apache/polaris-admin-tool:1.4.0` | Imagen del bootstrap |
| `OPENFOUNDRY_ICEBERG_CATALOG_HOST_PORT` | `8181` | Puerto host de la API REST |
| `OPENFOUNDRY_ICEBERG_CATALOG_MGMT_HOST_PORT` | `8182` | Puerto host de management |
| `OPENFOUNDRY_ICEBERG_CATALOG_DB` | `openfoundry_iceberg_catalog` | Base de datos backend |
| `OPENFOUNDRY_ICEBERG_CATALOG_REALM` | `openfoundry` | Nombre de realm Polaris |
| `OPENFOUNDRY_ICEBERG_CATALOG_CLIENT_ID` | `root` | Identificador de cliente raíz |
| `OPENFOUNDRY_ICEBERG_CATALOG_CLIENT_SECRET` | `s3cr3t` | Secret raíz (rotar fuera de dev) |
| `OPENFOUNDRY_POSTGRES_EXTRA_DATABASES` | `openfoundry_iceberg_catalog` | DBs extra creadas en el primer arranque de Postgres |

### Kubernetes

En Kubernetes el catálogo Iceberg lo provee **Lakekeeper** (manifiestos en
`infra/k8s/lakekeeper/`). El subchart `charts/iceberg-catalog` (Polaris)
existió como alternativa interna pero fue retirado por
[ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md);
el chart parent ya no lo declara como dependencia.
