# Go workers — Temporal SDK

This Go workspace hosts the **business workers** that execute
OpenFoundry workflows on Apache Temporal. Per
[ADR-0021](../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md)
the worker side of every workflow lives in Go (SDK GA, mature),
while the Rust services act exclusively as **clients** through
[`libs/temporal-client`](../libs/temporal-client).

## Layout

```
workers-go/
├── README.md                 # this file
├── go.work                   # Go 1.23 workspace pinning the modules
├── go.work.sum               # Go workspace dependency hashes (generated)
├── approvals/                # approval workflows with signals (S2.5)
└── automation-ops/           # automation-operations workflows (S2.7)
```

> The `reindex/` worker was removed in Tarea 4.3 (May 2026); it is now
> [`services/reindex-coordinator-service`](../services/reindex-coordinator-service)
> (Rust, Kafka-driven, Postgres-resumeable). Likewise, `pipeline/` was
> superseded by [`services/pipeline-build-service`](../services/pipeline-build-service)
> in Tarea 3.6, and `workflow-automation/` was superseded by
> [`services/workflow-automation-service`](../services/workflow-automation-service)
> in Tarea 5.4 (Kafka condition consumer + outbox-driven outcomes).

Each subdirectory is an independent Go module producing one binary
that registers a single task queue's workflows + activities.

## Conventions

* **Task queues** are pinned in
  [`libs/temporal-client/src/lib.rs::task_queues`](../libs/temporal-client/src/lib.rs).
  Workers import the **same constants** from
  `internal/contract/contract.go` of each module — and the value
  must match byte-for-byte. A typo silently wedges the workflow.
* **Workflow type names** are pinned in the same Rust module under
  `workflow_types`. Same agreement rule.
* **Activities never touch Cassandra/Postgres directly** — they call
  the Rust service that owns the data, which enforces Cedar
  authorization and writes the audit event.
* **Wire format: HTTP REST + JSON, bearer token, correlation
  header.** Activities call the owning service's REST surface
  (`POST /api/v1/...` documented in each service's OpenAPI). The
  request carries `Authorization: Bearer <service-token>` and
  `x-audit-correlation-id: <uuid-v7>`. `proto/` remains the
  source-of-truth for message shapes (and is what the TypeScript
  client and the Rust services compile against), but workers do not
  consume Go bindings — `buf.gen.yaml` only emits Rust + TypeScript.
  Rationale and decision history: [ADR-0021 §Worker
  layout](../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md)
  and tasks S2.3.c / S2.5.c / S2.6 of the
  [migration plan](../docs/architecture/migration-plan-cassandra-foundry-parity.md).
  Service URLs are read from `OF_<SERVICE>_URL` (the legacy
  `OF_<SERVICE>_GRPC_ADDR` name is still accepted for backward
  compatibility — it is misleading but harmless).
* **Audit correlation**: every workflow input must carry an
  `audit_correlation_id` UUID v7 (set by
  `StartWorkflowOptions::new` on the Rust side and exposed as a
  Temporal search attribute). Activities propagate it on every
  outbound HTTP call as the `x-audit-correlation-id` header.
* **Logging**: `log/slog` with JSON output, level from
  `OF_LOG_LEVEL` env var (defaults to `info`).
* **Metrics**: SDK ships Prometheus metrics on `:9090/metrics`.
  Grafana dashboard 17567.

## Local development

```bash
# Boot a single-node Temporal dev server matching the Rust E2E harness.
docker run --rm -p 7233:7233 -p 8233:8233 temporalio/temporal:1.7.0 \
  server start-dev --ip 0.0.0.0 --ui-ip 0.0.0.0 \
  --search-attribute audit_correlation_id=Keyword

# Run one worker.
just go-worker approvals
```

## Temporal E2E

The Rust integration tests can start their own
`temporalio/temporal:1.7.0` dev-server container and launch the real Go worker
modules with `go run .`.

```bash
cargo test -p pipeline-schedule-service --features it-temporal --test temporal_schedule_idempotency -- --test-threads=1
```

Local prerequisites: Docker daemon reachable by `testcontainers`, Go
on `PATH`, and the usual Cargo toolchain. CI should run the same two
commands in a job with Docker service access after `cargo check` and
`go test ./...` for the worker workspace. The tests do not require a
preexisting Temporal frontend; they allocate an ephemeral container and
tear down the worker processes at the end.

## Production

Each worker is a separate Kubernetes Deployment with 3 replicas,
auto-scaled by the Temporal SDK's `MaxConcurrentWorkflowTaskExecutionSize`
and `MaxConcurrentActivityExecutionSize` knobs (set via env vars,
documented in each module's `README.md`).

Images and chart values live in
[`infra/helm/apps/of-platform`](../infra/helm/apps/of-platform). The
chart renders `approvals-worker` and `automation-ops-worker` as
standalone Deployments with `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`,
`TEMPORAL_TASK_QUEUE`, service-token secrets and `:9090/metrics`.
