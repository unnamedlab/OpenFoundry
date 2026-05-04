//! E2E coverage for the Rust Temporal adapter and the real Go worker.

#![cfg(feature = "it-temporal")]

use std::{path::PathBuf, sync::Arc, time::Duration};

use temporal_client::{
    AutomationRunInput, GrpcWorkflowClient, Namespace, RuntimeClientConfig,
    WorkflowAutomationClient, WorkflowClient,
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

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn e2e_workflow_execute_through_go_worker() {
    let harness = boot_temporal().await;
    let namespace = Namespace::new(harness.namespace.clone());
    let mut worker = GoWorker::spawn(
        repo_root(),
        "workflow-automation",
        &harness.frontend,
        &harness.namespace,
    )
    .await;

    let grpc = GrpcWorkflowClient::connect(RuntimeClientConfig {
        host_port: Some(harness.frontend.clone()),
        namespace: harness.namespace.clone(),
        identity: "workflow-automation-service-e2e".to_string(),
        api_key: None,
        require_real_client: false,
        deployment_environment: None,
    })
    .await
    .expect("Temporal gRPC client");
    let client = WorkflowAutomationClient::new(
        Arc::new(grpc.clone()) as Arc<dyn WorkflowClient>,
        namespace.clone(),
    );

    let run_id = Uuid::now_v7();
    let input = AutomationRunInput {
        run_id,
        definition_id: Uuid::now_v7(),
        tenant_id: "tenant-e2e".into(),
        triggered_by: "temporal-e2e".into(),
        trigger_payload: serde_json::json!({"trigger": "rust-test"}),
    };

    let handle = client
        .start_run(run_id, input, Uuid::now_v7())
        .await
        .expect("workflow start");

    let result = match tokio::time::timeout(
        Duration::from_secs(120),
        grpc.workflow_result_json(&namespace, &handle.workflow_id, &handle.run_id),
    )
    .await
    {
        Ok(result) => result.expect("workflow result"),
        Err(error) => {
            let logs = worker.logs().unwrap_or_default();
            panic!("workflow completion timeout: {error}\nworker logs:\n{logs}");
        }
    };

    assert_eq!(result["run_id"], run_id.to_string());
    assert_eq!(result["status"], "completed");

    worker.stop().await;
}
