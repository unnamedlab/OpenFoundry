//! When another build holds a lock on a dataset that feeds our inputs,
//! resolution must transition us to BUILD_QUEUED instead of acquiring
//! locks.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;
use uuid::Uuid;

use crate::common::{job_spec, MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn build_queued_when_upstream_in_progress() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();

    // Our build wants to produce `mid.b` from `raw.a`. The "upstream"
    // build (already running) is producing `raw.a` from somewhere.
    specs.add(job_spec("ri.spec.s1", vec!["raw.a"], vec!["mid.b"]));
    versioning.add_branch(
        "raw.a",
        BranchSnapshot { name: "master".parse().unwrap(), head_transaction_rid: None },
    );

    // Plant an upstream build that has a lock on `raw.a`.
    let upstream_build = Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO builds (id, pipeline_rid, build_branch, state, trigger_kind, requested_by)
              VALUES ($1, 'ri.upstream', 'master', 'BUILD_RUNNING', 'MANUAL', 'system')"#,
    )
    .bind(upstream_build)
    .execute(&harness.pool)
    .await
    .unwrap();
    sqlx::query(
        "INSERT INTO build_input_locks (output_dataset_rid, build_id, transaction_rid)
            VALUES ('raw.a', $1, 'ri.foundry.main.transaction.upstream')",
    )
    .bind(upstream_build)
    .execute(&harness.pool)
    .await
    .unwrap();

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.b".to_string()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.queued",
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
    .expect("resolution must not fail; it should queue");
    assert_eq!(resolved.state, BuildState::Queued);
    assert!(resolved.queued_reason.is_some());

    let row_state: (String,) = sqlx::query_as("SELECT state FROM builds WHERE id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    assert_eq!(row_state.0, "BUILD_QUEUED");
}
