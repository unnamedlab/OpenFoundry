# Local Runtime Assets

This directory contains support files consumed by the root Compose files.
The Compose entrypoints stay at `infra/docker-compose.yml` and
`infra/docker-compose.dev.yml`; this directory keeps their mounted assets
out of the infrastructure root.

| Path | Consumed by |
| --- | --- |
| `postgres-init/` | `postgres` service, mounted at `/docker-entrypoint-initdb.d` |
| `nginx/` | `nginx` app-profile edge proxy |

Kubernetes-only manifests belong under `infra/k8s/platform/`.
