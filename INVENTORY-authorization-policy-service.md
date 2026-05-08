# Inventory — authorization-policy-service

> **2026-05-07 update:** this file is historical for the Cedar strategy
> decision. The Go port has moved beyond the original foundation plan: the
> runnable service now wires Cedar policies, ABAC policies/evaluation,
> RBAC roles/groups/permissions, governance/project constraints,
> checkpoints/purpose records, cipher catalogs, and network-boundary resources.
> The current stub audit found no productive placeholder handlers in
> `authorization-policy-service`; see `STUB-AUDIT.md` for the active status.


## DECISION (2026-05-06): Option A — adopt `github.com/cedar-policy/cedar-go`

User signed off on **Option A** (cedar-go) over the sidecar (B) and the
"wait" stance (C). Rationale captured in conversation:

- The Rust binary `src/main.rs` is `fn main() {}` — there is no live
  production system to preserve byte-identical evaluation with. The
  conformance contract becomes "Cedar spec ↔ Go impl" (AWS's problem),
  not "Rust impl ↔ Go impl" (ours). This collapses the argument for the
  sidecar.
- AWS maintains cedar-rust and cedar-go in lock-step with the same
  conformance test suite; pre-1.0 risk is bounded (API drift, not
  unsoundness) and can be managed by pinning a tag and mirroring AWS's
  conformance tests in CI.

### De-risking step before the full service port

This de-risking step has landed: `libs/authz-cedar-go` exists and the Go
`authorization-policy-service` now mounts the sliced handler/domain surface.
Keep extending conformance coverage as Cedar usage expands.

### Open question (non-blocking)

The Rust binary's `fn main() {}` remains historical source material. The Go
port is the canonical implementation unless the Rust binary is deliberately
revived in a later architecture decision.

---

## Background — why this was a STOP-and-ask gate

This service was flagged in the migration prompt as a **STOP-and-ask
candidate** for two independent reasons:

1. **Cedar dependency.** Service depends on the `authz-cedar` workspace
   lib (1671 LOC of Rust glue around `cedar-policy = "4"`). Cedar is a
   policy engine with formal semantics; reimplementing it in Go from
   scratch is a multi-month effort. The viable paths require a project
   decision.
2. **Rust binary is currently a stub.** `src/main.rs` is literally:

   ```rust
   fn main() {}
   ```

   with S8/B14 sub-modules included as dead-code modules. The Go port is now
   the live authorization-policy-service surface for Cedar/ABAC, governance,
   checkpoint/purpose, cipher, network-boundary, and top-level RBAC.

Historical note: this was the highest-risk port in Phase 3 before the Go
Cedar strategy landed. `libs/authz-cedar-go` and the Go service are now present;
keep this inventory updated against the Go implementation.

## Service shape

Total Rust source: **~5 203 LOC** across the consolidated crate.

| Subsystem (S8/B14 absorbed) | LOC ~ | Purpose                          |
|------------------------------|-------|----------------------------------|
| Top-level (handlers/, domain/, models/) | 1 800 | Roles, groups, policies, permissions, restricted views, ABAC evaluator |
| `security_governance/`        | 800   | Slice 6 — security posture, governance reviews |
| `checkpoints_purpose/`        | 700   | Slice 6 — purpose-of-use checkpoints |
| `cipher/`                     | 800   | Slice 6 — envelope encryption keys, key catalogs |
| `network_boundary/`           | 600   | Slice 6 — egress allowlists, residency boundaries |
| `audit_wiring.rs`             | 100   | Cross-cutting audit emission |

The four sub-modules (security_governance, checkpoints_purpose, cipher,
network_boundary) were absorbed from former standalone crates per
ADR-0030 (B14). They share the consolidated binary but ship independent
config + handlers + models — porting them as discrete slices is feasible.

### Handler surface (top-level)

| File                              | LOC | Surface area                             |
|-----------------------------------|-----|------------------------------------------|
| `handlers/role_mgmt.rs`           | 276 | Role CRUD, role/permission grants        |
| `handlers/permission_mgmt.rs`     |  69 | Permission catalog                        |
| `handlers/group_mgmt.rs`          | 286 | Group CRUD, membership, group→role grants |
| `handlers/policy_mgmt.rs`         | 183 | Cedar policy CRUD + reload                |
| `handlers/restricted_views.rs`    | 226 | CBAC restricted-view CRUD, consolidated in identity-federation Go slice-7a |
| `domain/rbac.rs`                  |  97 | RBAC evaluator                           |
| `domain/abac.rs`                  | 383 | ABAC + Cedar evaluator (depends on authz-cedar) |
| `domain/access.rs`                |  55 | Top-level access decision composer       |

### Migrations

| File                                           | Tables                          |
|------------------------------------------------|---------------------------------|
| `20260427030100_checkpoints_purpose_foundation.sql` | purpose checkpoints + records |
| `20260427020100_security_governance_foundation.sql` | governance reviews + posture  |
| `20260427080100_network_boundary_foundation.sql`    | egress rules + boundaries     |
| `20260427050100_cipher_foundation.sql`              | key catalogs + envelope keys  |
| `openfoundry-go/services/authorization-policy-service/internal/repo/migrations/0007_rbac_policy_management.sql` | authorization-policy RBAC: roles, groups, permissions, grants |
| Identity-federation slice-6 migrations | identity-local users, login/admin RBAC, API keys, SCIM groups |

## Cedar-go viability research

### Option A — adopt `github.com/cedar-policy/cedar-go` (recommended IF maturity acceptable)

cedar-go is the **official AWS-maintained Go port** of the Cedar policy
engine, mirroring the Rust `cedar-policy` crate's surface:

| Rust API surface (used by authz-cedar engine.rs)        | Go counterpart                              | Status |
|----------------------------------------------------------|---------------------------------------------|--------|
| `cedar_policy::PolicySet` + `is_authorized(req, ents)`  | `cedar.PolicySet` + `cedar.IsAuthorized(...)` | ✅     |
| `Request::new(principal, action, resource, ctx, schema)` | `cedar.NewRequest(...)`                     | ✅     |
| `Decision::Allow / Decision::Deny`                       | `cedar.Allow / cedar.Deny`                  | ✅     |
| `Response.diagnostics().reason()` (matched policy ids)   | `Response.Diagnostics().Reasons`            | ✅     |
| `Response.diagnostics().errors()`                        | `Response.Diagnostics().ErrorList`          | ✅     |
| `Entities` (entity store)                                | `cedar.EntityStore`                         | ✅     |
| Schema validation                                        | `cedar.NewSchema(...)`                      | ✅     |
| Policy text → AST parsing                                | `cedar.NewPolicySet(...)`                   | ✅     |

Risks:
1. cedar-go is **pre-1.0** as of writing. API may drift; pin a specific
   tag and review the changelog before bumping.
2. Cedar policy semantics are formally specified — both Rust and Go
   ports MUST agree on `Decision` for any policy/request pair. AWS
   maintains a conformance test suite that cedar-go runs in CI; we
   should mirror those tests in `libs/authz-cedar-go` as a regression
   guard.
3. cedar-go is sync-only (no async). Fine; Go's net/http handlers are
   synchronous anyway. Audit emission can be moved off-path with a
   buffered chan (matching the Rust `tokio::spawn` pattern).

### Option B — Cedar sidecar over gRPC

Run a small Rust binary embedding cedar-policy as a sidecar; expose a
gRPC `Authorize(req) → Decision` RPC. Go service calls it on the loopback.

Pros: zero risk on policy evaluation correctness — same code path as today.
Cons: extra deployment complexity (one more process per pod), latency
budget (sidecar IPC adds 0.2–1 ms per request — likely fine), ownership
overhead (we now maintain a second binary in two languages).

### Option C — Wait

Stay on Rust for authorization-policy-service indefinitely; let the
Phase 6 retire-Rust step revisit. This is the lowest-risk option but
contradicts the "1:1 Rust→Go migration" project goal.

### Recommendation

**Option A (cedar-go)** is the right path *if* the project is willing to
add a conformance-test commitment. Concretely:

- Port `libs/authz-cedar` to Go as `libs/authz-cedar-go` first (1 671 LOC,
  most of which is glue: PolicyStore, audit sink, Postgres reload, Kafka
  reload, NATS reload, iceberg-policies adapter, schedule policies).
- Mirror the upstream cedar-rust ↔ cedar-go conformance tests so any
  policy that authorizes Allow on Rust also authorizes Allow on Go.
- The Go service now includes the sliced authorization-policy surface:
  top-level RBAC/groups/policies, security_governance, checkpoints_purpose,
  cipher, and network_boundary. Keep extending conformance coverage as Cedar
  usage expands.

**Option B (sidecar)** is the right path *if* the team wants to ship Go
faster and is OK with a polyglot deployment. The Rust sidecar is a
50-LOC tonic gRPC server wrapping authz-cedar.

The sidecar option is no longer the active path; Cedar-go is the accepted path
for the Go port.

## Wire-format invariants

Locked/maintained by the Go implementation:
- Cedar policy IDs (UUIDs) preserved across the wire so audit logs match.
- `AuthzAuditEvent` JSON shape (timestamp, principal, action, resource,
  decision, tenant, policy_ids, diagnostics) must match the Rust struct
  byte-for-byte — currently emitted to Kafka via `audit-trail`.
- RBAC role + permission slugs. Ownership is split deliberately:
  identity-federation owns identity-local user/session/API-key/SCIM RBAC, while
  authorization-policy-service owns tenant-scoped authorization-policy roles,
  groups, permissions, memberships, user-role grants, and group→role grants.
- Cedar entity URID format: `Type::"value"` exactly as the Rust impl
  serializes.

## Current Go port status

1. **Lib** — `libs/authz-cedar-go` is present.
2. **Top-level RBAC** — roles, groups, permissions, membership, user-role grants,
   and group→role grants are implemented and mounted in
   `services/authorization-policy-service`.
3. **Cedar policies** — policy CRUD + reload is implemented.
4. **ABAC evaluator** — policy evaluation is implemented against the Cedar/ABAC
   domain.
5. **security_governance** routes are implemented and mounted.
6. **checkpoints_purpose** routes are implemented and mounted.
7. **cipher** routes are implemented and mounted.
8. **network_boundary** routes are implemented and mounted.
9. **HTTP wire-up** is complete in the Go router; the Rust `fn main() {}` stub is
   retained only as historical source material.
10. **Restricted views** CRUD is consolidated in identity-federation slice-7a;
    authorization-policy-service keeps read-time evaluation parity through
    `POST /api/v1/policy-evaluations`, which reads enabled `restricted_views`
    rows and applies row filters, hidden columns, allowed org IDs, markings,
    guest access, and consumer-mode flags.

## Decisions for human review

1. **Conformance commitment**: Are we OK adopting AWS's Cedar conformance test
   suite as a CI gate beyond the current `libs/authz-cedar-go` coverage?
2. **Rust stub lifecycle**: Is the Rust crate intentionally retained as reference
   code, or should it be deleted once Go parity is accepted?
