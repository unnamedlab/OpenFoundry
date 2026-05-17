# Policies and authorization

> **Sensitive admin surface.** Policy and role administration is the layer
> that turns identity into authorization decisions. Read the
> [Security overview](./security-overview.md) for how this layer composes
> with the other six, and the
> [Shared responsibility model](./shared-responsibility-model.md) for which
> roles can configure what. Anything modeled on a Foundry concept must
> follow the [Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).

Authorization in OpenFoundry is broader than role checks alone.

## Repository signals

Authorization responsibilities are split between two services:

- `services/identity-federation-service` — user identity, role and permission administration; **issues** the principal that decisions are taken about.
- `services/authorization-policy-service` — the **decision point**. Cedar-backed engine that evaluates ABAC + RBAC + restricted-view policies. Lib bindings live in `libs/authz-cedar-go`.

The implementation entry points live in:

- `services/authorization-policy-service/cmd/authorization-policy-service/main.go` + `internal/server/` — chi router, policy CRUD and evaluation endpoints
- `services/authorization-policy-service/internal/handlers/` — policy management, permission management, role binding handlers
- `services/identity-federation-service/internal/handlers/` — RBAC role administration on the identity side
- `libs/auth-middleware` — claims extraction + scope checks invoked from every protected route

## Why this matters

Operational platforms usually need a layered model:

- role-based access for broad capability boundaries (RBAC)
- policy-based evaluation for fine-grained control (Cedar)
- attribute-aware decisions for sensitive data and object operations (ABAC)
- restricted views for row/column-level filtering

The current repo already contains the primitives for that model. The pattern for distributing policies to the data-plane in-process is documented in [Policy bundles in-process](./policy-bundles.md).

## Role sets, operations, and delegation rank (SG.7)

Roles are bundled into **role sets** scoped to a resource context.
Migration [`0008_sg7_role_sets_and_operations.sql`](../../services/authorization-policy-service/internal/repo/migrations/0008_sg7_role_sets_and_operations.sql)
seeds the four canonical contexts:

| Context | Seeded role set | Default roles (rank → name) |
|---|---|---|
| `project` | `project-default` | 1: discoverer · 2: viewer · 3: editor · 4: owner |
| `ontology` | `ontology-default` | 1: discoverer · 2: viewer · 3: editor · 4: owner |
| `restricted_view` | `restricted-view-default` | 1: viewer · 2: editor · 3: owner |
| `platform_admin` | `platform-admin-default` | 1: viewer · 2: admin |

Each default role is bound to a low-level **operation catalog** entry
(`resource:action`) seeded by the same migration — `project:discover`,
`project:read`, `project:edit`, `project:manage`, `project:build`,
`project:apply`, `project:share`, the parallel `ontology:*` and
`restricted_view:*` entries, and the `platform:read|manage|audit`
admin operations.

