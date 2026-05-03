# Deployment Modes

OpenFoundry supports multiple operating profiles ranging from local development to opinionated Kubernetes overlays.

## Local Compose

The local stack lives under `infra/`:

- `infra/docker-compose.yml`
- `infra/docker-compose.dev.yml`

These files provide the backing services needed for day-to-day development and smoke execution. The `just infra-up` and `just dev-stack` commands are the normal contributor entrypoints. A monitoring overlay will be reintroduced as part of the formal observability work (T17); see `docs/observability/index.md`.

## Kubernetes Via Helm

Kubernetes delivery now uses five release-aligned charts under
`infra/k8s/helm/`:

- `of-platform`
- `of-data-engine`
- `of-ontology`
- `of-ml-aip`
- `of-apps-ops`

Cross-release environment posture lives under
`infra/k8s/helm/profiles/`:

- `values-dev.yaml`
- `values-staging.yaml`
- `values-prod.yaml`
- `values-multicloud.yaml`
- `values-airgap.yaml`
- `values-sovereign-eu.yaml`
- `values-apollo.yaml`

Each release keeps its own service-specific `values-{dev,staging,prod}.yaml`.

The supported operator entrypoints are:

```bash
cd infra/k8s/helm && helmfile -e prod apply
cd infra/k8s/helm && helmfile -e prod template > /tmp/openfoundry-prod.yaml
```

The Helm CI workflows lint every release and render the full bundle for
dev/staging/prod so template validity remains part of normal review.

## Terraform Assets

Terraform content is split into:

- `infra/terraform/modules/cdn`
- `infra/terraform/providers/openfoundry`

The custom provider directory also contains `provider.schema.json`, which is treated as a checked-in documentation and integration artifact.

## Docker Images

Selected services are published through `docker-publish.yml` using their service-local Dockerfiles. The workflow currently focuses on a subset of core services such as `gateway`, `auth-service`, `dataset-service`, `sql-bi-gateway-service`, `pipeline-service`, and `ontology-service`.

## Release Model

- container images are pushed through GitHub Actions
- tagged releases are published through `release.yml`
- docs are published independently through GitHub Pages

This separation keeps operational documentation deployable even when application release cadence changes.
