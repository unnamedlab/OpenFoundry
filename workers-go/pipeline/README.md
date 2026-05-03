# `pipeline` worker

Task queue: `openfoundry.pipeline`. Hosts `PipelineRun` workflow
invoked by both ad-hoc requests (`pipeline-authoring-service`) and
Temporal Schedules created by `pipeline-schedule-service` (S2.4).

## Configuration

| env var | default | purpose |
|---|---|---|
| `TEMPORAL_ADDRESS` | `127.0.0.1:7233` | Frontend gRPC; preferred Helm value |
| `TEMPORAL_HOST_PORT` | `127.0.0.1:7233` | Frontend gRPC fallback |
| `TEMPORAL_NAMESPACE` | `default` | Namespace |
| `TEMPORAL_TASK_QUEUE` | `openfoundry.pipeline` | Task queue polled by this worker |
| `OF_PIPELINE_AUTHORING_URL` | `http://pipeline-authoring-service:50080` | Base URL used by `BuildPipeline` for `GET /pipelines/{id}` and `POST /pipelines/_compile` |
| `OF_PIPELINE_BUILD_URL` | `http://pipeline-build-service:50081` | Base URL used by `ExecutePipeline` for `POST /pipelines/{id}/runs` |
| `OF_PIPELINE_BEARER_TOKEN` | _(empty)_ | Optional service bearer token forwarded to pipeline services |

`PIPELINE_AUTHORING_SERVICE_URL`, `PIPELINE_BUILD_SERVICE_URL`,
`OF_PIPELINE_BUILD_GRPC_ADDR`, and `OF_PIPELINE_EXEC_GRPC_ADDR` are
accepted as rollout fallbacks while manifests converge on the HTTP env
names.
