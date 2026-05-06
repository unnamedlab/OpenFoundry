# Nightly summary — Rust → Go autonomous run

**Date:** 2026-05-06
**Stop reason:** Hard architectural decision required — Cedar policy
engine strategy for `authorization-policy-service` and the cedar_authz
piece of `identity-federation-service` slice 8. See
[INVENTORY-authorization-policy-service.md](INVENTORY-authorization-policy-service.md).

## What landed

15 commits across the autonomous run, all on
`frontend/settings-mfa-apikeys-sso`, **never pushed to remote**.

| Iter | Commit    | Service / slice                                                  |
|------|-----------|------------------------------------------------------------------|
| 1    | d7daad3c  | Phase 2 — notification-alerting-service                           |
| 2    | 6165cbe8  | Phase 2 — sdk-generation-service                                  |
| 3    | 4a0e3087  | Phase 2 — telemetry-governance-service (CRUD baseline)            |
| 4    | c92e8866  | Phase 3 prep — identity-federation-service inventory              |
| 5    | 9a333f80  | Phase 3 / identity-federation slice 1 — register/login/token      |
| 6    | b29cd226  | Phase 3 / identity-federation slice 2 — cassandra-kernel scaffold |
| 7    | 0e141b83  | Phase 3 / identity-federation slice 3 — MFA TOTP                  |
| 8    | 8cebd686  | Phase 3 / identity-federation slice 4 — WebAuthn                  |
| 9    | ecbd5c65  | Phase 3 / identity-federation slice 5a — OIDC SSO                 |
| 10   | 5ab352a3  | Phase 3 / identity-federation slice 6 — RBAC CRUD                 |
| 11   | 073ae61c  | Phase 3 / identity-federation slice 7a — restricted views CBAC   |
| 12   | 3e22f6b3  | Phase 3 / tenancy-organizations slice 1 — organizations + enrollments |
| 13   | 13eba464  | Phase 2 follow-up — telemetry-governance streaming-monitors      |
| 14   | 81a1b7b0  | Phase 3 / tenancy-organizations slice 2 — workspace + favorites + recents |
| 15   | 1b259f38  | Phase 3 / tenancy-organizations slice 3 — sharing                 |

**Total LOC delta inside `openfoundry-go/`:** +12 599 / −28 across 118
files.

## Phase status

| Phase | Status |
|-------|--------|
| 0 — Foundations (scaffolding, libs/core-models, observability, auth-middleware, service template, CI) | ✅ done |
| 1 — Core libs (db-pool, event-bus-control, event-bus-data, audit-trail, idempotency, outbox, testing) | ✅ done |
| 1.5 — Tier-2 libs | partial — cassandra-kernel scaffold landed alongside identity-federation slice 2 |
| 2 — Stateless edge services | ✅ all 6 services migrated; streaming-monitor follow-up closed |
| 3 — Identity & authz | 🟡 in progress — see breakdown below |
| 4 — Data & ontology | pending |
| 5 — pyo3 sidecars | pending |
| 6 — ML/AI/apps & retire Rust | pending |

### Phase 3 breakdown

- **identity-federation-service** — slices 1, 2, 3, 4, 5a, 6, 7a ✅
  - 5b (SAML 2.0 + XML signing) — pending follow-up
  - 7b (control panel + ABAC + scoped sessions admin) — pending follow-up
  - 8 (Cedar + JWKS rotation + Vault + SCIM) — **STOP-and-ask** on Cedar
- **tenancy-organizations-service** — slices 1, 2, 3 ✅; full active
  surface complete. Spaces / projects / trash / resource_resolve are
  RETIRED upstream (verified via Rust `src/main.rs`) and deferred unless
  upstream re-introduces them.
- **authorization-policy-service** — INVENTORY written; **STOP-and-ask**.
  Rust binary is currently `fn main() {}` with all 5 203 LOC of handlers
  as dead-code library namespaces. See INVENTORY for Cedar A/B/C
  options.
- **audit-compliance-service** — pending; clean port (no flagged risks).

## Tests added

Every committed slice ships unit tests pinning the wire format. Notable:

- `libs/auth-middleware`: JWT + tenant context middleware tests.
- `libs/cassandra-kernel`: gocql cluster builder + migration ledger tests.
- `services/identity-federation-service/internal/{oidc,webauthn,rbac,...}`:
  per-slice tests covering register/login flows, MFA TOTP RFC 6238
  conformance, WebAuthn attestation/assertion, OIDC PKCE + nonce, RBAC
  permission wildcards, restricted-view CBAC.
