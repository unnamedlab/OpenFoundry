# `pipeline` worker

Task queue: `openfoundry.pipeline`. Hosts `PipelineRun` workflow
invoked by both ad-hoc requests (`pipeline-authoring-service`) and
Temporal Schedules created by `pipeline-schedule-service` (S2.4).
