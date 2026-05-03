# `automation-ops` worker

Task queue: `openfoundry.automation-ops`. Hosts `AutomationOpsTask`
(S2.7). Replaces the cron-driven sweepers in
`automation-operations-service` with durable workflows.

## Configuration

| env var | default | purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` | `127.0.0.1:7233` | Frontend gRPC; preferred Helm value |
| `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Frontend gRPC fallback |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.automation-ops` | Task queue polled by this worker |
| `OF_AUTOMATION_OPS_URL` | `http://automation-operations-service:50116` | Base URL used by `ExecuteTask` for `POST /api/v1/automations/{task_id}/runs` |
| `OF_AUTOMATION_OPS_BEARER_TOKEN` | _(empty)_ | Optional service bearer token forwarded to automation-operations-service |

`AUTOMATION_OPERATIONS_SERVICE_URL` and the older
`OF_AUTOMATION_OPS_GRPC_ADDR` are accepted as rollout fallbacks while
manifests converge on the HTTP env name.
