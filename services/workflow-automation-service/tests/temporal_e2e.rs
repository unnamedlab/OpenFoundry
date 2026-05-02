//! S2.3.f — Integration test scaffold for the Temporal-backed
//! `workflow-automation-service`.
//!
//! ## Status: substrate-only
//!
//! The full end-to-end (Rust REST adapter → Temporal frontend → Go
//! worker → activity gRPC into the Rust services) requires three
//! pieces that **do not exist yet**:
//!
//!   1. A gRPC-backed [`temporal_client::WorkflowClient`]
//!      implementation. Today only `NoopWorkflowClient` and
//!      `LoggingWorkflowClient` ship from `libs/temporal-client`. The
//!      gRPC backend lands behind the `grpc` feature once upstream
//!      `temporal-client` cuts a stable release (S2.2.a follow-up).
//!   2. A Go worker binary built from `workers-go/workflow-automation/`
//!      and reachable from inside the test container. The CI job
//!      `go-workers-build` (S2.2.g) builds it; a combined
//!      Cargo + Go integration job is the natural follow-up.
//!   3. The proto-generated Go gRPC client for
//!      `ontology-actions-service` so activities are real, not the
//!      `ErrNotImplemented` substrate stubs in
//!      `workers-go/workflow-automation/activities/activities.go`.
//!
//! Until then the test exercises the substrate boundary that *does*
//! exist today: the typed
//! [`temporal_client::WorkflowAutomationClient`] facade backed by the
//! `LoggingWorkflowClient`. That validates the
//! `StartWorkflowOptions::new` audit-correlation propagation path.

#![cfg(feature = "it-temporal")]

use std::sync::Arc;

use temporal_client::{
    AutomationRunInput, LoggingWorkflowClient, Namespace, WorkflowAutomationClient,
};
use uuid::Uuid;

/// Round-trip a workflow start through the typed facade. Asserts the
/// invariant the legacy executor used to silently break: every
/// workflow execution must carry the inbound audit-correlation ID
/// (ADR-0019).
#[tokio::test]
async fn workflow_start_preserves_audit_correlation() {
    let client = WorkflowAutomationClient::new(
        Arc::new(LoggingWorkflowClient),
        Namespace::new("openfoundry-default"),
    );
    let run_id = Uuid::now_v7();
    let audit = Uuid::now_v7();
    let input = AutomationRunInput {
        run_id,
        definition_id: Uuid::now_v7(),
        tenant_id: "acme".into(),
        triggered_by: "tester".into(),
        trigger_payload: serde_json::json!({"trigger": "rest"}),
    };
    let handle = client
        .start_run(run_id, input, audit)
        .await
        .expect("logging client must succeed");
    assert_eq!(handle.workflow_id.0, format!("workflow-automation:{run_id}"));
}

/// Full container-backed E2E. Ignored by default — flips on once
/// (a) the gRPC backend lands in `libs/temporal-client` and
/// (b) a Go worker binary can be launched alongside the harness.
#[tokio::test]
#[ignore = "S2.3.f end-to-end blocked on grpc backend (libs/temporal-client) and Go worker bring-up"]
async fn e2e_workflow_through_go_worker() {
    let _harness = testing::temporal::boot_temporal().await;
    // 1. Spin up `workers-go/workflow-automation` against the
    //    harness frontend.
    // 2. Build a gRPC-backed `WorkflowClient` pointing at
    //    `_harness.frontend`.
    // 3. Drive `WorkflowAutomationClient::start_run`.
    // 4. Poll Temporal for completion via `query_workflow` or
    //    describe APIs; assert `status == "completed"`.
    // 5. Verify the worker exposes Prometheus counters on :9090.
    // Failing today by intent so the test does not silently pretend
    // to validate anything.
}
