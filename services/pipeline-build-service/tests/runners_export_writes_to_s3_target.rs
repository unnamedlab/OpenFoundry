//! EXPORT runner pushes a manifest to the configured target. We
//! validate the contract against a wiremock S3-flavoured endpoint —
//! the runner is HTTP-based, so any S3 / GCS / JDBC connector that
//! exposes a webhook fits the same shape. (The minio testcontainer
//! variant lives in `connector-management-service` integration
//! tests; here we only validate the runner's outbound payload.)

use std::sync::Arc;

use pipeline_build_service::domain::build_executor::{JobContext, JobOutcome, JobRunner};
use pipeline_build_service::domain::build_resolution::JobSpec;
use pipeline_build_service::domain::runners::{logic_kinds, ExportJobRunner};
use serde_json::json;
use uuid::Uuid;
use wiremock::matchers::{body_partial_json, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn export_runner_posts_manifest_to_s3_endpoint() {
    let mock = MockServer::start().await;
    Mock::given(method("POST"))
        .and(path("/exports/upload"))
        .and(body_partial_json(json!({
            "export_target": "S3",
            "acl_alias": "marketing-bucket",
        })))
        .respond_with(ResponseTemplate::new(202))
        .expect(1)
        .mount(&mock)
        .await;

    let runner = ExportJobRunner::new(reqwest::Client::new());
    let runner: Arc<dyn JobRunner> = Arc::new(runner);

    let endpoint = format!("{}/exports/upload", mock.uri());
    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.export".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            // EXPORT may have zero outputs.
            output_dataset_rids: vec![],
            logic_kind: logic_kinds::EXPORT.into(),
            logic_payload: json!({
                "export_target": "S3",
                "endpoint": endpoint,
                "options": {"region": "us-east-1", "prefix": "ds/"},
                "source_dataset_rid": "ri.foundry.main.dataset.src",
                "acl_alias": "marketing-bucket",
            }),
            content_hash: "h".into(),
        },
        resolved_inputs: vec![],
        force_build: false,
        log_sink: None,
    };
    let outcome = runner.run(&ctx).await;
    match outcome {
        JobOutcome::Completed { output_content_hash } => {
            assert!(!output_content_hash.is_empty());
        }
        other => panic!("expected Completed, got {other:?}"),
    }
}

#[tokio::test]
async fn export_runner_refuses_when_acl_alias_missing() {
    let runner = ExportJobRunner::new(reqwest::Client::new());
    let runner: Arc<dyn JobRunner> = Arc::new(runner);
    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.export".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: vec![],
            logic_kind: logic_kinds::EXPORT.into(),
            logic_payload: json!({
                "export_target": "HTTP",
                "endpoint": "http://nope.invalid",
                "source_dataset_rid": "ri.src",
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
            assert!(reason.contains("acl_alias"), "{reason}");
        }
        other => panic!("expected Failed, got {other:?}"),
    }
}
