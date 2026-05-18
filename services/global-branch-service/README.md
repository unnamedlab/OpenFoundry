# global-branch-service

## LLM quick context (current code)

Coordinates global branches and participants across services.

Agent note: legacy code-repo branch paths are not the source of truth; use /api/v1/global-branches.

Current surface:
- `/api/v1/global-branches*`
- `/api/v1/global-branches/{id}/participants*`
- `/api/v1/global-branches/{id}/merge|abandon`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `1` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `config`, `domain`, `handler`, `models`, `repo`, `server`.
- Local service files present: `config.yaml`, `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `CONFIG_FILE`

Keep this section in sync when changing routes, config, or persistence behavior.

Cross-application Global Branching surface (parity target tracked in
[`docs/migration/foundry-global-branching-1to1-checklist.md`](../../docs/migration/foundry-global-branching-1to1-checklist.md)).

**Status: Milestone A.** The service now hosts lifecycle CRUD for
global branches and the per-service participation coordinator. A
global branch is the cross-application unit (Ontology, Datasets,
Workshop, Pipelines) that ties together a set of local branches; each
service enrolment is a `Participation` row keyed by `(global_branch_id, service_name)`.

ADR-0030 originally merged this surface into `code-repository-review-service`.
That service still owns the legacy `/api/v1/global-branches/*` shape
the frontend (`apps/web/src/lib/api/global-branches.ts`) calls today.
The gateway routing switch to this binary is tracked separately —
see [`internal/README.md`](internal/README.md).

## Endpoints

All product routes are tenant-scoped (`claims.org_id` is the implicit
`tenant_id` filter). Anonymous requests are 401; authenticated calls
without an `org_id` claim are 403.

| Route | Description |
|---|---|
| `GET /healthz` | Liveness payload |
| `GET /metrics` | Prometheus scrape |
| `GET /_meta/capabilities` | Capability catalog |
| `POST /api/v1/global-branches` | Create a global branch (status=`open`) |
| `GET /api/v1/global-branches` | List branches (optional `?status=` filter) |
| `GET /api/v1/global-branches/{id}` | Fetch a single branch |
| `PATCH /api/v1/global-branches/{id}` | Update branch metadata (`name`, `description`) |
| `POST /api/v1/global-branches/{id}/abandon` | Move open branch to terminal `abandoned` |
| `POST /api/v1/global-branches/{id}/merge` | Coordinator: flips every non-conflict participation to `merged` and stamps the branch |
| `POST /api/v1/global-branches/{id}/participants` | Register a service participation |
| `DELETE /api/v1/global-branches/{id}/participants/{service}` | Remove a participation |
| `GET /api/v1/global-branches/{id}/participants` | List participations on a branch |

The merge endpoint refuses to proceed when any participation row is in
`conflict` state (returns 409 with `ErrCannotMergeWithConflicts`).
Adding a participation to a branch that is already `merged` or
`abandoned` returns 409 with `ErrBranchClosed`.

## Audit emissions

Each mutating endpoint enqueues an `audit.events.v1` envelope into the
local outbox inside the same `pgx.Tx` as the state write (ADR-0022).
The custom event kinds are:

- `global_branch.created`
- `global_branch.merged`
- `global_branch.abandoned`
- `global_branch.participation_added`
- `global_branch.participation_removed`

These kinds are emitted with the canonical `audittrail.AuditEvent`
wire shape; consumers should filter on the `kind` string. (The
`libs/audit-trail` constant catalog stays restricted to the variants
the audit-sink + Iceberg schema currently recognise.)

## Build

```sh
go build -o bin/global-branch-service ./services/global-branch-service/cmd/global-branch-service
go test -count=1 ./services/global-branch-service/...
go test -count=1 -tags=integration ./services/global-branch-service/...
```

The integration test in `internal/repo/repo_integration_test.go`
spins up a postgres:16-alpine container via `libs/testing.BootPostgres`
and exercises the full create → add-participation → merge flow plus
tenant isolation and the duplicate-name conflict path.

## Configuration

Standard koanf precedence: `config.yaml` defaults → `CONFIG_FILE`
override → `OF_*` env vars.

| Key | Purpose | Required |
|---|---|---|
| `OF_DATABASE_URL` / `DATABASE_URL` | Postgres DSN for the product repository | Yes for normal boot |
| `OF_ENVIRONMENT` | Runtime environment; `production`/`prod` enables fail-closed production policy | Yes in production |
| `OF_ALLOW_UNWIRED_PRODUCT_ROUTES` | Explicit non-production smoke mode without DB-backed product routes | No (defaults to `false`) |
| `OF_JWT__SECRET` | JWT HS256 secret | Yes |
| `OF_SERVER__ADDR` | Listen address | No (defaults to `:8080`) |

The service fails closed when no DB DSN is configured. The only
exception is explicit non-production smoke mode
(`OF_ALLOW_UNWIRED_PRODUCT_ROUTES=true` while `OF_ENVIRONMENT` is not
`production`/`prod`), which leaves `/healthz` and the capability
catalog usable while omitting product routes from both the router and
the capability snapshot.
