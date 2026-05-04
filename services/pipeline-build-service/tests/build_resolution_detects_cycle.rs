//! Build resolution must reject cyclic JobSpec graphs (Foundry doc:
//! "Detects cycles in the specified input datasets and fails the
//! build if there are cycles present").

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, BuildResolutionError, ResolveBuildArgs, resolve_build,
};

use crate::common::{MockDatasetClient, MockJobSpecRepo, job_spec, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn build_resolution_detects_cycle_returns_cycle_path() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    // s1: a → b ; s2: b → a (cycle)
    specs.add(job_spec("ri.spec.s1", vec!["a"], vec!["b"]));
    specs.add(job_spec("ri.spec.s2", vec!["b"], vec!["a"]));
    versioning.add_branch(
        "a",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );
    versioning.add_branch(
        "b",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["a".to_string(), "b".to_string()];
    let result = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.cyclic",
            build_branch: &build_branch,
            job_spec_fallback: &[],
            output_dataset_rids: &outputs,
            force_build: false,
            requested_by: "tester",
            trigger_kind: "MANUAL",
            abort_policy: "DEPENDENT_ONLY",
        },
        &specs,
        &versioning,
    )
    .await;

    match result {
        Err(BuildResolutionError::CycleDetected { cycle_path }) => {
            assert!(!cycle_path.is_empty());
        }
        other => panic!("expected CycleDetected, got {other:?}"),
    }

    // No build row should have been persisted (the cycle is detected
    // before the BUILD_RESOLUTION insert).
    let count: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM builds")
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    assert_eq!(count.0, 0);
}
