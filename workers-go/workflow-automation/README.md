# `workflow-automation` worker

Temporal task queue: `openfoundry.workflow-automation`

This worker registers the business workflows that replace the
legacy `services/workflow-automation-service/src/domain/executor.rs`
in-process scheduler (archived in S2.3.a). Each branch / parallel /
compensation / human-in-the-loop pattern of the old executor lands
here as a typed `WorkflowDefinition` over time.

## Build

```bash
just go-build  # builds all 4 workers
just go-build-svc workflow-automation
```

## Run locally

```bash
docker run --rm -p 7233:7233 -p 8233:8233 temporalio/auto-setup:1.24
just go-worker workflow-automation
```

## Configuration

| env var | default | purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` | `127.0.0.1:7233` | Frontend gRPC; preferred Helm value |
| `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Frontend gRPC fallback |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.workflow-automation` | Task queue polled by this worker |
| `OF_LOG_LEVEL` | `info` | slog level |
| `METRICS_ADDR` | `:9090` | Prometheus exporter |
| `OF_ONTOLOGY_ACTIONS_URL` | _(required for action activity)_ | Base URL for `ontology-actions-service` |
| `OF_ONTOLOGY_ACTIONS_BEARER_TOKEN` | _(required for action activity)_ | Service bearer token used on `POST /api/v1/ontology/actions/{id}/execute` |

`ONTOLOGY_ACTIONS_SERVICE_URL`, `ONTOLOGY_SERVICE_URL`, and the
older `OF_ONTOLOGY_ACTIONS_GRPC_ADDR` are accepted as URL fallbacks so
existing environments can roll forward without an immediate manifest
rename.
