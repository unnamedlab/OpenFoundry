# global-branch-service — internals

This binary hosts the canonical Global Branching surface (Milestone A
of [`docs/migration/foundry-global-branching-1to1-checklist.md`](../../../docs/migration/foundry-global-branching-1to1-checklist.md)).

## Package layout

| Package | Role |
|---|---|
| `internal/domain/` | Pure types and lifecycle rules (no DB / no HTTP). Owns `GlobalBranch`, `Participation`, status enums, and the `Err*` sentinels. |
| `internal/repo/` | pgx-backed persistence + embedded migrations. Returns the domain sentinels so the HTTP layer can map them to status codes via `errors.Is`. |
| `internal/handler/` | chi handlers. Each mutating endpoint opens a `pgx.Tx`, performs the SQL write, calls `libs/audit-trail.EmitToOutbox`, and commits — so the state change and the audit event land atomically (ADR-0022). |
| `internal/models/` | HTTP wire types (`CreateBranchRequest`, `BranchResponse`, …). Kept separate from `domain` so the domain types stay JSON-tag-free. |
| `internal/server/` | Router wiring + lifecycle (graceful shutdown). |
| `internal/config/` | koanf-backed config. Mirrors `docs/templates/service-skeleton`. |

## Outstanding gateway routing (Milestone A scope-out)

The frontend's
[`apps/web/src/lib/api/global-branches.ts`](../../../apps/web/src/lib/api/global-branches.ts)
still calls the `/api/v1/global-branches/*` shape exposed by
`code-repository-review-service` — that's where ADR-0030 originally
merged the surface. **This binary intentionally does not register
itself in `services/edge-gateway-service/internal/proxy/router_table.go`
for those paths yet.** Doing so today would create a double-routing
race because both upstreams answer `/api/v1/global-branches/*`.

A follow-up PR will:

1. Retire the `code-repository-review-service` implementation of the
   global-branches endpoints (or fold its handlers into this binary).
2. Flip the gateway's `DefaultUpstreams.GlobalBranch` (or add an
   explicit route entry) to point at `global-branch-service:8080`.
3. Update the frontend client only if the wire shape diverges.

Until that lands, this binary is exercised via direct in-cluster
calls and the integration test in `internal/repo/`.

## Audit event kinds

Five custom kinds are emitted from the handler layer:

- `global_branch.created`
- `global_branch.merged`
- `global_branch.abandoned`
- `global_branch.participation_added`
- `global_branch.participation_removed`

They share the `audittrail.AuditEnvelope` wire format. Consumers
(audit-sink, audit-compliance) should filter on the `kind` string —
the constants are declared in `internal/handler/branch.go`, not in
`libs/audit-trail`, because the library's variant catalog stays
restricted to kinds the Iceberg schema currently recognises.

## Local outbox table

`internal/repo/migrations/0001_global_branches.sql` ships its own
copy of the canonical `outbox.events` table (mirrors the schema owned
by `libs/outbox`). Every mutating handler enqueues the audit envelope
into this table inside the same transaction as the state mutation;
Debezium captures the WAL and forwards it via EventRouter SMT to
Kafka. The table stays empty in steady state thanks to the
`Enqueue → Delete` pattern in `libs/outbox`.
