# `notebook-runtime-service` (Go)

## LLM quick context (current code)

Owns notebooks, cells, sessions, notepad documents, gateway kernel proxying, query/AI helper calls, and notebook runtime state.

Agent note: can run against Jupyter Kernel Gateway and Python sidecar settings.

Current surface:
- `/api/v1/notebooks*`
- `/api/v1/notepad/documents*`
- `/api/v1/kernels* gateway proxy`
- `POST /api/v1/queries/execute`
- `POST /api/v1/ai/chat/completions`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `handler`, `kernel`, `kernelgw`, `models`, `repo`, `server`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `AI_SERVICE_URL`, `DATABASE_URL`, `DATA_DIR`, `HOST`, `JWT_SECRET`, `KERNEL_GATEWAY_AUTH_TOKEN`, `KERNEL_GATEWAY_HTTP_URL`, `KERNEL_GATEWAY_WS_URL`
- `KERNEL_GC_INTERVAL_SECONDS`, `KERNEL_IDLE_TIMEOUT_SECONDS`, `NOTEBOOK_RUNTIME_SMOKE_MODE`, `PORT`, `PYTHON_SIDECAR_BINARY`, `PYTHON_SIDECAR_TIMEOUT_SECONDS`, `QUERY_SERVICE_URL`, `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

Notebook + notepad runtime: notebooks, cells, sessions, kernel execution,
workspace files, notepad documents + presence + export.

## Port status

| Component | Status |
|---|---|
| Health (`/health`, `/healthz`) + Prometheus (`/metrics`) | ✅ wired |
| URL grid (every Rust route mounted under `/api/v1`) | ✅ wired |
| Notebook CRUD | ✅ pgx-backed in production; explicit `NOTEBOOK_RUNTIME_SMOKE_MODE=true` enables in-memory smoke CRUD without `DATABASE_URL` |
| Cell CRUD | ✅ pgx-backed in production; explicit smoke mode persists cells in memory for smoke tests |
| Session CRUD | ✅ pgx-backed in production; explicit smoke mode persists session lifecycle in memory |
| Workspace files | ✅ ported through `internal/domain/environment` with path-normalisation/traversal tests |
| Notepad document CRUD | ✅ repository-backed via Postgres with in-memory no-DB fallback |
| Notepad presence | ✅ repository-backed via Postgres with in-memory no-DB fallback |
| Notepad export | ✅ wired through `internal/domain/notepad` HTML rendering |
| Cell execute (Python) | ✅ wired through `libs/python-sidecar` when `PYTHON_SIDECAR_BINARY` is set; unset config returns an explicit `python kernel sidecar is not configured` execution error rather than a placeholder envelope |
| Cell execute (SQL / R / LLM) | ✅ adapters are wired: SQL posts to query-service, R shells out to `Rscript`, LLM posts to ai-service chat completions and tracks session conversation IDs |

## Production vs smoke matrix

| Mode | Required configuration | Notebook / cell / session CRUD | Python execution |
|---|---|---|---|
| Production | `DATABASE_URL` set, `NOTEBOOK_RUNTIME_SMOKE_MODE=false` (default) | Uses Postgres via pgx. If the DB pool cannot be created, CRUD responds `503` instead of silently synthesising resources. | Uses the managed gRPC sidecar only when `PYTHON_SIDECAR_BINARY` points at `openfoundry-pyruntime`; otherwise Python cell execution returns an explicit not-configured error payload. |
| Production misconfigured | `DATABASE_URL` unset, `NOTEBOOK_RUNTIME_SMOKE_MODE=false` | Stable `503` for notebook, cell, and session CRUD with `DATABASE_URL is required unless NOTEBOOK_RUNTIME_SMOKE_MODE=true`. | Same as production: sidecar is configured independently from the DB. |
| Smoke / tests | `DATABASE_URL` unset, `NOTEBOOK_RUNTIME_SMOKE_MODE=true` | Uses the in-memory repository for real create/list/get/update/delete round trips. Data is process-local and is lost on restart. | May use a real sidecar, a fake gRPC sidecar in tests, or return the explicit not-configured error when no Python sidecar is wired. |

## Smoke mode contract

`NOTEBOOK_RUNTIME_SMOKE_MODE=true` is the only documented mode where notebook,
cell, and session routes may operate without a database. In smoke mode, the Go
service uses an in-memory repository so CRUD routes return concrete resources
instead of empty envelopes. With `DATABASE_URL` unset and smoke mode disabled,
notebook/cell/session CRUD returns `503` with a clear database-required error.

Notepad document and presence routes keep their existing in-memory fallback when
no Postgres pool is available because the repository abstraction owns that
fallback and the route contract remains concrete.

## Python sidecar contract

- `PYTHON_SIDECAR_BINARY` unset: the service starts, but Python cell execution
  returns an explicit sidecar-not-configured error payload.
- `PYTHON_SIDECAR_BINARY` set: startup creates and health-checks a managed
  `openfoundry-pyruntime` subprocess via `libs/python-sidecar`.
- Tests cover both unset configuration and a fake sidecar binary that speaks the
  same gRPC/health contract used by the manager.

## Build & run

```sh
go build -o bin/notebook-runtime-service ./services/notebook-runtime-service/cmd/notebook-runtime-service
go test ./services/notebook-runtime-service/... ./libs/python-sidecar/...
```

## Configuration

| Variable | Default |
|---|---|
| `HOST` | `0.0.0.0` |
| `PORT` | `50134` |
| `JWT_SECRET` | (required for protected routes) |
| `DATABASE_URL` | unset; production CRUD returns `503` unless smoke mode is enabled |
| `NOTEBOOK_RUNTIME_SMOKE_MODE` | `false` |
| `DATA_DIR` | `/tmp/notebook-data` (workspace files live under `<data_dir>/workspaces/<notebook_id>/`) |
| `QUERY_SERVICE_URL` | `http://127.0.0.1:50133` |
| `AI_SERVICE_URL` | `http://127.0.0.1:50127` |
| `PYTHON_SIDECAR_BINARY` | unset |
| `PYTHON_SIDECAR_TIMEOUT_SECONDS` | `60` |
