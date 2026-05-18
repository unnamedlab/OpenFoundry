# compute-module-service

## LLM quick context (current code)

Registers compute modules and related metadata for reusable compute/plugin-like units.

Agent note: current implementation is a compact HTTP service with config.yaml support.

Current surface:
- `/api/v1/compute-modules*`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `2` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `executionmode`, `handler`, `models`, `repo`, `server`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `CONFIG_FILE`

Keep this section in sync when changing routes, config, or persistence behavior.

CRUD and project-placement API for OpenFoundry Compute Module
resources. Implements item `CM.1` from
[docs/migration/foundry-compute-modules-1to1-checklist.md](../../docs/migration/foundry-compute-modules-1to1-checklist.md):
create, list, get, update metadata, move, duplicate, archive, restore,
and delete `compute_module` records, with function-mode or
pipeline-mode selection chosen at creation time.

Image, container, replica, scaling, function-spec, and pipeline-spec
resources are tracked by later items in the same checklist and will
live alongside this service or in sibling services per the suggested
service boundaries.

## Endpoints

All routes are mounted behind JWT auth (`Authorization: Bearer …`)
and registered with the capability catalog at
`GET /_meta/capabilities`.

| Method | Path                                          | Capability ID                          |
| ------ | --------------------------------------------- | -------------------------------------- |
| POST   | `/api/v1/compute-modules`                     | `compute-module.modules.create`        |
| GET    | `/api/v1/compute-modules`                     | `compute-module.modules.list`          |
| GET    | `/api/v1/compute-modules/{id}`                | `compute-module.modules.get`           |
| PATCH  | `/api/v1/compute-modules/{id}`                | `compute-module.modules.update`        |
| POST   | `/api/v1/compute-modules/{id}/move`           | `compute-module.modules.move`          |
| POST   | `/api/v1/compute-modules/{id}/duplicate`      | `compute-module.modules.duplicate`     |
| POST   | `/api/v1/compute-modules/{id}/archive`        | `compute-module.modules.archive`       |
| POST   | `/api/v1/compute-modules/{id}/restore`        | `compute-module.modules.restore`       |
| DELETE | `/api/v1/compute-modules/{id}`                | `compute-module.modules.delete`        |

`GET /healthz` and `GET /metrics` follow the OpenFoundry service
template.

## Build & run

```sh
go build -o bin/compute-module-service ./services/compute-module-service/cmd/compute-module-service
./bin/compute-module-service -config services/compute-module-service/config.yaml
```

Or via Docker:

```sh
docker build -t openfoundry/compute-module-service:dev -f services/compute-module-service/Dockerfile .
```

## Storage

The service ships with an in-memory repository
(`internal/repo.MemoryRepository`) that drives every CM.1 test path.
A Postgres-backed implementation will land alongside the goose
migration in `internal/repo/migrations/0001_compute_modules.sql`
once CM.2/CM.3 require persistent state.
