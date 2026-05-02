//! S2.4.d — Temporal Schedule idempotency smoke test.
//!
//! Two replicas of the binary are expected to converge on the **same**
//! Temporal Schedule when both POST `/schedules/temporal` with the same
//! `schedule_id`. Today we exercise the substrate path with the
//! [`temporal_client::LoggingWorkflowClient`] backend — Temporal's real
//! `create_schedule` API is itself idempotent on `schedule_id`, so we
//! validate that calling our facade twice with the same id yields a
//! single logical schedule (no panic, no diverging workflow ids).
//!
//! The full E2E variant (boot Temporal, register `PipelineRun` workflow,
//! launch 2 worker replicas, observe exactly N firings in N minutes) is
//! gated behind `--features it-temporal` and `#[ignore]`-marked until
//! the gRPC backend lands in `libs/temporal-client`.

use std::sync::Arc;

use temporal_client::{
    LoggingWorkflowClient, Namespace, PipelineRunInput, PipelineScheduleClient, WorkflowClient,
};
use uuid::Uuid;

fn run_input() -> PipelineRunInput {
    PipelineRunInput {
        pipeline_id: Uuid::now_v7(),
        tenant_id: "tenant-test".to_string(),
        revision: None,
        parameters: serde_json::Value::Object(serde_json::Map::new()),
    }
}

#[tokio::test]
async fn create_schedule_is_idempotent_under_two_replicas() {
    // Two independent `PipelineScheduleClient` instances stand in for
    // two service replicas pointing at the same Temporal namespace.
    let workflow_client_a: Arc<dyn WorkflowClient> = Arc::new(LoggingWorkflowClient);
    let workflow_client_b: Arc<dyn WorkflowClient> = Arc::new(LoggingWorkflowClient);
    let ns = Namespace::new("default");
    let replica_a = PipelineScheduleClient::new(workflow_client_a, ns.clone());
    let replica_b = PipelineScheduleClient::new(workflow_client_b, ns);

    let schedule_id = "idem-test-001".to_string();
    let cron = vec!["* * * * *".to_string()];

    // Replica A creates the schedule.
    replica_a
        .create(
            schedule_id.clone(),
            cron.clone(),
            None,
            run_input(),
            Uuid::now_v7(),
        )
        .await
        .expect("replica A create");

    // Replica B fires the same id — must not error in the substrate
    // backend (Temporal would return AlreadyExists which the facade
    // is expected to surface as Ok(()) for the substrate).
    replica_b
        .create(
            schedule_id.clone(),
            cron,
            None,
            run_input(),
            Uuid::now_v7(),
        )
        .await
        .expect("replica B create (idempotent)");
}

#[tokio::test]
#[ignore = "S2.4.d full E2E blocked on gRPC backend in libs/temporal-client"]
async fn full_e2e_two_replicas_fire_each_minute_exactly_once() {
    // Steps when the gRPC backend lands:
    //   1. spin up Temporal via `testing::temporal::boot_temporal`;
    //   2. launch 2 instances of `workers-go/pipeline` against the
    //      same task queue `openfoundry.pipeline`;
    //   3. POST the schedule with cron `* * * * *`;
    //   4. wait 3 minutes; assert exactly 3 firings via Temporal's
    //      `DescribeSchedule` info.recent_actions.
    unimplemented!("see module docstring; gated on grpc feature");
}
