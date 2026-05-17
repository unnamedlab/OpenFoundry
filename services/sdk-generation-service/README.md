# sdk-generation-service

SDK + OpenAPI contract generation/publication/versioning service.

Today the binary wires server + auth + metrics so the documented
endpoints can be exercised end-to-end; wire format is pinned via
the model JSON tags.

## Endpoints

| Method | Path                                                 | Auth       |
| ------ | ---------------------------------------------------- | ---------- |
| GET    | `/healthz`                                           | —          |
| GET    | `/metrics`                                           | —          |
| GET    | `/api/v1/sdk-generation-jobs`                        | bearer JWT |
| POST   | `/api/v1/sdk-generation-jobs`                        | bearer JWT |
| GET    | `/api/v1/sdk-generation-jobs/{id}`                   | bearer JWT |
| GET    | `/api/v1/sdk-generation-jobs/{id}/publications`      | bearer JWT |
| POST   | `/api/v1/sdk-generation-jobs/{id}/publications`      | bearer JWT |
| POST   | `/api/v1/sdk/generate`                               | bearer JWT |

## SDK generation (POC, 2 services wired)

`POST /api/v1/sdk/generate` shells out to `tools/of-sdk-gen`, which in
turn drives the per-language generators below. The response is a zip
of the produced client tree.

Request body:

```json
{ "service": "audit-compliance-service", "language": "ts" }
```

Supported `service` values today (POC):

- `audit-compliance-service`
- `notification-alerting-service`

Each service ships its OpenAPI input at `internal/openapi/openapi.yaml`.
That YAML is the canonical input — it is hand-authored against the
chi router wiring (see each service's `internal/server/server.go`).

### Required CI / host tooling

The generators run as subprocesses; they are not Go dependencies and
are intentionally absent from `go.mod`. The host (developer laptop or
CI runner) must provide:

| Tool                          | Install                                            | Verify                                              |
| ----------------------------- | -------------------------------------------------- | --------------------------------------------------- |
| `npx` (Node 18+)              | `brew install node` / `apt install nodejs npm`     | `npx --yes openapi-typescript-codegen --version`    |
| `openapi-python-client`       | `pip install openapi-python-client`                | `openapi-python-client --version`                   |

CI must install both before running `go test -tags=integration
./services/sdk-generation-service/...`. Each integration test
auto-skips with a clear message when its tool is missing, so the unit
test suite stays green on bare runners.

When run inside the service container, set `OF_REPO_ROOT` so the
generator can locate `services/<svc>/internal/openapi/openapi.yaml`.

## Schema

Two tables under the configured Postgres database, applied via
embedded migrations at startup (idempotent `CREATE TABLE IF NOT EXISTS`):

- `sdk_generation_jobs` — `(id, payload jsonb, created_at, updated_at)`
- `sdk_generation_publications` — `(id, parent_id → jobs, payload jsonb, created_at)`

## Configuration

| Variable                       | Required | Purpose                              |
| ------------------------------ | :------: | ------------------------------------ |
| `DATABASE_URL`                 | ✅       | Postgres connection string           |
| `JWT_SECRET` (or `OPENFOUNDRY_JWT_SECRET`) | ✅ | HS256 secret                |
| `HOST` / `PORT`                |          | default `0.0.0.0:50144`              |
| `METRICS_ADDR`                 |          | default `0.0.0.0:9090`               |
| `OTEL_TRACES_EXPORTER=none`    |          | disable tracing                      |

## Build / run

```sh
make build-services
DATABASE_URL=postgres://localhost/sdkgen JWT_SECRET=$(openssl rand -hex 32) \
OTEL_TRACES_EXPORTER=none ./bin/sdk-generation-service
```
