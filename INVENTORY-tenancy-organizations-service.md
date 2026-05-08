# Inventory — tenancy-organizations-service

Snapshot of the Rust crate `services/tenancy-organizations-service` taken
during the Go port. Total Rust source: ~3 950 LOC across handlers + models
+ domain (excluding migrations).

## Active vs retired surfaces (verified 2026-05-06)

The current Rust binary (`src/main.rs`) only mounts a subset of the
historical handlers. The comment there is explicit:

> Cross-bounded-context project / space / trash / resource-operation
> handlers are intentionally not wired here anymore. Those flows must
> come back through upstream APIs and/or local read-models rather than
> direct database pools into `ontology` or `nexus`.

Live mount in Rust today: `/api/v1/workspace/*` only, via
`routes::workspace_router()`, covering favorites + recents + sharing.

| Rust file                          | LOC | Live in Rust? | Go slice    |
|------------------------------------|-----|---------------|-------------|
| `handlers/organizations.rs`        | 114 | ✅            | **1** ✅    |
| `handlers/enrollments.rs`          | 108 | ✅            | **1** ✅    |
| `handlers/favorites.rs`            | 157 | ✅            | **2** ✅    |
| `handlers/recents.rs`              | 131 | ✅            | **2** ✅    |
| `handlers/workspace.rs` (primitives)| 106 | ✅            | **2** ✅    |
| `handlers/sharing.rs`              | 326 | ✅            | **3** ✅    |
| `handlers/spaces.rs`               | 179 | ❌ retired    | deferred    |
| `handlers/projects.rs`             | 786 | ❌ retired    | deferred    |
| `handlers/trash.rs`                | 340 | ❌ retired    | deferred    |
| `handlers/tenant_resolution.rs`    |  30 | ❌ retired    | deferred    |
| `handlers/resource_resolve.rs`     | 169 | ❌ retired    | deferred    |
| `handlers/resource_ops.rs`         | 509 | ❌ retired    | deferred    |

The retired surfaces stay in the Rust crate as reference but are not part
of the current port plan — they'll be re-evaluated when upstream BCs
expose proper APIs (per the Rust `main.rs` comment above).

## Schema (Postgres)

| Migration (Rust filename)                          | Live? | Go migration                       |
|----------------------------------------------------|-------|------------------------------------|
| `20260427000100_tenancy_organizations_foundation.sql` | ✅ | `0001_tenancy_organizations_foundation.sql` |
| `20260501000300_user_favorites.sql`                | ✅    | `0002_user_favorites.sql`          |
| `20260501000400_resource_access_log.sql`           | ✅    | `0003_resource_access_log.sql`     |
| `20260501000500_resource_shares.sql`               | ✅    | `0004_resource_shares.sql`         |
| `20260423091500_nexus_foundation.sql`              | ❌ retired (nexus_*) | —             |
| `20260425223000_spaces_and_admin_lifecycle.sql`    | ❌ retired (spaces) | —              |

## Domain logic

- `domain/tenant_resolution.rs` (124 LOC) — retired; RID → org/space/project
  lookup tied to nexus_* tables.
- `domain/project_access.rs` (334 LOC) — retired; project access decisions.

Both stay as Rust reference until upstream APIs replace them.

## Model crate split

- `models/organization.rs` (36) — ✅ ported.
- `models/enrollment.rs` (32) — ✅ ported.
- favorites/recents — ✅ ported (in `internal/workspace/models.go`).
- `models/space.rs` (84) — retired.
- `models/project.rs` (131) — retired.
- `models/control_plane.rs` (42) — retired.
- `models/peer.rs` (104) — retired.

## Wire-format invariants (locked)

Different list envelopes by surface (must NOT be unified):

| Surface          | Envelope          | Reason                              |
|------------------|-------------------|-------------------------------------|
| Organizations / enrollments | `{"items": [...]}` | Foundation slice convention. |
| Workspace (favorites, recents, sharing) | `{"data": [...]}` | Rust impl predates the {items} convention. |

Other invariants:
- Snake-case JSON for every body field.
- IDs are RFC-4122 v4 UUIDs.
- Timestamps are ISO-8601 with timezone (UTC).
- Status enums: `active`, `disabled`, `archived` for organizations.
- ResourceKind values: `dataset`, `pipeline`, `notebook`, `app`, `dashboard`,
  `report`, `model`, `workflow`, `other`, `ontology_project`,
  `ontology_folder`, `ontology_resource_binding`. Legacy aliases `project`,
  `folder`, `resource_binding` map to the ontology_* canonical names.

These are pinned by `internal/handlers/handlers_test.go` and
`internal/workspace/handlers_test.go`.

## Sliced port plan (revised)

1. **Foundation** ✅ — organizations + enrollments CRUD.
2. **Workspace primitives + favorites + recents** ✅ — `ResourceKind` enum,
   `user_favorites` table, `resource_access_log` table, full CRUD.
3. **Sharing** — pending. `resource_shares` table + rule evaluation;
   POST `/resources/{kind}/{id}/share`, GET `/resources/{kind}/{id}/shares`,
   DELETE `/shares/{id}`, GET `/shared-with-me`, GET `/shared-by-me`.

Retired (no port planned): spaces, projects, trash, tenant_resolution,
resource_resolve, resource_ops, nexus_* tables. Re-port if upstream APIs
re-introduce them.

## Configuration parity

Same env names as the Rust crate:

- `DATABASE_URL` (required)
- `OPENFOUNDRY_JWT_SECRET` / `JWT_SECRET` (required)
- `HOST`, `PORT` (default `0.0.0.0:50113`)

Port `50113` matches the Rust foundation listener.
