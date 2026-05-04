//! E2E schedule idempotency against Temporal with two real pipeline workers.

#![cfg(feature = "it-temporal")]

use std::{path::PathBuf, sync::Arc};

use temporal_client::{
    GrpcWorkflowClient, Namespace, PipelineRunInput, PipelineScheduleClient, RuntimeClientConfig,
    WorkflowClient,
};
use testing::{go_workers::GoWorker, temporal::boot_temporal};
use uuid::Uuid;

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(2)
        .expect("repo root from service manifest")
        .to_path_buf()
}

fn run_input() -> PipelineRunInput {
    PipelineRunInput {
        pipeline_id: Uuid::now_v7(),
        tenant_id: "tenant-e2e".to_string(),
        revision: None,
        parameters: serde_json::json!({"source": "temporal-e2e"}),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn create_schedule_is_idempotent_with_two_go_workers() {
    let harness = boot_temporal().await;
    let repo_root = repo_root();
    let mut worker_a = GoWorker::spawn(
        &repo_root,
        "pipeline",
        &harness.frontend,
        &harness.namespace,
    )
    .await;
    let mut worker_b = GoWorker::spawn(
        &repo_root,
        "pipeline",
        &harness.frontend,
        &harness.namespace,
    )
    .await;

    let namespace = Namespace::new(harness.namespace.clone());
    let grpc = GrpcWorkflowClient::connect(RuntimeClientConfig {
        host_port: Some(harness.frontend.clone()),
        namespace: harness.namespace.clone(),
        identity: "pipeline-schedule-service-e2e".to_string(),
        api_key: None,
        require_real_client: false,
        deployment_environment: None,
    })
    .await
    .expect("Temporal gRPC client");
    let shared: Arc<dyn WorkflowClient> = Arc::new(grpc);
    let replica_a = PipelineScheduleClient::new(shared.clone(), namespace.clone());
    let replica_b = PipelineScheduleClient::new(shared, namespace);

    let schedule_id = format!("pipeline-idempotency-{}", Uuid::now_v7());
    let cron = vec!["0 0 1 1 *".to_string()];

    replica_a
        .create(
            schedule_id.clone(),
            cron.clone(),
            Some("UTC".to_string()),
            run_input(),
            Uuid::now_v7(),
        )
        .await
        .expect("replica A schedule create");

    replica_b
        .create(
            schedule_id.clone(),
            cron,
            Some("UTC".to_string()),
            run_input(),
            Uuid::now_v7(),
        )
        .await
        .expect("replica B duplicate schedule create");

    replica_a
        .delete(&schedule_id)
        .await
        .expect("schedule cleanup");
    worker_a.stop().await;
    worker_b.stop().await;
}
