//! Resolution must (a) open one transaction per output and (b) commit
//! a `build_input_locks` row per output dataset.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;

use crate::common::{MockDatasetClient, MockJobSpecRepo, job_spec, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn build_resolution_acquires_locks_via_open_transaction() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.s1", vec!["raw.a"], vec!["mid.b"]));
    versioning.add_branch(
        "raw.a",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.b".to_string()];

    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.lock",
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
    .await
    .expect("resolution succeeds");
    assert_eq!(resolved.state, BuildState::Resolution);
    assert_eq!(resolved.opened_transactions.len(), 1);

    let lock: (String, String) = sqlx::query_as(
        "SELECT output_dataset_rid, transaction_rid FROM build_input_locks WHERE build_id = $1",
    )
    .bind(resolved.build_id)
    .fetch_one(&harness.pool)
    .await
    .unwrap();
    assert_eq!(lock.0, "mid.b");
    assert!(lock.1.starts_with("ri.foundry.main.transaction."));

    // Job row + WAITING audit transition.
    let job_count: (i64,) = sqlx::query_as("SELECT COUNT(*) FROM jobs WHERE build_id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    assert_eq!(job_count.0, 1);
    let transition_count: (i64,) = sqlx::query_as(
        "SELECT COUNT(*) FROM job_state_transitions t JOIN jobs j ON j.id = t.job_id WHERE j.build_id = $1",
    )
    .bind(resolved.build_id)
    .fetch_one(&harness.pool)
    .await
    .unwrap();
    assert_eq!(
        transition_count.0, 1,
        "exactly one initial WAITING transition logged"
    );
}
