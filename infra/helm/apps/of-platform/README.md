# of-platform — retired Temporal workers

> **All Go Temporal workers in this chart have been retired** by
> FASE 3-7 of the Foundry-pattern migration. The chart no longer
> deploys any Temporal worker; what remains is the cross-release
> ConfigMap and the optional `apollo` + `approvals-timeout-sweep`
> CronJobs.
>
> Per-task replacement summary:
>
> * `pipeline-worker` (task queue `openfoundry.pipeline`) — Tarea
>   3.6. Pipeline runs are now submitted as `SparkApplication` CRs
>   by `pipeline-build-service`, and cron-driven runs are fired by
>   the `schedules-tick` `CronJob` (binary from
>   `libs/event-scheduler`).
> * `workflow-automation-worker` (task queue
>   `openfoundry.workflow-automation`) — Tarea 5.4. Automation runs
>   are driven by `workflow-automation-service` itself: it consumes
>   `automate.condition.v1` from Kafka, dispatches the effect HTTP
>   call to `ontology-actions-service`, and publishes
>   `automate.outcome.v1` via the transactional outbox + Debezium.
> * `automation-ops-worker` (task queue
>   `openfoundry.automation-ops`) — Tarea 6.5. Saga-driven
>   operations are driven by `automation-operations-service`
>   itself: it consumes `saga.step.requested.v1` from Kafka, runs
>   the matching step graph through `libs/saga::SagaRunner`, and
>   publishes `saga.step.*.v1` lifecycle events via the
>   transactional outbox. Compensations execute LIFO inside the
>   runner; the chaos test at
>   `services/automation-operations-service/tests/saga_chaos.rs`
>   validates the contract.
> * `approvals-worker` (task queue `openfoundry.approvals`) — Tarea
>   7.5. Approval lifecycle is driven by `approvals-service`
>   itself: HTTP handlers persist the
>   `audit_compliance.approval_requests` row + the matching
>   `approval.*.v1` outbox event in one transaction (state machine
>   via `libs/state-machine::PgStore<ApprovalRequest>`). Timeouts
>   are owned by the companion `approvals-timeout-sweep` CronJob —
>   see `templates/approvals-timeout-sweep-cronjob.yaml` and
>   `services/approvals-service/src/bin/approvals_timeout_sweep.rs`.
>   Audit emission is a synchronous HTTP POST inside the decide
>   handler; FASE 9 collapses it into a Kafka consumer of
>   `approval.completed.v1`.

## Overlays

Use the environment overlays with the base values:

```bash
helm template of-platform infra/helm/apps/of-platform \
  -f infra/helm/apps/of-platform/values.yaml \
  -f infra/helm/apps/of-platform/values-dev.yaml

helm upgrade --install of-platform infra/helm/apps/of-platform \
  -n openfoundry --create-namespace \
  -f infra/helm/apps/of-platform/values.yaml \
  -f infra/helm/apps/of-platform/values-staging.yaml
```

## Verify the approvals-timeout-sweep CronJob (Tarea 7.4)

```bash
kubectl -n openfoundry get cronjob approvals-timeout-sweep
kubectl -n openfoundry get jobs -l app.kubernetes.io/name=approvals-timeout-sweep
kubectl -n openfoundry logs -l app.kubernetes.io/name=approvals-timeout-sweep --tail=50
```

A healthy run logs `timeout sweep completed` with `expired=N`
where `N` is the number of pending rows that crossed their
`expires_at` deadline since the previous tick (default cadence:
every 5 minutes).
