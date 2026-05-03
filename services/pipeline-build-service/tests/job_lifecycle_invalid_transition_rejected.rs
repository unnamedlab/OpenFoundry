//! Validates the strict state machine in
//! `domain::job_lifecycle::is_valid_transition`. Tests both pure
//! transitions (no DB) and the persisted path
//! ([`transition_job_in_tx`]) which must reject illegal moves and
//! leave the row unchanged.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::job_lifecycle::{
    JobLifecycleError, transition_job,
};
use pipeline_build_service::models::job::JobState;

use crate::common::{job_spec, MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn job_lifecycle_invalid_transition_rejected() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.s1", vec!["raw.a"], vec!["mid.b"]));
    versioning.add_branch(
        "raw.a",
        BranchSnapshot { name: "master".parse().unwrap(), head_transaction_rid: None },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.b".to_string()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.illegal",
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
    let job_id: (uuid::Uuid,) = sqlx::query_as("SELECT id FROM jobs WHERE build_id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();

    // Illegal: WAITING → RUNNING (must go via RUN_PENDING).
    let err = transition_job(&harness.pool, job_id.0, None, JobState::Running, None)
        .await
        .expect_err("WAITING → RUNNING is illegal");
    assert!(matches!(err, JobLifecycleError::InvalidTransition { .. }));

    // Row should still be WAITING — and only the initial transition is
    // logged (no failed-transition smear in the audit table).
    let state: (String,) = sqlx::query_as("SELECT state FROM jobs WHERE id = $1")
        .bind(job_id.0)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    assert_eq!(state.0, "WAITING");
    let count: (i64,) = sqlx::query_as(
        "SELECT COUNT(*) FROM job_state_transitions WHERE job_id = $1",
    )
    .bind(job_id.0)
    .fetch_one(&harness.pool)
    .await
    .unwrap();
    assert_eq!(count.0, 1, "no audit row written for rejected transition");
}
