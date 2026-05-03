//! ANALYTICAL runner consumes an object-set query and emits a
//! deterministic `output_content_hash` derived from
//! (query, ontology_rid, output_schema). The hash is the contract the
//! staleness check relies on.

use std::sync::Arc;

use pipeline_build_service::domain::build_executor::{JobContext, JobOutcome, JobRunner};
use pipeline_build_service::domain::build_resolution::JobSpec;
use pipeline_build_service::domain::runners::{logic_kinds, AnalyticalJobRunner};
use serde_json::json;
use uuid::Uuid;

fn ctx_with(payload: serde_json::Value, outputs: Vec<&str>) -> JobContext {
    JobContext {
        build_id: Uuid::nil(),
        build_branch: "master".into(),
        job_id: Uuid::now_v7(),
        job_spec: JobSpec {
            rid: "ri.spec.an".into(),
            pipeline_rid: "ri.p".into(),
            branch_name: "master".into(),
            inputs: vec![],
            output_dataset_rids: outputs.into_iter().map(String::from).collect(),
            logic_kind: logic_kinds::ANALYTICAL.into(),
            logic_payload: payload,
            content_hash: "h".into(),
        },
        resolved_inputs: vec![],
        force_build: false,
        log_sink: None,
    }
}

#[tokio::test]
async fn analytical_runner_emits_deterministic_hash() {
    let runner = AnalyticalJobRunner::default();
    let runner: Arc<dyn JobRunner> = Arc::new(runner);

    let payload = json!({
        "object_set_query": { "filter": "owner = 'team_a'" },
        "ontology_rid": "ri.ontology.main",
        "output_schema": { "columns": ["id", "owner"] },
    });
    let outcome1 = runner.run(&ctx_with(payload.clone(), vec!["ri.out"])).await;
    let outcome2 = runner.run(&ctx_with(payload, vec!["ri.out"])).await;

    let (h1, h2) = match (outcome1, outcome2) {
        (JobOutcome::Completed { output_content_hash: a }, JobOutcome::Completed { output_content_hash: b }) => (a, b),
        other => panic!("unexpected outcomes: {other:?}"),
    };
    assert_eq!(h1, h2, "same payload must produce same hash");
}

#[tokio::test]
async fn analytical_runner_rejects_multi_output() {
    let runner = AnalyticalJobRunner::default();
    let runner: Arc<dyn JobRunner> = Arc::new(runner);
    let outcome = runner
        .run(&ctx_with(
            json!({"object_set_query": {}}),
            vec!["a", "b"],
        ))
        .await;
    match outcome {
        JobOutcome::Failed { reason } => {
            assert!(reason.contains("exactly one output"), "{reason}");
        }
        other => panic!("expected Failed, got {other:?}"),
    }
}
