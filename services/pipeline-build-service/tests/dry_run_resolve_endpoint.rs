//! P2 — `compile_build_graph + branch_resolution` round-trip via the
//! in-memory repo path used by the `dry-run-resolve` endpoint.
//!
//! The full HTTP round-trip lives in the Playwright spec
//! `apps/web/tests/e2e/pipeline-builder-fallback-chain-editor.spec.ts`.
//! Here we validate the *resolution payload shape* the endpoint
//! produces by exercising the underlying domain modules with the same
//! inputs the handler would receive.

use pipeline_build_service::domain::build_resolution::{InputSpec, JobSpec};
use pipeline_build_service::domain::job_graph::{InMemoryJobSpecRepo, compile_build_graph};

fn job_spec(rid: &str, branch: &str, inputs: &[&str], outputs: &[&str]) -> JobSpec {
    JobSpec {
        rid: rid.to_string(),
        pipeline_rid: "ri.foundry.main.pipeline.dry-run".to_string(),
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
        content_hash: "cafebabe".to_string(),
    }
}

#[tokio::test]
async fn dry_run_compiles_graph_with_inline_specs() {
    let mut repo = InMemoryJobSpecRepo::new();
    repo.insert(job_spec(
        "ri.foundry.main.jobspec.B",
        "feature",
        &["ri.foundry.main.dataset.A"],
        &["ri.foundry.main.dataset.B"],
    ));

    let graph = compile_build_graph(
        "ri.foundry.main.pipeline.dry-run",
        "feature",
        &["ri.foundry.main.dataset.B".to_string()],
        &["master".to_string()],
        &repo,
    )
    .await
    .expect("compile");

    assert_eq!(graph.nodes.len(), 1);
    let node = &graph.nodes[0];
    assert_eq!(node.job_spec.rid, "ri.foundry.main.jobspec.B");
    assert_eq!(node.job_spec.branch_name, "feature");

    // The endpoint's response shape mirrors `node.job_spec.inputs`
    // verbatim — verify the fallback chain came through as declared.
    assert_eq!(
        node.job_spec.inputs[0].fallback_chain,
        vec!["master".to_string()]
    );
}

#[tokio::test]
async fn dry_run_returns_missing_spec_outcome_for_unknown_dataset() {
    let repo = InMemoryJobSpecRepo::new();
    let err = compile_build_graph(
        "ri.foundry.main.pipeline.dry-run",
        "feature",
        &["ri.foundry.main.dataset.never-published".to_string()],
        &[],
        &repo,
    )
    .await
    .expect_err("missing spec");

    let formatted = format!("{err}");
    assert!(formatted.contains("missing JobSpec"));
}
