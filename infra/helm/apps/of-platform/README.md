# of-platform Temporal workers

`of-platform` deploys the four S2 Temporal business workers as
standalone Kubernetes `Deployment` resources:

| Deployment | Task queue | Metrics |
|---|---|---|
| `workflow-automation-worker` | `openfoundry.workflow-automation` | `:9090/metrics` |
| `pipeline-worker` | `openfoundry.pipeline` | `:9090/metrics` |
| `approvals-worker` | `openfoundry.approvals` | `:9090/metrics` |
| `automation-ops-worker` | `openfoundry.automation-ops` | `:9090/metrics` |

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
  workflow-automation-worker pipeline-worker approvals-worker automation-ops-worker
```

2. Check each pod logs the task queue and namespace:

```bash
kubectl -n openfoundry logs deploy/workflow-automation-worker | grep 'worker starting'
kubectl -n openfoundry logs deploy/pipeline-worker | grep 'worker starting'
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
  --task-queue openfoundry.workflow-automation \
  --task-queue-type workflow
```

Repeat with `openfoundry.pipeline`, `openfoundry.approvals`, and
`openfoundry.automation-ops`. The command should show non-empty
`pollers`. If a queue has zero pollers, inspect the matching Deployment
logs and confirm `TEMPORAL_ADDRESS`, `TEMPORAL_NAMESPACE`, and
`TEMPORAL_TASK_QUEUE` match the Rust clients.

4. Check Prometheus scraping:

```bash
kubectl -n openfoundry get svc,servicemonitor \
  workflow-automation-worker pipeline-worker approvals-worker automation-ops-worker
kubectl -n openfoundry port-forward svc/workflow-automation-worker 9090:9090
curl -fsS http://127.0.0.1:9090/healthz
curl -fsS http://127.0.0.1:9090/metrics
```
