# network-boundary-service

## LLM quick context (current code)

Owns network-boundary placeholders and data-connection egress policy lifecycle/evaluation APIs.

Agent note: parts are scheduled for consolidation into authorization-policy-service; egress policies are implemented.

Current surface:
- `/api/v1/network-boundaries* (501 placeholder)`
- `/api/v1/network-boundary* (501 placeholder)`
- `/api/v1/data-connection/egress-policies*`
- `POST /api/v1/data-connection/egress-policies:evaluate-workload`
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

Backs the `/api/v1/network-boundaries`, `/api/v1/network-boundary` and
`/api/v1/data-connection/egress-policies` route prefixes that the edge
gateway fans out to `u.NetworkBoundary`. Without this binary those
paths return 502 to the frontend because no upstream is listening.

Per [ADR-0030](../../docs/architecture/adr/ADR-0030-service-consolidation-30-targets.md)
the boundary surface is slated to merge into `authorization-policy-service`
during milestone S8.6 / B14. Until then `/api/v1/network-boundaries` and
`/api/v1/network-boundary` remain 501 placeholders:

```http
HTTP/1.1 501 Not Implemented
Content-Type: application/json; charset=utf-8

{"code":"not_implemented","service":"network-boundary-service","milestone":"S8.6/B14"}
```

`/healthz`, `/metrics` and `/_meta/*` work normally so the pod passes
k8s probes and is visible to platform tooling.

The `/api/v1/data-connection/egress-policies` surface is implemented for
SG.34. It supports direct, agent-proxy, and same-region bucket policy
creation; pending/active/paused/revoked lifecycle state; immutable
destination semantics; high-risk importer grants; and workload runtime
evaluation through `POST /api/v1/data-connection/egress-policies:evaluate-workload`.

## Build

```sh
go build -o bin/network-boundary-service ./services/network-boundary-service/cmd/network-boundary-service
```

## Routes

Keep this list in sync with the `u.NetworkBoundary` branch in
[`services/edge-gateway-service/internal/proxy/router_table.go`](../edge-gateway-service/internal/proxy/router_table.go).
