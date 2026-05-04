# of-platform Temporal workers

`of-platform` deploys the S2 Temporal business workers as
standalone Kubernetes `Deployment` resources:

| Deployment | Task queue | Metrics |
|---|---|---|
| `approvals-worker` | `openfoundry.approvals` | `:9090/metrics` |

> **Note:** The `pipeline-worker` (task queue `openfoundry.pipeline`)
> was removed by Tarea 3.6 of the Foundry-pattern migration. Pipeline
> runs are now submitted as `SparkApplication` CRs by
> `pipeline-build-service`, and cron-driven runs are fired by the
> `schedules-tick` `CronJob` (binary from `libs/event-scheduler`).
>
> The `workflow-automation-worker` (task queue
> `openfoundry.workflow-automation`) was removed by Tarea 5.4.
> Automation runs are now driven by the
> `workflow-automation-service` itself: it consumes
> `automate.condition.v1` from Kafka, dispatches the effect HTTP
> call to `ontology-actions-service`, and publishes
> `automate.outcome.v1` via the transactional outbox + Debezium.
>
> The `automation-ops-worker` (task queue
> `openfoundry.automation-ops`) was removed by Tarea 6.5.
> Saga-driven operations are now driven by the
> `automation-operations-service` itself: it consumes
> `saga.step.requested.v1` from Kafka, runs the matching step
> graph through `libs/saga::SagaRunner`, and publishes
> `saga.step.*.v1` lifecycle events via the transactional outbox.
> Compensations execute LIFO inside the runner; the chaos test
> at `services/automation-operations-service/tests/saga_chaos.rs`
> validates the contract.

Each worker gets `TEMPORAL_ADDRESS`, `TEMPORAL_HOST_PORT`,
`TEMPORAL_NAMESPACE`, `TEMPORAL_TASK_QUEUE`, `METRICS_ADDR`, activity
service URLs and optional bearer-token `secretKeyRef`s from
`temporalWorkers.*` values. They are not sidecars.

## Overlays

Use the environment overlays with the base values:

```bash
helm template of-platform infra/k8s/helm/of-platform \
  -f infra/k8s/helm/of-platform/values.yaml \
  -f infra/k8s/helm/of-platform/values-dev.yaml

helm upgrade --install of-platform infra/k8s/helm/of-platform \
  -n openfoundry --create-namespace \
  -f infra/k8s/helm/of-platform/values.yaml \
  -f infra/k8s/helm/of-platform/values-staging.yaml
```

`values-dev.yaml` uses one replica and disables `ServiceMonitor` by
default. Staging and prod enable `ServiceMonitor`; prod starts at three
replicas with PDB `minAvailable: 2`.

## Verify Pollers

1. Check the Deployments are independent:

```bash
kubectl -n openfoundry get deploy \
  approvals-worker automation-ops-worker
```

2. Check each pod logs the task queue and namespace:

```bash
kubectl -n openfoundry logs deploy/approvals-worker | grep 'worker starting'
kubectl -n openfoundry logs deploy/automation-ops-worker | grep 'worker starting'
```

3. Ask Temporal for active pollers. Port-forward the frontend if it is
not directly reachable:

```bash
kubectl -n temporal port-forward svc/temporal-frontend 7233:7233

temporal task-queue describe \
  --address 127.0.0.1:7233 \
  --namespace openfoundry-dev \
  --task-queue openfoundry.approvals \
  --task-queue-type workflow
```

Repeat with `openfoundry.automation-ops`.
The command should show non-empty `pollers`. If a queue has zero
pollers, inspect the matching Deployment logs and confirm
`TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`, and `TEMPORAL_TASK_QUEUE`
match the Rust clients.

4. Check Prometheus scraping:

```bash
kubectl -n openfoundry get svc,servicemonitor \
  approvals-worker automation-ops-worker
kubectl -n openfoundry port-forward svc/approvals-worker 9090:9090
curl -fsS http://127.0.0.1:9090/healthz
curl -fsS http://127.0.0.1:9090/metrics
```
