# `notebook-runtime-service` (Go)

Notebook + notepad runtime: notebooks, cells, sessions, kernel
execute, workspace files, notepad documents + presence + export.

## Port status

| Component | Status |
|---|---|
| Health (`/health`, `/healthz`) + Prometheus (`/metrics`) | ✅ |
| URL grid (every Rust route mounted under `/api/v1`) | ✅ |
| `internal/domain/notepad` (HTML rendering for export, markdown subset, slug, presence cleanup SQL) | ✅ ported 1:1 with unit tests |
| `internal/domain/environment` (workspace seed + path normalisation + file CRUD on disk) | ✅ ported 1:1 with security-relevant traversal tests |
| Notepad export endpoint (consumes `domain/notepad`) | ✅ wired |
| Notebook / cell / session CRUD | ✅ wired against pgx when `DATABASE_URL` is available; falls back to empty envelopes for smoke/test mode without a DB |
| Notepad document + presence CRUD | 🟡 productive stub — list/get/delete return empty envelopes and create/update/upsert return 501 until a repository slice lands |
| Cell execute | 🟡 Python path wired through `PYTHON_SIDECAR_BINARY`; without that config it returns an explicit `python kernel sidecar is not configured` error. SQL/R/LLM kernels return kernel-not-supported errors until sidecars/adapters exist. |
| Notebook / cell / session CRUD | 🟡 partially backed by pgx with no-DB smoke fallbacks |
| Notepad document CRUD + presence | ✅ repository-backed via Postgres with in-memory no-DB fallback |
| Cell execute (Python / SQL / R / LLM kernels) | ✅ wired. Python uses `python-sidecar`; SQL posts to query-service `/api/v1/queries/execute`; R shells out to `Rscript`; LLM posts to ai-service `/api/v1/ai/chat/completions` and tracks conversation IDs per session. |

## Build & run

```sh
go build -o bin/notebook-runtime-service ./services/notebook-runtime-service/cmd/notebook-runtime-service
go test ./services/notebook-runtime-service/...
```

## Configuration

| Variable | Default |
|---|---|
| `HOST` | `0.0.0.0` |
| `PORT` | `50134` |
| `JWT_SECRET` | (required) |
| `DATABASE_URL` | unset (notebook/cell/session CRUD falls back to empty envelopes without it) |
| `DATA_DIR` | `/tmp/notebook-data` (workspace files live under `<data_dir>/workspaces/<notebook_id>/`) |
| `QUERY_SERVICE_URL` | `http://127.0.0.1:50133` |
| `AI_SERVICE_URL` | `http://127.0.0.1:50127` |
| `PYTHON_SIDECAR_BINARY` | unset (Python cells return a configured error until set) |
| `PYTHON_SIDECAR_TIMEOUT_SECONDS` | `60` |
