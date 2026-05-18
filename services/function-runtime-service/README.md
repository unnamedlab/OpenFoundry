# function-runtime-service

User-authored function runtime (v0). Hosts the registry, versioning,
and execution path for OpenFoundry Functions. v0 ships TypeScript +
Python *stubs* that shell out to `node` / `python3`; the v1 design
swaps those for an in-process isolated runtime (deno / v8go for TS,
subinterpreters or wasm for Python). See
[internal/executor/README.md](./internal/executor/README.md) for the
trade-off log.

This service is the v0 cut of the
[Functions runtime migration checklist](../../docs/migration/foundry-functions-runtime-1to1-checklist.md).
`agent-runtime-service` covers LLM tooling; this binary covers
*user-authored* TS / Python functions invoked from Workshop, Slate,
Automate, Actions and AIP Logic.

## Layout

```
services/function-runtime-service/
  cmd/function-runtime-service/main.go     entrypoint (config → store → registry → server)
  config.yaml                              shipped defaults; override via OF_* env
  internal/server/                         chi router + auth + audit
  internal/handlers/                       /api/v1/functions/* HTTP layer
  internal/domain/                         sentinel errors
  internal/repo/                           Store interface + memory + pgx impls
  internal/repo/migrations/                goose-style SQL (function_definitions, function_versions, function_runs)
  internal/executor/                       Executor interface + TS/Python stubs (timeouts + rlimit hooks)
  internal/models/                         wire + persistence types
  internal/config/                         koanf-backed config (Config.Load)
```

## Run locally

```sh
export OF_JWT__SECRET=dev-not-secret
# Postgres-backed (recommended): set OF_DATABASE__URL.
# Without it the service uses an in-memory store; logged at startup.
go run ./services/function-runtime-service/cmd/function-runtime-service \
  -config ./services/function-runtime-service/config.yaml
```

## HTTP surface (all under `/api/v1/functions`, JWT-gated)

| Verb | Path | Purpose |
|------|------|---------|
| `POST`   | `/api/v1/functions`                       | Register a new FunctionDefinition (optionally with v1 source) |
| `GET`    | `/api/v1/functions`                       | List definitions (filter by `namespace`, `status`, `runtime`, `limit`) |
| `GET`    | `/api/v1/functions/{id}`                  | Get a definition |
| `POST`   | `/api/v1/functions/{id}/versions`         | Publish a new immutable version |
| `POST`   | `/api/v1/functions/{id}/activate?version=N` | Promote version N to the active pointer |
| `POST`   | `/api/v1/functions/{id}/deprecate`        | Mark the definition deprecated |
| `POST`   | `/api/v1/functions/{id}/invoke`           | Synchronous execution; body: `{version?, input, timeout_seconds?}` |
| `POST`   | `/api/v1/functions/{id}/invoke-async`     | Fire-and-forget execution; returns `202` with the queued run |
| `GET`    | `/api/v1/functions/runs/{run_id}`         | Look up a run |
| `GET`    | `/api/v1/functions/runs`                  | List runs (filter by `function_id`, `status`, `limit`) |

Every route lives behind `libs/auth-middleware`; the caller's tenant
is taken from the JWT (`claims.OrgID`), not from request body fields.

## Persistence

Three tables, scoped by tenant:

- `function_definitions` — identity row (namespace, name, runtime,
  signature, status, active_version pointer).
- `function_versions` — immutable per-version source pointers.
- `function_runs` — execution attempts with input, output, error,
  timing.

See [0001_function_runtime_foundation.sql](./internal/repo/migrations/0001_function_runtime_foundation.sql).
Migrations are applied at startup when `OF_DATABASE__URL` is set.

## Audit

The server mounts `audittrail.Middleware()` once per router. Every
request emits one `request handled` slog record tagged
`category=audit` that `audit-compliance-service` collects.

## Edge gateway registration — TODO for the gateway-routing task

Per the migration checklist and project policy, **this service does
not register itself** in the edge gateway. The next merge of the
gateway-routing task should add the following two prefixes to
`services/edge-gateway-service/internal/proxy/router_table.go`
(slot it next to the `agent-runtime` AI block to keep the
"AI / Functions" cluster contiguous):

```go
// ── functions ──
case strings.HasPrefix(path, "/api/v1/functions"):
    return u.FunctionRuntime
```

The matching upstream URL must be added to `internal/config/config.go`:

```go
// In config.UpstreamURLs:
FunctionRuntime string `koanf:"function_runtime"`
// In DefaultUpstreams():
FunctionRuntime: "http://function-runtime-service:50190",
```

(Port `50190` matches the value baked into [config.yaml](./config.yaml);
adjust if the deployment chart pins a different one.)

No Helm / ArgoCD wiring is included in this PR — those land alongside
the gateway change.

## Testing

```sh
go build  ./services/function-runtime-service/...
go vet    ./services/function-runtime-service/...
go test   -count=1 ./services/function-runtime-service/...
```

Live-runtime tests (TS round-trip, timeout) are skipped automatically
when `node` is not on `$PATH`, so the suite stays green on minimal
images.

## v1 follow-ups

Tracked in [internal/executor/README.md](./internal/executor/README.md):

- replace `node` / `python3` subprocess stubs with deno / v8go /
  subinterpreters / wasm;
- wire `code-repository-service` blob fetch for non-inline `source_uri`;
- enforce real RLIMIT_AS (today only a hint env on linux);
- per-run `tmpfs` mount for sandboxed filesystem;
- emit structured run events via `libs/audit-trail`'s `EmitToOutbox`
  inside the run transaction.
