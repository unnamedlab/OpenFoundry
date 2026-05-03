# Deployment Model

OpenFoundry currently supports two complementary deployment modes in-repository:

- local developer runtime with Docker-backed infrastructure and host-run services
- Kubernetes-oriented delivery through the split Helm releases under `infra/k8s/helm/`

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

> El stack de Compose por defecto **ya no incluye un Iceberg REST
> Catalog**. Apache Polaris fue retirado del Compose el 2026-04-30
> (PR #61) y de los charts Helm de OpenFoundry; en Kubernetes el único
> catálogo soportado es **Lakekeeper** (`infra/k8s/platform/manifests/lakekeeper/`),
> conforme a
> [ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md).
> Los flujos DX que necesiten un catálogo Iceberg local deben apuntar
> a un Lakekeeper desplegado fuera del Compose (p. ej. minikube/kind +
> `infra/k8s/platform/manifests/lakekeeper/`); ningún servicio del workspace lo consume
> directamente desde el stack Compose.

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

Kubernetes delivery is split into two layers:

- `infra/k8s/platform/` owns third-party releases, operator CRs,
  bootstrap manifests and runtime packages.
- `infra/k8s/helm/` owns the five OpenFoundry application releases.

The app layer is split into five release-aligned charts:

```text
infra/k8s/helm/
├── of-platform
├── of-data-engine
├── of-ontology
├── of-ml-aip
├── of-apps-ops
└── of-shared
```

Cross-release environment posture lives in `infra/k8s/helm/profiles/`,
while each release keeps its own `values-{dev,staging,prod}.yaml`.
Install platform first, then apps:

```bash
cd infra/k8s/platform && helmfile -e prod apply
cd infra/k8s/helm && helmfile -e prod apply
```

The supported render entrypoints are:

```bash
cd infra/k8s/platform && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" > /tmp/openfoundry-platform-prod.yaml
cd infra/k8s/helm && helmfile -e prod template > /tmp/openfoundry-prod.yaml
```

## Environment Overlays

Shared profiles:

- `infra/k8s/helm/profiles/values-dev.yaml`
- `infra/k8s/helm/profiles/values-staging.yaml`
- `infra/k8s/helm/profiles/values-prod.yaml`
- `infra/k8s/helm/profiles/values-airgap.yaml`
- `infra/k8s/helm/profiles/values-apollo.yaml`
- `infra/k8s/helm/profiles/values-multicloud.yaml`
- `infra/k8s/helm/profiles/values-sovereign-eu.yaml`

Per-release overlays:

- `infra/k8s/helm/of-platform/values-{dev,staging,prod}.yaml`
- `infra/k8s/helm/of-data-engine/values-{dev,staging,prod}.yaml`
- `infra/k8s/helm/of-ontology/values-{dev,staging,prod}.yaml`
- `infra/k8s/helm/of-ml-aip/values-{dev,staging,prod}.yaml`
- `infra/k8s/helm/of-apps-ops/values-{dev,staging,prod}.yaml`

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

- lints the platform layer and renders its production bundle
- refreshes dependencies for the five releases
- lints each app release against the production profile
- renders the full dev/staging/prod bundle

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

In dev we run **RustFS** (Apache-2.0, S3-compatible) — not MinIO. The
shared profile `infra/k8s/helm/profiles/values-dev.yaml` pins
`objectStore.backend: rustfs` and endpoint `http://rustfs:9000`.
Credentials live in the dev secret `open-foundry-dev-env`.

### Production (Ceph RGW via Rook)

In prod the backend is **Ceph RGW** operated by **Rook** (Apache-2.0). The
shared profile `infra/k8s/helm/profiles/values-prod.yaml` sets:

```yaml
objectStore:
  backend: ceph
  endpoint: http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80
```

To deploy / re-point production at Ceph:

1. Apply the Rook stack — either via the Terraform module
   `infra/terraform/modules/ceph` (recommended) or by `kubectl apply -f
   infra/k8s/platform/manifests/rook/`. See `infra/runbooks/ceph.md`.
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
   cd infra/k8s/helm && helmfile -e prod apply
   ```

The full E2E procedure (OBC creation, credential extraction, smoke test,
expansion, disaster recovery) lives in `infra/runbooks/ceph.md`.
## Iceberg REST Catalog

OpenFoundry usa **Lakekeeper** como Iceberg REST Catalog en Kubernetes, por
[ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md).
El antiguo subchart `charts/iceberg-catalog` (Apache Polaris) fue retirado:
ya no forma parte de los charts Helm de OpenFoundry. La URL del catálogo
REST en el clúster se publica como `icebergRestCatalog.url` en
`infra/k8s/helm/profiles/values-{dev,prod}.yaml` y los manifiestos vivos
están bajo `infra/k8s/platform/manifests/lakekeeper/` (ver `infra/k8s/platform/manifests/lakekeeper/README.md`).

### Local (Docker Compose)

El stack Compose para desarrollo **ya no levanta** un Iceberg REST Catalog
propio. Apache Polaris (`apache/polaris` + `apache/polaris-admin-tool`) y
sus servicios `iceberg-catalog-bootstrap` / `iceberg-catalog` fueron
eliminados de `infra/docker-compose.yml` el 2026-04-30 para cerrar la
divergencia compose ↔ Kubernetes que dejaba [ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md):
si Lakekeeper es el único catálogo en producción, exponer Polaris en el
DX por defecto generaba dependencias accidentales sobre un componente
retirado.

Los servicios que integran con Iceberg leen `ICEBERG_CATALOG_URL` cuando
se activa el backend Iceberg. En `dataset-versioning-service`, Iceberg es
el backend por defecto: si `DATASET_WRITER_BACKEND` no está definido, el
servicio arranca como `iceberg` y falla en arranque si
`ICEBERG_CATALOG_URL` no está definida. El writer legacy solo se usa
cuando el backend configurado es explícitamente `legacy`. Para ejercitar
el camino Iceberg en local,
apunta esa variable a un Lakekeeper externo accesible desde los
contenedores.

### Variables de entorno (Compose)

Sin servicio Polaris en Compose ya no quedan variables propias del
catálogo en `infra/docker-compose.yml`. La variable relacionada con
Iceberg es obligatoria para los servicios que arranquen con backend
Iceberg:

| Variable | Default | Propósito |
| --- | --- | --- |
| `ICEBERG_CATALOG_URL` | _unset_ | URL del Iceberg REST Catalog que consumirán los servicios con backend Iceberg. |
| `OPENFOUNDRY_POSTGRES_EXTRA_DATABASES` | _empty_ | DBs extra opcionales creadas en el primer arranque de Postgres por `infra/local/postgres-init/01-create-databases.sh`. |

### Kubernetes

En Kubernetes el catálogo Iceberg lo provee **Lakekeeper** (manifiestos en
`infra/k8s/platform/manifests/lakekeeper/`). El subchart `charts/iceberg-catalog` (Polaris)
existió como alternativa interna pero fue retirado por
[ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md);
el chart parent ya no lo declara como dependencia.
