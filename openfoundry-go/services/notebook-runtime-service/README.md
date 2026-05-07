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
| Notebook / cell / session / notepad CRUD | 🟡 stubbed — empty envelope or 501. Needs sqlc against the existing migrations |
| Cell execute (Python / SQL / R / LLM kernels) | ❌ not ported. The Rust crate uses PyO3, R-script subprocess, and reqwest into AI / query services. The Go port would need an out-of-process kernel manager (separate task). |

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
| `DATABASE_URL` | unset (CRUD remains stubbed without it) |
| `DATA_DIR` | `/tmp/notebook-data` (workspace files live under `<data_dir>/workspaces/<notebook_id>/`) |
| `QUERY_SERVICE_URL` | `http://127.0.0.1:50133` |
| `AI_SERVICE_URL` | `http://127.0.0.1:50127` |
