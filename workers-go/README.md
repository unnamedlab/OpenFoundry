# Go workers — retired

> **All Go Temporal workers have been retired** by FASE 3-7 of the
> Foundry-pattern migration plan
> ([`docs/architecture/migration-plan-foundry-pattern-orchestration.md`](../docs/architecture/migration-plan-foundry-pattern-orchestration.md)).
> The directory is kept as a placeholder; FASE 9 / Tarea 9.x will
> remove it entirely.
>
> Per-task replacements:
>
> * `pipeline/` → [`services/pipeline-build-service`](../services/pipeline-build-service)
>   + the `schedules-tick` `CronJob` (binary from
>   [`libs/event-scheduler`](../libs/event-scheduler)) — Tarea 3.6.
> * `reindex/` → [`services/reindex-coordinator-service`](../services/reindex-coordinator-service)
>   (Rust, Kafka-driven, Postgres-resumable) — Tarea 4.3.
> * `workflow-automation/` → [`services/workflow-automation-service`](../services/workflow-automation-service)
>   (Kafka condition consumer + outbox-driven outcomes) — Tarea
>   5.4.
> * `automation-ops/` → [`services/automation-operations-service`](../services/automation-operations-service)
>   (Kafka saga consumer + `libs/saga::SagaRunner` with LIFO
>   compensation) — Tarea 6.5.
> * `approvals/` → [`services/approvals-service`](../services/approvals-service)
>   (`audit_compliance.approval_requests` state machine + the
>   `approvals-timeout-sweep` CronJob in
>   [`infra/helm/apps/of-platform`](../infra/helm/apps/of-platform/templates/approvals-timeout-sweep-cronjob.yaml))
>   — Tarea 7.5.
>
> The Cassandra-parity migration plan
> [`docs/architecture/migration-plan-cassandra-foundry-parity.md`](../docs/architecture/migration-plan-cassandra-foundry-parity.md)
> still references the workers historically (S2.3 / S2.5 / S2.7
> streams). Those references are retained as audit trail; do not
> resurrect them.

## Layout

```
workers-go/
├── README.md       # this file
├── go.work         # placeholder workspace (no live modules)
└── go.work.sum     # workspace dependency hashes (generated)
```

There are no Go modules under this directory anymore. `just
go-tidy` is now a no-op.
