//! P2 — Foundry guarantee: "A build never creates branches on input
//! datasets." When a JobSpec only consumes a dataset (never produces
//! it), the run guarantees layer rejects any
//! `branch.create`-equivalent call against that dataset.

use core_models::dataset::transaction::DatasetRid;
use pipeline_build_service::domain::build_resolution::{InputSpec, JobSpec};
use pipeline_build_service::domain::job_graph::{InMemoryJobSpecRepo, compile_build_graph};
use pipeline_build_service::domain::run_guarantees::{
    GuaranteeError, assert_input_dataset_not_branched,
};

fn rid(suffix: &str) -> DatasetRid {
    format!("ri.foundry.main.dataset.{suffix}").parse().unwrap()
}

fn job_spec(rid: &str, branch: &str, inputs: &[&str], outputs: &[&str]) -> JobSpec {
    JobSpec {
        rid: rid.to_string(),
        pipeline_rid: "ri.foundry.main.pipeline.guarantees".to_string(),
        branch_name: branch.to_string(),
        inputs: inputs
            .iter()
            .map(|d| InputSpec {
                dataset_rid: d.to_string(),
                fallback_chain: vec!["master".into()],
                view_filter: vec![],
                require_fresh: false,
            })
            .collect(),
        output_dataset_rids: outputs.iter().map(|s| s.to_string()).collect(),
        logic_kind: "sql".to_string(),
        logic_payload: serde_json::Value::Null,
        content_hash: "feedface".to_string(),
    }
}

#[tokio::test]
async fn rejects_branch_creation_on_input_only_dataset() {
    // Graph: B = transform(A); C = transform(B). A is input-only.
    let mut repo = InMemoryJobSpecRepo::new();
    repo.insert(job_spec(
        "ri.foundry.main.jobspec.B",
        "feature",
        &["ri.foundry.main.dataset.A"],
        &["ri.foundry.main.dataset.B"],
    ))
    .insert(job_spec(
        "ri.foundry.main.jobspec.C",
        "feature",
        &["ri.foundry.main.dataset.B"],
        &["ri.foundry.main.dataset.C"],
    ));

    let graph = compile_build_graph(
        "ri.foundry.main.pipeline.guarantees",
        "feature",
        &[
            "ri.foundry.main.dataset.B".to_string(),
            "ri.foundry.main.dataset.C".to_string(),
        ],
        &[],
        &repo,
    )
    .await
    .expect("compile");

    // Branching B (an output) is allowed.
    assert!(
        assert_input_dataset_not_branched(
            "ri.foundry.main.build.42",
            &graph,
            &[rid("B")],
        )
        .is_ok()
    );

    // Branching A (input-only) must fail.
    let err = assert_input_dataset_not_branched(
        "ri.foundry.main.build.42",
        &graph,
        &[rid("A")],
    )
    .expect_err("must reject branching on input-only dataset");
    match err {
        GuaranteeError::InputDatasetBranched {
            build_id,
            dataset_rid,
        } => {
            assert_eq!(build_id, "ri.foundry.main.build.42");
            assert_eq!(dataset_rid, rid("A"));
        }
        other => panic!("expected InputDatasetBranched, got {other:?}"),
    }
}
