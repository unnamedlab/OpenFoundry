# knowledge-index-service

## LLM quick context (current code)

Placeholder backend for AI knowledge-base management routes.

Agent note: search routes for knowledge bases are routed to retrieval-context-service by the gateway.

Current surface:
- `ANY /api/v1/ai/knowledge-bases* (501 placeholder)`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- No SQL migration files live under this service directory.
- Main internal packages: `config`, `handler`, `server`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `CONFIG_FILE`

Keep this section in sync when changing routes, config, or persistence behavior.

Stub binary that backs the `/api/v1/ai/knowledge-bases*` routes the
edge gateway has been pointing at via `u.KnowledgeIndex` (see
`services/edge-gateway-service/internal/proxy/router_table.go`). Until
the real implementation lands every request returns a structured 501:

```json
{
  "code": "not_implemented",
  "service": "knowledge-index-service",
  "milestone": "S8.6"
}
```

`/api/v1/ai/knowledge-bases/.../search` is routed to
`retrieval-context-service` by the gateway and never reaches this
binary.

## Exposed surfaces

- `GET  /healthz`               — liveness payload
- `GET  /metrics`               — Prometheus scrape endpoint
- `ANY  /api/v1/ai/knowledge-bases[/*]` — 501 placeholder (auth required)

## Build

```sh
go build -o bin/knowledge-index-service ./services/knowledge-index-service/cmd/knowledge-index-service
```
