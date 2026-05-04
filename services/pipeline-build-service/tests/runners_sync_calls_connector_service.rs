//! SYNC runner forwards the job to `connector-management-service`'s
//! `POST /api/v1/data-integration/syncs/{id}/run` endpoint.

use std::sync::Arc;

use pipeline_build_service::domain::build_executor::{JobContext, JobOutcome, JobRunner};
use pipeline_build_service::domain::build_resolution::JobSpec;
use pipeline_build_service::domain::runners::{SyncJobRunner, logic_kinds};
use serde_json::json;
use uuid::Uuid;
use wiremock::matchers::{method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sync_runner_calls_connector_service_run_endpoint() {
    let mock = MockServer::start().await;
    let sync_id = Uuid::now_v7();
    let ingest_id = Uuid::now_v7();

    Mock::given(method("POST"))
        .and(path(format!(
            "/api/v1/data-integration/syncs/{sync_id}/run"
        )))
        .respond_with(
            ResponseTemplate::new(202).set_body_json(json!({ "ingest_job_id": ingest_id })),
        )
        .expect(1)
        .mount(&mock)
        .await;

    let runner = SyncJobRunner::new(mock.uri(), reqwest::Client::new());
    let runner: Arc<dyn JobRunner> = Arc::new(runner);

    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.sync".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: vec!["ri.out".into()],
            logic_kind: logic_kinds::SYNC.into(),
            logic_payload: json!({
                "source_rid": "ri.foundry.main.connector.demo",
                "sync_def_id": sync_id,
            }),
            content_hash: "h".into(),
        },
        resolved_inputs: vec![],
        force_build: false,
        log_sink: None,
    };
    let outcome = runner.run(&ctx).await;
    match outcome {
        JobOutcome::Completed {
            output_content_hash,
        } => {
            assert!(output_content_hash.starts_with("sync:"));
            assert!(output_content_hash.contains(&ingest_id.to_string()));
        }
        other => panic!("unexpected outcome: {other:?}"),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn sync_runner_fails_on_connector_error() {
    let mock = MockServer::start().await;
    let sync_id = Uuid::now_v7();
    Mock::given(method("POST"))
        .and(path(format!(
            "/api/v1/data-integration/syncs/{sync_id}/run"
        )))
        .respond_with(ResponseTemplate::new(502).set_body_string("source unreachable"))
        .mount(&mock)
        .await;

    let runner = SyncJobRunner::new(mock.uri(), reqwest::Client::new());
    let runner: Arc<dyn JobRunner> = Arc::new(runner);
    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.sync".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: vec!["ri.out".into()],
            logic_kind: logic_kinds::SYNC.into(),
            logic_payload: json!({
                "source_rid": "ri.foundry.main.connector.demo",
                "sync_def_id": sync_id,
            }),
            content_hash: "h".into(),
        },
        resolved_inputs: vec![],
        force_build: false,
        log_sink: None,
    };
    let outcome = runner.run(&ctx).await;
    match outcome {
        JobOutcome::Failed { reason } => {
            assert!(reason.contains("502"), "{reason}");
        }
        other => panic!("expected Failed, got {other:?}"),
    }
}
