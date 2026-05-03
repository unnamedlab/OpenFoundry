# ADR-0021: Temporal on Cassandra, with business workers in Go

- **Status:** Accepted
- **Date:** 2026-05-02
- **Deciders:** OpenFoundry platform architecture group
- **Supersedes / supplements:**
  - The bespoke scheduler in
    [services/workflow-automation-service/src/domain/executor.rs](../../../services/workflow-automation-service/src/domain/executor.rs)
    and the in-process tick loop in
    [services/pipeline-schedule-service/src/main.rs](../../../services/pipeline-schedule-service/src/main.rs).
  - The "workflow engine casero" risk recorded in
    [docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md).
- **Related ADRs:**
  - [ADR-0020](./ADR-0020-cassandra-as-operational-store.md) — Temporal
    persistence and visibility share the platform Cassandra cluster.
  - [ADR-0022](./ADR-0022-transactional-outbox-postgres-debezium.md) —
    Activities that must publish a domain event do so by writing to the
    Postgres outbox; Debezium publishes to Kafka.
  - [ADR-0025](./ADR-0025-eliminate-custom-scheduler.md) — Companion
    decision that retires the in-house scheduler.

## Context

OpenFoundry today runs two pieces of in-process workflow machinery:

- `services/workflow-automation-service` reimplements scheduling and
  orchestration with Postgres state and the `cron = "0.12"` crate.
- `services/pipeline-schedule-service` runs an in-process tick loop and
  is currently scaled to **a single replica** because two replicas would
  double-fire jobs.

Both implementations lack durable execution, signal handling, exactly-
once scheduling guarantees, retries with bounded backoff, deduplication
of side effects and any cross-service coordination story. The audit in
[docs/architecture/audit-and-reference-no-spof.md](../audit-and-reference-no-spof.md)
classifies this as a critical SPOF and a correctness risk.

The platform also needs, going forward:

- Long-running approval flows with human-in-the-loop signals.
- Pipeline runs with retry, compensation and visibility.
- Scheduled jobs with exactly-once semantics across replicas and DCs.
- A multi-DC story that survives a regional failure
  ([ADR-0023](./ADR-0023-iceberg-cross-region-dr.md)).

A workflow engine that is durable, replayable and multi-DC is the
correct primitive here. We need to choose the engine, the persistence
backend and — critically — the language in which workers are written,
because the Rust ecosystem around Temporal is not at the same maturity
as Go or Java.

## Options considered

### Engine choice

#### Engine A — Temporal (chosen)

- Apache-2.0, mature, durable execution model, exactly-once activity
  semantics, signals, queries, updates, schedules, child workflows,
  versioning (`patched`), continue-as-new, multi-DC.
- First-class persistence backends include **Cassandra**, which we are
  already adopting in [ADR-0020](./ADR-0020-cassandra-as-operational-store.md);
  this collapses two storage decisions into one.
- Polyglot SDK story (Go, Java, .NET, TypeScript, Python, Ruby; Rust in
  prerelease) makes it possible to mix the language we use for workers
  with the language we use for services.

#### Engine B — Argo Workflows

- Kubernetes-native, YAML / DAG style, optimised for batch / CI-style
  pipelines.
- Lacks first-class signals, durable per-instance state machines, and
  a programmable workflow API. Not a fit for human-in-the-loop
  approvals or for long-running domain workflows.

#### Engine C — Cadence (Uber)

- The ancestor of Temporal. Smaller community, slower release cadence,
  Temporal is the de facto successor.

#### Engine D — Roll our own (current state)

- Already proved insufficient. Rejected.

### Persistence backend for Temporal

Temporal's first-class backends are Cassandra, PostgreSQL and MySQL.

- **Cassandra (chosen)** — Aligns with
  [ADR-0020](./ADR-0020-cassandra-as-operational-store.md). Multi-DC
  story works out of the box with `NetworkTopologyStrategy`. No second
  storage technology to introduce.
- **PostgreSQL** — Would force us to either size the consolidated
  Postgres clusters for Temporal write load (a poor fit per the same
  reasoning as ADR-0020) or stand up a dedicated `pg-temporal` cluster
  (a regression from the consolidation in
  [ADR-0024](./ADR-0024-postgres-consolidation.md)).

### Visibility backend

Temporal's visibility store can be backed by the same persistence
backend, by Elasticsearch (advanced visibility) or by SQL.

- **Cassandra (chosen, advanced visibility via Cassandra 5 SAI)** —
  Avoids introducing Elasticsearch as a new operational dependency.
  Cassandra 5's Storage-Attached Indexes provide enough query
  flexibility for the visibility workloads we need (workflow listings,
  filters by type / status / time / tag).
