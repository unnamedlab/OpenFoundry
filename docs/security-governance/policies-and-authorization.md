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