- `services/telemetry-governance-service/internal/streamingmonitors`:
  enum SCREAMING_SNAKE_CASE pinning, comparator FP-tolerant EQ semantics,
  `{"data": [...]}` envelope.
- `services/tenancy-organizations-service/internal/{handlers,workspace}`:
  Organization/Enrollment JSON shape, `{"items": [...]}` envelope,
  ResourceKind legacy aliases (project → ontology_project), workspace
  `{"data": [...]}` envelope (different from org/enrollment), AccessLevel
  enum, share principal split (exactly one of user/group), share
  validation paths.

All tests run with `go test -race -count=1 ./...` after every iteration.

## Decisions deferred for human review

### 1. Cedar strategy (BLOCKING for authorization-policy-service + identity-federation slice 8)

Three options documented in INVENTORY-authorization-policy-service.md:

- **A. Adopt `github.com/cedar-policy/cedar-go`** (recommended). Pre-1.0,
  AWS-maintained, API mirror of cedar-policy v4. Requires committing to
  AWS's conformance test suite as a CI gate.
- **B. Cedar sidecar over gRPC.** Run a small Rust binary embedding
  cedar-policy; Go service calls it on loopback. Zero policy-evaluation
  risk; adds polyglot deployment + sidecar latency budget.
- **C. Wait** — keep authorization-policy-service in Rust until Phase 6.

This decision gates ~5 200 LOC of porting work plus the cedar_authz
piece of identity-federation slice 8.

### 2. Workspace inventory finding — retired upstream surfaces

Audit of `services/tenancy-organizations-service/src/main.rs` confirmed
that spaces / projects / trash / tenant_resolution / resource_resolve /
resource_ops are all unmounted upstream ("Cross-bounded-context project
/ space / trash / resource-operation handlers are intentionally not
wired here anymore"). Scope was re-scoped: ~2 150 LOC of Rust handlers
won't be ported unless upstream re-introduces them. Worth confirming
this matches the project's strategic intent.

### 3. Iceberg writer for audit-sink + ai-sink (Phase 2 follow-up, non-blocking)

Both sinks ship `JSONLWriter` (production-suitable) and an
`IcebergWriter` stub that fails loudly. iceberg-go's write API was
unstable when ports landed; revisit when iceberg-go ≥1.0 publishes
stable writes.

### 4. SAML 2.0 (identity-federation slice 5b, non-blocking)

XML signing infrastructure (crewjam/saml + russellhaering/goxmldsig in
Go) ports cleanly but needs IdP test certs + metadata fixtures to
validate end-to-end. Pending until dev infra ships SAML test rig.

### 5. Sessions Cassandra wiring (identity-federation slice 2b, non-blocking)

`libs/cassandra-kernel` and the `sessionscassandra` adapter are
scaffolded. The active backend remains Postgres; flipping the switch is
a one-line config change, gated on Scylla being in dev infrastructure.

### 6. Ontology actions (Phase 4, non-blocking)

`ontology-actions-service` uses pyo3 → Python sidecar pattern. Plan to
treat this as a polyglot service per the migration doc Phase 5 strategy.
Flag for explicit go/no-go before starting that port.

## Build warnings worth flagging

None. `go build ./... && go vet ./... && go test -race ./...` clean
across the workspace at HEAD (1b259f38).

## Resume protocol

When the human signs off on Cedar strategy:

1. Update todos: pick a Cedar option (A/B/C) and unblock either
   authorization-policy-service migration or the cedar_authz slice 8.
2. If Option A (cedar-go): port `libs/authz-cedar-go` first, mirror
   AWS's cedar conformance tests, only then port handlers/domain in
   slices.
3. If Option B (sidecar): write the 50-LOC tonic Rust sidecar + Go gRPC
   client; flip authorization-policy-service to call out.
4. If Option C (wait): mark the todo deferred to Phase 6 and continue
   with audit-compliance-service (the next non-Cedar Phase 3 service).

Other unblocked work that doesn't need Cedar:

- `audit-compliance-service` migration.
- Phase 4 services (data + ontology).
- identity-federation slice 5b (SAML XML signing) once dev infra exists.
- identity-federation slice 7b (control panel + ABAC) — note ABAC
  evaluator is the Cedar piece; control_panel pages + scoped sessions
  admin are independent.