- **Elasticsearch** — Rejected. Adds a new HA stateful system whose
  search needs are already covered by Vespa
  ([ADR-0028](./ADR-0028-search-backend-abstraction.md)) for our
  domain search. Adding Elasticsearch only for Temporal visibility
  would duplicate the search-engine concern in the platform.

### Worker language (the contentious decision)

This is the decision that requires the most evidence, because the Rust
SDK looks tantalisingly close to usable as of May 2026.

#### Worker option W1 — Rust SDK end-to-end

- `temporalio-sdk` is at **v0.4.0** on crates.io as of 29 April 2026.
- `temporalio-sdk-core`, the shared core that powers TypeScript / Python
  / .NET / Ruby SDKs, is also at **v0.4.0** and is documented as
  "APIs provided by this crate are not considered stable and may break
  at any time."
- Release cadence in 2026 has been aggressive: v0.2 → v0.3 → v0.4 in
  41 days, with breaking changes flagged as `💥` in the changelog on
  every minor.
- Material gaps versus the Go and Java SDKs (open issues on the
  upstream repo as of late April 2026):
  - **No interceptors** for client / activity / workflow
    (issues #1138, #1139, #1140) — required for centralised payload
    encryption, tracing and audit.
  - **No testing framework** (issue #1144) — no
    `TestWorkflowEnvironment`, no time-skipping, no replay test
    helper. This is critical for CI.
  - **No Sessions** — sticky activity routing to a worker is not
    implemented.
  - **No Protobuf binary payload codec** (issue #1142, JSON only).
- Determinism detector exists and is enabled by default, but does not
  catch every source of non-determinism (e.g. `futures::select!` without
  `biased`, or `SystemTime::now()`); developers must still hand-pick
  workflow-safe primitives (`workflows::select!`, `workflows::join!`).
- No public production reference. Reddit thread of February 2025 was
  answered by the official Temporal account with "we don't have an
  official Rust SDK"; the prerelease crates appeared in February 2026.
- Versioning (`ctx.patched("…")`) is implemented and documented, on
  par with Go's `workflow.GetVersion`, but issue #1223 (NDE on patches)
  was opened and fixed within 24 hours in April 2026 — the mechanism
  works, but the surface is still finding edge cases.

The cost of choosing W1 today is:

- Workflows already in flight may not be replay-safe across minor
  releases (the `💥` markers are not a paper risk).
- We would build interceptors, the testing framework and Sessions
  ourselves, or live without them.
- We would be the production reference user — first to find any class
  of bugs that the maintainers have not yet exercised against real
  workloads.

#### Worker option W2 — Go SDK in dedicated worker pods (chosen)

- `go.temporal.io/sdk` is GA, has been used in production by Temporal
  Inc. and many third parties for years, and exposes the full feature
  surface (interceptors, testing framework, Sessions, Schedules,
  versioning, replay tests, encryption codecs).
- Workers run as **independent Kubernetes Deployments**, not as
  sidecars in the Rust service pods. Communication with Rust services
  is **HTTP REST + JSON** (see "Wire format" below); Temporal
  mediates execution.
- The Rust services keep the **client side** of Temporal, using the
  `temporalio-client` crate. The client surface is much more stable
  than the worker surface (it is a thin wrapper over the gRPC API),
  it is the part of the prerelease that the rest of the SDK ecosystem
  is built on, and it gives Rust services native `async/await` access
  to start workflows, send signals, query state and wait for results.
- Operational footprint:
  - One additional language toolchain (Go) in CI and in the developer
    laptop. Mitigated by isolating Go code under `workers-go/` with
    its own `go.work` and dedicated `just` recipes.
  - Independent scaling of workers: a workflow that needs more
    activity throughput scales by adding `workers-go/<domain>/`
    pods, not by scaling the Rust services.
  - Independent crash isolation between Rust services and Go workers.

#### Worker option W3 — Java SDK in dedicated worker pods

- Equivalent maturity to Go. Rejected because Go's runtime footprint
  (single static binary, ~20 MB image) is a better fit for the
  platform than the JVM, and because the team has more Go than Java
  bandwidth.

#### Worker option W4 — Sidecar (same pod) with Go / Java

- Co-located worker shares the lifecycle of the Rust service pod.
  Adds coupling (a worker restart impacts the service and vice versa),
  complicates HPA (the metric you scale on becomes ambiguous), and
  buys us nothing that pod-separated workers do not already give us.
- Rejected.

## Decision

We adopt **Temporal 1.24+** with **Cassandra as both persistence and
visibility backend**, deployed via the official `temporalio/temporal`
Helm chart. We deploy:

- `frontend` × 3
- `history` × 3
- `matching` × 3
- `worker` (Temporal system worker) × 3
- `web` (Temporal UI) × 2 behind the OpenFoundry edge gateway with
  OIDC authentication via `identity-federation-service`.

Business workflows and activities are written in **Go**, using the GA
`go.temporal.io/sdk` v1.28+, and live under a dedicated top-level
directory `workers-go/` with one binary per domain
(`workflow-automation`, `pipeline`, `approvals`, `automation-ops`,
`reindex`).

Rust services keep the **client side only**, using the
`temporalio-client` crate (currently v0.4 prerelease but already the
most stable surface of the Rust SDK) wrapped behind a workspace crate
`libs/temporal-client` with strongly typed per-domain client helpers.

Activities Go invokes do not bypass service boundaries: they call the
owning Rust service over **HTTP REST + JSON** with a service-token
bearer and the `x-audit-correlation-id` header (see "Wire format"
below). There is no shortcut from Go workers to Cassandra or
Postgres. The single exception is the `reindex` worker, whose
explicit job is to scan Cassandra and publish to Kafka — that
exception is documented inline in [`workers-go/README.md`](../../../workers-go/README.md).

### Wire format between Go activities and Rust services

When ADR-0021 was first drafted (May 2026) the assumption was that
activities would consume Go bindings generated from `proto/`. We
reverted that assumption while wiring S2.3 / S2.5 / S2.6 because:

1. The Rust services on the receiving end (`ontology-actions-service`,
   `pipeline-authoring-service`, `pipeline-build-service`,
   `audit-compliance-service`, `automation-operations-service`) all
   expose REST handlers, not gRPC servers. Adding gRPC servers in
   Rust would have been a parallel, larger migration with no
   functional payoff for the worker case (single in-cluster hop, no
   streaming).
2. `buf.gen.yaml` already emits Rust + TypeScript bindings; adding a
   third Go target would force every proto change to regenerate and
   commit `proto/gen/go`, plus a Dockerfile/COPY dance for each
   worker image. The activities are thin enough (a JSON encode + an
   HTTP POST) that the bindings would not earn their keep.
3. The audit-correlation header `x-audit-correlation-id` is identical
   on the wire whether the transport is HTTP or gRPC metadata, so
   nothing in the audit chain is sensitive to the choice.

The canonical contract for activities is therefore:

- **Transport**: HTTP/1.1 to the owning service inside the cluster.
- **Body**: JSON, shape derived from the corresponding `proto/`
  message but written directly as `map[string]any` in Go (the proto
  files remain the source-of-truth that the Rust handler validates
  against).
- **Auth**: `Authorization: Bearer <service-token>` from
  `OF_<SERVICE>_BEARER_TOKEN`.
- **Correlation**: `x-audit-correlation-id: <uuid-v7>` from the
  workflow's `audit_correlation_id` search attribute.
- **Idempotency**: 4xx responses (other than 429) become
  `temporal.NewNonRetryableApplicationError`; 5xx and 429 are retried
  by Temporal under the workflow's `RetryPolicy`.

`proto/` continues to be the contract source for the TypeScript web
client and for `libs/proto-gen` (Rust). If a future audit shows the
maintenance cost of hand-written JSON in Go activities exceeds the
buf-generated alternative, this decision can be revisited without
touching the Rust services or the Temporal wiring.


## Topology and configuration

### Helm release

- Chart: `temporalio/temporal` (Helm chart, Apache-2.0).
- Namespace: `temporal-system`.
- Persistence: Cassandra cluster from
  [ADR-0020](./ADR-0020-cassandra-as-operational-store.md), keyspaces
  `temporal_persistence` and `temporal_visibility`.
- TLS: enabled in production (mTLS via Linkerd in-mesh; Temporal
  frontend exposed only inside the mesh).
- Authentication: Temporal CLI / UI authenticate against
  `identity-federation-service` OIDC.

### Keyspaces

| Keyspace                | RF                       | Schema source                  |
| ----------------------- | ------------------------ | ------------------------------ |
| `temporal_persistence`  | `{dc1:3, dc2:3, dc3:3}`  | Temporal schema CLI            |
| `temporal_visibility`   | `{dc1:3, dc2:3, dc3:3}`  | Temporal schema CLI            |

Schema is bootstrapped via `temporal-cassandra-tool setup-schema` and
`update-schema` as a Helm Job (`pre-install` / `pre-upgrade`).

### Default consistency

`LOCAL_QUORUM` for both reads and writes, matching the platform default
([ADR-0020](./ADR-0020-cassandra-as-operational-store.md)).

### Worker layout

```
workers-go/
  go.work
  go.work.sum
  shared/                     (proto-generated clients, common helpers)
  workflow-automation/        (binary; one Temporal task queue)
    cmd/worker/main.go
    workflows/
    activities/
    Dockerfile
  pipeline/
  approvals/
  automation-ops/
  reindex/
```

Each worker:

- Reads `TEMPORAL_HOSTPORT`, `TEMPORAL_NAMESPACE` and
  `TEMPORAL_TASK_QUEUE` from environment.
- Registers its workflows and activities at startup.
- Calls Rust services over HTTP REST + JSON (see "Wire format"
  above), propagating the `x-audit-correlation-id` header.
- Emits OpenTelemetry traces and Prometheus metrics in the same
  format as the rest of the platform.

### Rust client layout

`libs/temporal-client` exposes typed wrappers per domain:

```rust
pub struct WorkflowAutomationClient { /* … */ }
impl WorkflowAutomationClient {
    pub async fn run_action_workflow(&self, req: RunActionRequest) -> Result<WorkflowHandle>;
}
```

Backed by `temporalio_client::Client`, configured from the same
`TEMPORAL_HOSTPORT` env var. No business logic lives in Rust workers;
no workflow definitions live in Rust.

## Operational consequences

- New top-level directory `workers-go/` with its own `go.work` and CI
  job (`go-workers-build`).
- New `infra/k8s/platform/manifests/temporal/` Helm release.
- New runbook `infra/runbooks/temporal.md` covering schema upgrades,
  task queue rebalancing, scaling history shards, namespace
  configuration, retention policies and the failover procedure for the
  multi-DC scenario in [ADR-0023](./ADR-0023-iceberg-cross-region-dr.md).
- Grafana dashboard 17567 imported for Temporal SDK metrics.
- New `just` recipes: `just go-build`, `just go-test`,
  `just go-worker <name>`, `just temporal-tctl`.
- Dependency on the Go toolchain in CI and dev images.

## Consequences

### Positive

- Durable execution, exactly-once activity semantics and bounded
  retries become platform primitives, removing a whole class of
  correctness bugs.
- The custom scheduler in `pipeline-schedule-service` is retired
  ([ADR-0025](./ADR-0025-eliminate-custom-scheduler.md)); Temporal
  Schedules give exactly-once cron semantics across replicas and DCs.
- Multi-DC failover for workflows comes for free with the Cassandra
  multi-DC topology already chosen in
  [ADR-0020](./ADR-0020-cassandra-as-operational-store.md).
- We pick the language for workers based on **today's** maturity, not
  on aspiration; the platform does not block on the Rust SDK reaching
  GA.
- Rust services stay Rust. Their public contract (gRPC / OpenAPI / SDK)
  does not change because Temporal lives behind it.

### Negative

- A second language toolchain (Go) enters CI and dev environments.
  Mitigated by strict isolation under `workers-go/` and by the fact
  that the team already operates Go-based infrastructure (Strimzi
  operator, k8ssandra-operator are written in Go but not in our repo;
  this is the first Go code we own).
- Workers cannot share business types with Rust services through the
  Rust type system; the contract is HTTP/JSON, with `proto/` as the
  message-shape source-of-truth. This is a feature, not a bug — the
  contract is explicit, versioned, and the same one external clients
  use.
- Temporal adds a new HA stateful system to operate (the Temporal
  cluster itself). Mitigated by sharing the Cassandra backend with
  the rest of the platform.

### Neutral

- The `temporalio-client` Rust crate is still prerelease. The client
  surface is small and stable, the risk is low, and migrating to a
  later version (or to a future Rust worker SDK) does not require
  changing the worker code, which is in Go.

## Re-evaluation trigger

This ADR is **scheduled for re-evaluation in May 2027** (T+12 months),
or sooner if **any** of the following becomes true:

- The Rust `temporalio-sdk` worker crate reaches a 1.0 release with a
  semver-stable API.
- Interceptors, the testing framework and Sessions are all merged
  upstream and tagged in a release.
- One of the SDK maintainers publishes a production reference at
  comparable scale to our workloads.
- A platform-internal pain point (e.g. cross-language type drift,
  build-time cost of Go workers) outweighs the maturity benefit of
  Go SDK.

If re-evaluation concludes that the Rust SDK is ready, the migration
path is straightforward: rewrite one worker domain at a time inside
`workers-go/` → `workers-rust/`, keep the same wire contract (REST
or whatever it has become by then), retire
each Go binary as its Rust replacement passes the test suite. The
service-side code (Rust) does not change at all because it only ever
talked to Temporal through `temporalio-client`.

## Follow-ups

- Implement migration plan task **S2.1** (Temporal cluster HA on
  Cassandra).
- Implement migration plan task **S2.2** (Rust client crate
  `libs/temporal-client` + Go worker scaffolding under `workers-go/`).
- Implement migration plan tasks **S2.3 – S2.7** (port each existing
  workflow to a Go worker; retire the Rust scheduler).
- Add a CI check that fails if any Rust crate adds a direct dependency
  on `temporalio-sdk` (the worker SDK) — only `temporalio-client` is
  permitted in Rust until this ADR is re-evaluated.
