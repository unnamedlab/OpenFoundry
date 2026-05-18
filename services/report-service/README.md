# report-service

## LLM quick context (current code)

Placeholder backend for report routes routed by edge-gateway.

Agent note: currently returns structured 501 for report product endpoints until implementation lands.

Current surface:
- `ANY /api/v1/reports* (501 placeholder)`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- No SQL migration files live under this service directory.
- Main internal packages: `config`, `handlers`, `server`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `CONFIG_FILE`

Keep this section in sync when changing routes, config, or persistence behavior.

Stub binary that backs the `/api/v1/reports*` routes the edge gateway
has been pointing at via `u.Report` (see
`services/edge-gateway-service/internal/proxy/router_table.go`). Until
the real implementation lands every request returns a structured 501:

```json
{
  "code": "not_implemented",
  "service": "report-service",
  "milestone": "S8.6"
}
```

The frontend's `apps/web/src/lib/api/reports.ts` calls (`/reports/overview`,
`/reports/catalog`, `/reports/definitions`, `/reports/schedules`,
`/reports/executions/*`) all land on this binary via the gateway.

## Exposed surfaces

- `GET  /healthz`               — liveness payload
- `GET  /metrics`               — Prometheus scrape endpoint
- `ANY  /api/v1/reports[/*]`    — 501 placeholder (auth required)

## Build

```sh
go build -o bin/report-service ./services/report-service/cmd/report-service
```
