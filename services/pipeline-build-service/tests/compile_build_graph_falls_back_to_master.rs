//! P2 — `compile_build_graph` walks the JobSpec fallback chain.
//!
//! Replays the doc example from
//! /docs/foundry/data-integration/branching § "Example: Building on
//! branches": datasets A, B, C; the JobSpec for B/C lives on `master`,
//! the build runs on `feature` with fallback `feature → master`.
//!
//! Because compilation is pure (no DB, no HTTP), this test runs in the
//! default `cargo test` pass — no Docker required.

use pipeline_build_service::domain::build_resolution::{InputSpec, JobSpec};
use pipeline_build_service::domain::job_graph::{InMemoryJobSpecRepo, compile_build_graph};

fn job_spec(rid: &str, branch: &str, inputs: &[&str], outputs: &[&str]) -> JobSpec {
    JobSpec {
        rid: rid.to_string(),
        pipeline_rid: "ri.foundry.main.pipeline.demo".to_string(),
        branch_name: branch.to_string(),
        inputs: inputs
            .iter()
            .map(|d| InputSpec {
                dataset_rid: d.to_string(),
                fallback_chain: vec!["master".to_string()],
                view_filter: vec![],
                require_fresh: false,
            })
            .collect(),
        output_dataset_rids: outputs.iter().map(|s| s.to_string()).collect(),
        logic_kind: "sql".to_string(),
        logic_payload: serde_json::Value::Null,
        content_hash: "deadbeef".to_string(),
    }
}

#[tokio::test]
async fn build_on_feature_falls_back_to_master_jobspecs() {
    let mut repo = InMemoryJobSpecRepo::new();
    // master publishes specs for B and C; feature has none.
    repo.insert(job_spec(
        "ri.foundry.main.jobspec.B",
        "master",
        &["ri.foundry.main.dataset.A"],
        &["ri.foundry.main.dataset.B"],
    ))
    .insert(job_spec(
        "ri.foundry.main.jobspec.C",
        "master",
        &["ri.foundry.main.dataset.B"],
        &["ri.foundry.main.dataset.C"],
    ));

    let graph = compile_build_graph(
        "ri.foundry.main.pipeline.demo",
        "feature",
        &[
            "ri.foundry.main.dataset.B".to_string(),
            "ri.foundry.main.dataset.C".to_string(),
        ],
        &["master".to_string()],
        &repo,
    )
    .await
    .expect("compile_build_graph");

    // Two nodes, ordered B then C (B has no upstream producer in this
    // graph; C consumes B).
    assert_eq!(graph.nodes.len(), 2);
    assert_eq!(graph.nodes[0].job_spec.rid, "ri.foundry.main.jobspec.B");
    assert_eq!(graph.nodes[1].job_spec.rid, "ri.foundry.main.jobspec.C");
    // Both specs were resolved on `master` (the fallback), not `feature`.
    assert!(
        graph
            .nodes
            .iter()
            .all(|n| n.job_spec.branch_name == "master")
    );
}

#[tokio::test]
async fn build_prefers_feature_jobspec_when_present() {
    let mut repo = InMemoryJobSpecRepo::new();
    // Feature publishes its own spec for B (overrides master).
    repo.insert(job_spec(
        "ri.foundry.main.jobspec.B/feature",
        "feature",
        &["ri.foundry.main.dataset.A"],
        &["ri.foundry.main.dataset.B"],
    ))
    .insert(job_spec(
        "ri.foundry.main.jobspec.B/master",
        "master",
        &["ri.foundry.main.dataset.A"],
        &["ri.foundry.main.dataset.B"],
    ));

    let graph = compile_build_graph(
        "ri.foundry.main.pipeline.demo",
        "feature",
        &["ri.foundry.main.dataset.B".to_string()],
        &["master".to_string()],
        &repo,
    )
    .await
    .expect("compile_build_graph");

    assert_eq!(graph.nodes.len(), 1);
    assert_eq!(
        graph.nodes[0].job_spec.rid,
        "ri.foundry.main.jobspec.B/feature"
    );
}
