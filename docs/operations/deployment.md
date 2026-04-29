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
- Meilisearch
- pgvector (extensión sobre PostgreSQL)

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
