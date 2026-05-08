# Migration plan: Rust → Go (openfoundry-go)

This document is the operating model for the multi-quarter Rust-to-Go
re-implementation of OpenFoundry. The Go workspace lives at the
sibling directory [`openfoundry-go/`](../../openfoundry-go/). The Rust
workspace at the repo root remains the production source of truth
until each service flips over.

> Goal: **functional 1:1 paridad** (same proto, OpenAPI, SQL schema,
> Kafka topics, JWT shape, /healthz payload). This is *not* a literal
> 1:1 port — see "What does NOT migrate literally" below.

---

## Phases

| Phase | Scope | Status |
|-------|-------|--------|
| 0 | Foundations: scaffolding, libs/core-models, observability, auth-middleware, service template, CI | ✅ done |
| 1 | Core libs (db-pool, event-bus-control, event-bus-data, audit-trail, idempotency, outbox, testing) | ✅ done |
| 1.5 | Tier-2 libs and kernels | ✅ package-root ports reconciled — `openfoundry-go/INVENTORY-PHASE6.md` now lists every Rust `libs/*/Cargo.toml` against `openfoundry-go/libs/*`. Config-gated backend work remains for live Cassandra/Kafka/NATS/search/vector consumers; `query-engine` is an explicit DataFusion exception. |
| 2 | Stateless edge services: edge-gateway, notification-alerting, sdk-generation, telemetry-governance, audit-sink, ai-sink | ✅ done — all six service roots exist in Go. Audit/AI sink Iceberg writers remain config-gated production adapters, not missing ports. |
| 3 | Identity & authz: identity-federation, authorization-policy, tenancy-organizations, audit-compliance | ✅ package-root ports reconciled — identity-federation, authorization-policy, tenancy-organizations, and audit-compliance all have Go service roots. Remaining work is adapter/runtime hardening rather than an absent service port. |
| 4 | Data & ontology: dataset-versioning, media-sets, iceberg-catalog, ontology-{definition,query,actions,indexer}, connector-management, ingestion-replication, object-database | ✅ package-root ports reconciled — every listed service has a Go service root. Dataset/backing filesystem, ontology query/indexer, and sidecar-backed actions paths are classified as config-gated where they need live dependencies. |
| 5 | pyo3-bound services as Python sidecars: notebook-runtime, pipeline-build, ontology-actions | ✅ sidecar pattern accepted and Go service roots exist. These services are **ported but config-gated** because production execution depends on the configured Python sidecar and related runtime adapters. |
| 6 | ML/AI/apps & retire Rust | ✅ package-root ports reconciled — the former architecture+slice list (`code-repository-review`, `media-transform-runtime`, `entity-resolution`, `ontology-exploratory-analysis`, `reindex-coordinator`, `lineage`, `workflow-automation`, `federation-product-exchange`) now has Go service roots. Remaining work is integration/configuration plus explicit exclusions, not absent-service backlog. |

`sql-bi-gateway-service` and `query-engine` are excepted: they keep
DataFusion/Arrow push-down execution in Rust because Go has no equivalent
query engine. Any Go directories around those names are shims/support code,
not a claim that the DataFusion engine was literally ported.

---

## What does NOT migrate literally

Five places where the Go re-implementation deliberately diverges from
the Rust shape:

1. **`pyo3` services** → Python sidecars over gRPC on loopback.
   The Go service owns lifecycle (subprocess) and the protocol; these
   package roots are classified as ported but config-gated until the
   sidecar binary/configuration is present in the target deployment.
2. **`datafusion` push-down** → kept in Rust (`sql-bi-gateway-service`,
   `query-engine`).
3. **`authz-cedar` Cargo features** → split into separate Go packages
   (`cedar/postgres`, `cedar/nats`, …). No `#[cfg(feature)]` equivalent.
4. **`sqlx::query!` compile-time check** → `sqlc` in CI (same level of
   safety, different moment in the build).
5. **Async traits with generics** → flat interfaces per entity. Do not
   port the `Repository<T: Entity>` hierarchy literally.

---

## Wire-compat invariants

The Go side MUST keep these byte-identical to the Rust side while both
implementations coexist:

- `/healthz` payload shape.
- JWT claims field names + JSON tags.
- Dataset RID format `ri.foundry.main.dataset.<uuid-v7>`.
- Transaction state/type tokens (`open|committed|aborted`,
  `snapshot|append|update|delete`).
- Marking source discriminator (`{"kind":"direct"}` /
  `{"kind":"inherited_from_upstream","upstream_rid":"..."}`).
- Media reference camelCase keys (`mediaSetRid`, `mediaItemRid`,
  `branch`, `schema`).
- Schema field type discriminator (`{"type":"DECIMAL","precision":...
,"scale":...}`).

The test suites under `openfoundry-go/libs/core-models/**/*_test.go`
already lock these.

---

## Cutover protocol (per service)

Each service follows the same pattern:

1. **Ship Go binary alongside Rust** in the same Helm release with the
   same SecretMount, Postgres user and Kafka principal.
2. **Header-based routing** at `edge-gateway-service`:
   `X-Of-Migration: go-canary` → Go pod, default → Rust pod.
3. **Contract diff suite** runs against both implementations in CI;
   mismatches block the cutover.
4. **Traffic ramp**: 1 % → 10 % → 50 % → 100 %, each step ≥ 24 h with
   error-budget gates.
5. **Soak**: 100 % Go for ≥ 14 days before removing the Rust pod.
6. **Decommission**: remove the Rust crate from the workspace, delete
   the Helm subchart entry.

---

## Operating cadence

- One service migration in flight at a time per pair of engineers.
- One PR per service migration, never bundling multiple.
- The `openfoundry-go/` Makefile + `.github/workflows/ci.yml` are the
  authoritative gates. Local `make ci` must pass before review.
- Memory-only state: every change to behavior is gated by a contract
  test, never by hidden state.

---

## How to start

```sh
cd openfoundry-go
make tools          # one-time
make gen            # populate libs/proto-gen
make ci             # green = ready to migrate the next service
```

When you migrate a new service, **copy `services/_template/` and rename
`template` → `<your-service>` everywhere** (filename, package, ldflags
target, Dockerfile ARG). Add the OpenAPI spec under `api/`, the SQL
schema + queries under `internal/repo/`, and a sqlc block in
`sqlc.yaml`. CI handles the rest.