The admin surface lives on the authorization-policy-service:

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/role-sets[?context=…]` | List role sets (filtered). Returns each set with its ranked role members. |
| `POST/GET/PATCH/DELETE /api/v1/role-sets[/{id}]` | Role-set CRUD. `context` must be one of the four wire constants. |
| `POST /api/v1/role-sets/{id}/roles` | Add a role to the set with an explicit `rank`. Upsert on `(role_set_id, role_id)`. |
| `DELETE /api/v1/role-sets/{id}/roles/{role_id}` | Remove a role from the set. |
| `POST /api/v1/role-sets/{id}/delegation:check` | Returns `{allowed, grantor_rank, target_rank, reason}`. The check resolves the grantor's highest rank via direct `user_roles` and `group_roles → group_members`, then compares to the target role's rank. |
| `GET /api/v1/operations` | The seeded operation catalog. |

**Delegation rank invariant.** SG.7 requires that "role delegation
cannot exceed the grantor's own role level." The
[`Repo.CheckDelegation`](../../services/authorization-policy-service/internal/repo/role_sets.go)
helper resolves the grantor's highest rank inside a role set and
compares against the target role's rank. `AssignRole` enforces it
opt-in via `POST /api/v1/users/{id}/roles?role_set_id=<uuid>` (admins
bypass per the gateway permission check); callers that don't supply
`role_set_id` keep the legacy "permission gate alone" behaviour for
backwards compatibility. The
[`/control-panel/role-sets`](../../apps/web/src/routes/control-panel/RoleSetsPage.tsx)
admin UI exposes a "Delegation check" tool that hits the same
endpoint without performing the grant.

## Marking categories, markings, and permission checks (SG.11-SG.15)

Marking administration is currently implemented in
[`authorization-policy-service`](../../services/authorization-policy-service/).
Migration
[`0009_sg11_marking_categories.sql`](../../services/authorization-policy-service/internal/repo/migrations/0009_sg11_marking_categories.sql)
adds category metadata, visibility, category permissions, and category
audit events. Migration
[`0010_sg12_markings.sql`](../../services/authorization-policy-service/internal/repo/migrations/0010_sg12_markings.sql)
adds stable markings inside those categories, per-marking permissions,
and marking audit events. Migration
[`0011_sg13_marking_permission_model.sql`](../../services/authorization-policy-service/internal/repo/migrations/0011_sg13_marking_permission_model.sql)
adds direct resource marking rows and audited apply/remove attempts.
Migration
[`0012_sg14_marking_enforcement_inheritance.sql`](../../services/authorization-policy-service/internal/repo/migrations/0012_sg14_marking_enforcement_inheritance.sql)
adds inheritance edges for project/folder hierarchy and lineage-derived
data dependencies. Migration
[`0013_sg15_marking_aware_build_outputs.sql`](../../services/authorization-policy-service/internal/repo/migrations/0013_sg15_marking_aware_build_outputs.sql)
adds build/transaction output diff events.

| Table | Purpose |
|---|---|
| `marking_categories` | Tenant-scoped category slug/display name/description, `visible` or `hidden` visibility, optional organization restriction, metadata, creator, and timestamps. |
| `marking_category_permissions` | Category Administrator / Category Viewer grants to users or groups. |
| `marking_category_audit_events` | Audit rows for category creation, updates, permission grants/revocations, and blocked deletion attempts. |
| `markings` | Stable marking ID, category, slug/display name/description, metadata, creator, and timestamps. `category_id` is immutable. |
| `marking_permissions` | Marking `administrator`, `remover`, `applier`, and `member` grants to users or groups. |
| `marking_audit_events` | Audit rows for marking creation, updates, permission grants/revocations, blocked deletion, and blocked category moves. |
| `resource_markings` | Direct markings applied to resources, with resource kind/id, marking id, metadata, actor, and timestamp. |
| `resource_marking_audit_events` | Audit rows for resource marking application/removal plus denied attempts. |
| `resource_marking_edges` | Resource-to-resource inheritance edges. `hierarchy` edges propagate access requirements; `lineage` edges propagate data requirements. |
| `resource_marking_build_events` | Transaction/build diff records for output publication, including added/removed/unchanged markings and blocked declassification attempts. |

The API surface is:

| Endpoint | Purpose |
|---|---|
| `GET/POST/PATCH/DELETE /api/v1/marking-categories[/{id}]` | Category list/create/read/update plus audited delete-block response. |
| `PUT/DELETE /api/v1/marking-categories/{id}/permissions[...]` | Grant/revoke Category Administrator / Category Viewer. |
| `GET /api/v1/marking-categories/{id}/audit-events` | Category audit history. |
| `GET/POST /api/v1/marking-categories/{id}/markings` | List or create markings inside a category. Creation accepts an optional stable `id`. |
| `GET/PATCH/DELETE /api/v1/markings/{id}` | Read/update a marking plus audited delete-block response. |
| `PUT /api/v1/markings/{id}/category` | Always returns `405` and audits `marking.category_move_blocked`; markings cannot move categories. |
| `PUT/DELETE /api/v1/markings/{id}/permissions[...]` | Grant/revoke marking `administrator`, `remover`, `applier`, or `member`. |
| `GET /api/v1/markings/{id}/audit-events` | Marking audit history. |
| `POST /api/v1/markings/{id}/permission-check` | Explain whether a principal can manage, apply, remove, or access data protected by a marking. |
| `GET /api/v1/resource-markings` | List direct markings for a `resource_kind` and `resource_id`. |
| `POST /api/v1/resource-markings` | Apply a direct marking when the caller can apply the marking and has resource update-markings evidence. |
| `POST /api/v1/resource-markings/remove` | Remove a direct marking when the caller can remove it and has apply or equivalent expand-access evidence. |
| `GET /api/v1/resource-markings/effective` | Resolve direct and inherited markings for a resource, including source paths and provenance. |
| `GET/PUT/DELETE /api/v1/resource-marking-edges` | List, upsert, or delete hierarchy/lineage inheritance edges. |
| `POST /api/v1/resource-access:check` | Check organization, role, resource-marking, and lineage-derived data-marking requirements together. |
| `POST /api/v1/resource-marking-builds:publish` | Dry-run or apply a build/output marking publication by creating lineage edges and computing output marking diffs. |
| `GET /api/v1/resource-marking-build-events` | List build/transaction security diffs by build, transaction, or output resource. |

Discoverability mirrors the category visibility rules: visible
categories are listed to callers with `markings:read`; hidden
categories are listed only to marking writers/auditors, category
viewers/admins, or principals that hold a permission on any marking
inside the category. Marking metadata is redacted from ordinary readers
unless they are marking writers/auditors or hold category/marking
administrator rights.

The SG.13 permission check intentionally keeps marking administration
separate from data access. `administrator`, `applier`, `remover`, and
`member` are independent grants; only `member` satisfies access to data
protected by the marking. Applying a direct resource marking requires
`applier` plus proof that the caller can update markings on the target
resource. Removing a direct resource marking requires `remover`, that
same resource proof, and either `applier` or an equivalent expand-access
authorization. Both denied apply/remove attempts are written to
`resource_marking_audit_events`.

SG.14 turns those grants into enforcement facts. `resource_marking_edges`
point from an ancestor/upstream resource to the resource that inherits
the marking. `hierarchy` edges cover project/folder containment and
therefore contribute to `resource_access`; `lineage` edges cover data
dependencies and therefore contribute to `data_access`. The effective
marking resolver keeps every source path so a checker can show whether
a requirement came directly from the resource, from hierarchy, from
lineage, or from a mixed path. `POST /api/v1/resource-access:check`
combines this effective marking set with caller-supplied organization
and role evidence. Resource access requires organization, role, and all
direct/hierarchy markings; data access additionally requires all
lineage-derived markings.

SG.15 is the publish-time primitive that build/runtime services should
call before committing derived resources. The publish request is generic
over resource kinds, so datasets, media sets, code resources, ontology
object types, functions, and model artifacts can all be represented as
`{resource_kind, resource_id}` references. The endpoint writes lineage
edges from every input to every output, computes before/after effective
markings, and stores a diff in `resource_marking_build_events`.
Replacing existing output lineage can remove inherited markings; when
that happens, the publication is rejected unless the actor satisfies the
same removal rule used for direct resource markings.

The Control Panel surface lives at
[`/control-panel/marking-categories`](../../apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx)
and supports category creation, marking creation, permission grants,
revocation, audit inspection, delete-block probes, category-move block
probes, permission checks, and direct resource marking apply/remove
probes, inheritance edge management, effective marking provenance,
resource/data access checks, and build output marking diffs.
