# Deployment Model

OpenFoundry currently supports two complementary deployment modes in-repository:

- local developer runtime with Docker-backed infrastructure and host-run services
- Kubernetes-oriented delivery through the split Helm releases under `infra/helm/apps/`

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
- pgvector (extension on top of PostgreSQL)

> The default Compose stack **no longer includes an Iceberg REST
> Catalog**. Apache Polaris was removed from Compose on 2026-04-30
> (PR #61) and from the OpenFoundry Helm charts; on Kubernetes the only
> supported catalog is **Lakekeeper** (`infra/helm/infra/manifests/lakekeeper/`),
> in line with
> [ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md).
> DX flows that need a local Iceberg catalog must point to a Lakekeeper
> deployed outside of Compose (e.g. minikube/kind +
> `infra/helm/infra/manifests/lakekeeper/`); no workspace service consumes
> it directly from the Compose stack.

> Meilisearch is **no** longer part of the default DX stack. It remains
> available as a "first-run" demo under the optional
> `--profile demo` of `infra/compose/docker-compose.dev.yml`; no service or
> test depends on it (2026-04 consolidation in
> [ADR-0007](../architecture/adr/ADR-0007-search-engine-choice.md)).

> Qdrant is retired due to an OSS license restriction; future replacement: Vespa
> (Apache-2.0). For now pgvector covers the embedded case. Lexical + vector +
> ranking search in production is concentrated in Vespa, not in
> OpenSearch — see
> [ADR-0007](../architecture/adr/ADR-0007-search-engine-choice.md).

Development overrides live in `infra/compose/docker-compose.dev.yml`.

## Kubernetes Packaging

Kubernetes delivery is split into two layers:

- `infra/helm/infra/` owns third-party releases, operator CRs,
  bootstrap manifests and runtime packages.
- `infra/helm/apps/` owns the six OpenFoundry application releases (plus
  the `of-shared` library chart).

The app layer is split into six release-aligned charts:

```text
infra/helm/apps/
├── of-platform
├── of-data-engine
├── of-ontology
├── of-ml-aip
├── of-apps-ops
├── of-web
└── of-shared
```

Cross-release environment posture lives in `infra/helm/apps/profiles/`,
while each release keeps its own `values-{dev,staging,prod}.yaml`.
Install platform first, then apps:

```bash
cd infra/helm/infra && helmfile -e prod apply
cd infra/helm/apps && helmfile -e prod apply
```

The supported render entrypoints are:

```bash
cd infra/helm/infra && helmfile -e prod template --args "--api-versions monitoring.coreos.com/v1/PodMonitor" > /tmp/openfoundry-platform-prod.yaml
cd infra/helm/apps && helmfile -e prod template > /tmp/openfoundry-prod.yaml
```

## Environment Overlays

Shared profiles:

- `infra/helm/apps/profiles/values-dev.yaml`
- `infra/helm/apps/profiles/values-staging.yaml`
- `infra/helm/apps/profiles/values-prod.yaml`
- `infra/helm/apps/profiles/values-airgap.yaml`
- `infra/helm/apps/profiles/values-apollo.yaml`
- `infra/helm/apps/profiles/values-multicloud.yaml`
- `infra/helm/apps/profiles/values-sovereign-eu.yaml`

Per-release overlays:

- `infra/helm/apps/of-platform/values-{dev,staging,prod}.yaml`
- `infra/helm/apps/of-data-engine/values-{dev,staging,prod}.yaml`
- `infra/helm/apps/of-ontology/values-{dev,staging,prod}.yaml`
- `infra/helm/apps/of-ml-aip/values-{dev,staging,prod}.yaml`
- `infra/helm/apps/of-apps-ops/values-{dev,staging,prod}.yaml`

This layout signals that the repository is designed to support more than one operational profile instead of a single one-size-fits-all manifest.

## Local Commands

Common local deployment and runtime entry points are split between the
canonical `Makefile`, the Compose stack, and the Go `of-cli` smoke runner:

```bash
make tools
make build-services
cd infra/compose && docker compose up -d
go run ./tools/of-cli -- smoke run \
  --scenario smoke/scenarios/p2-runtime-critical-path.json \
  --output smoke/results/p2-runtime-critical-path.json
./smoke/chaos/run.sh
```

The root `justfile` is only a compatibility shim over `make`; do not add
new deployment recipes there unless they delegate to a Makefile target.

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
shared profile `infra/helm/apps/profiles/values-dev.yaml` pins
`objectStore.backend: rustfs` and endpoint `http://rustfs:9000`.
Credentials live in the dev secret `open-foundry-dev-env`.

### Production (Ceph RGW via Rook)

In prod the backend is **Ceph RGW** operated by **Rook** (Apache-2.0). The
shared profile `infra/helm/apps/profiles/values-prod.yaml` sets:

```yaml
objectStore:
  backend: ceph
  endpoint: http://rook-ceph-rgw-openfoundry.rook-ceph.svc:80
```

To deploy / re-point production at Ceph:

1. Apply the Rook stack — either via the Terraform module
   `infra/terraform/modules/ceph` (recommended) or by `kubectl apply -f
   infra/helm/infra/manifests/rook/`. See `infra/runbooks/ceph.md`.
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
   cd infra/helm/apps && helmfile -e prod apply
   ```

The full E2E procedure (OBC creation, credential extraction, smoke test,
expansion, disaster recovery) lives in `infra/runbooks/ceph.md`.
## Iceberg REST Catalog

OpenFoundry uses **Lakekeeper** as the Iceberg REST Catalog on Kubernetes, per
[ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md).
The former `charts/iceberg-catalog` subchart (Apache Polaris) has been retired:
it is no longer part of the OpenFoundry Helm charts. The in-cluster REST
catalog URL is published as `icebergRestCatalog.url` in
`infra/helm/apps/profiles/values-{dev,prod}.yaml`, and the live manifests
are under `infra/helm/infra/manifests/lakekeeper/` (see `infra/helm/infra/manifests/lakekeeper/README.md`).

### Local (Docker Compose)

The development Compose stack **no longer starts** its own Iceberg REST
Catalog. Apache Polaris (`apache/polaris` + `apache/polaris-admin-tool`) and
its `iceberg-catalog-bootstrap` / `iceberg-catalog` services were
removed from `infra/compose/docker-compose.yml` on 2026-04-30 to close the
compose ↔ Kubernetes divergence left by [ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md):
if Lakekeeper is the only catalog in production, exposing Polaris in
DX by default created accidental dependencies on a retired component.

Services that integrate with Iceberg read `ICEBERG_CATALOG_URL` when
the Iceberg backend is enabled. In `dataset-versioning-service`, Iceberg is
the default backend: if `DATASET_WRITER_BACKEND` is not set, the
service starts up as `iceberg` and fails on startup if
`ICEBERG_CATALOG_URL` is not set. The legacy writer is only used
when the configured backend is explicitly `legacy`. To exercise
the Iceberg path locally,
point that variable to an external Lakekeeper reachable from the
containers.

### Environment variables (Compose)

With no Polaris service in Compose there are no longer any catalog-specific
variables in `infra/compose/docker-compose.yml`. The Iceberg-related variable
is required for services that start with the Iceberg backend:

| Variable | Default | Purpose |
| --- | --- | --- |
| `ICEBERG_CATALOG_URL` | _unset_ | URL of the Iceberg REST Catalog consumed by services running the Iceberg backend. |
| `OPENFOUNDRY_POSTGRES_EXTRA_DATABASES` | _empty_ | Optional extra DBs created on the first Postgres startup by `infra/local/postgres-init/01-create-databases.sh`. |

### Kubernetes

On Kubernetes the Iceberg catalog is provided by **Lakekeeper** (manifests in
`infra/helm/infra/manifests/lakekeeper/`). The `charts/iceberg-catalog` subchart (Polaris)
existed as an internal alternative but was retired by
[ADR-0008](../architecture/adr/ADR-0008-iceberg-rest-catalog-lakekeeper.md);
the parent chart no longer declares it as a dependency.
