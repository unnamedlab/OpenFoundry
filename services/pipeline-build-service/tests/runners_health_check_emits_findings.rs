//! HEALTH_CHECK runner POSTs the finding to dataset-quality-service.

use std::sync::Arc;

use pipeline_build_service::domain::build_executor::{JobContext, JobOutcome, JobRunner};
use pipeline_build_service::domain::build_resolution::JobSpec;
use pipeline_build_service::domain::runners::{HealthCheckJobRunner, logic_kinds};
use serde_json::json;
use uuid::Uuid;
use wiremock::matchers::{body_partial_json, method, path};
use wiremock::{Mock, MockServer, ResponseTemplate};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn health_check_runner_posts_finding_to_quality_service() {
    let mock = MockServer::start().await;
    let target_rid = "ri.foundry.main.dataset.target";

    Mock::given(method("POST"))
        .and(path(format!(
            "/api/v1/datasets/{target_rid}/health-checks/results"
        )))
        .and(body_partial_json(json!({
            "passed": true,
            "check_kind": "ROW_COUNT_NONZERO",
        })))
        .respond_with(ResponseTemplate::new(201))
        .expect(1)
        .mount(&mock)
        .await;

    let runner = HealthCheckJobRunner::new(mock.uri(), reqwest::Client::new());
    let runner: Arc<dyn JobRunner> = Arc::new(runner);

    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.hc".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: vec![target_rid.into()],
            logic_kind: logic_kinds::HEALTH_CHECK.into(),
            logic_payload: json!({
                "check_kind": "ROW_COUNT_NONZERO",
                "target_dataset_rid": target_rid,
                "params": { "expect_passed": true }
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
            assert!(
                output_content_hash.contains("RowCountNonzero")
                    && output_content_hash.contains("true"),
                "unexpected hash: {output_content_hash}"
            );
        }
        other => panic!("expected Completed, got {other:?}"),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn health_check_runner_rejects_target_mismatch() {
    let runner = HealthCheckJobRunner::new("http://nope.invalid".into(), reqwest::Client::new());
    let runner: Arc<dyn JobRunner> = Arc::new(runner);

    let ctx = JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.hc".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: vec!["ri.out.different".into()],
            logic_kind: logic_kinds::HEALTH_CHECK.into(),
            logic_payload: json!({
                "check_kind": "FRESHNESS_SLA",
                "target_dataset_rid": "ri.out.expected",
                "params": {},
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
            assert!(
                reason.contains("not present in JobSpec outputs"),
                "{reason}"
            );
        }
        other => panic!("expected Failed, got {other:?}"),
    }
}
