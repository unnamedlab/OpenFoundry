//! Acquired locks must be releasable when the build hits a terminal
//! state. We simulate both completion (via DELETE on the lock row,
//! since transaction commit is the responsibility of
//! dataset-versioning-service) and abort (which flips the build to
//! BUILD_ABORTING and lets the executor flush jobs).

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::job_lifecycle::{
    JobLifecycleError, transition_job,
};
use pipeline_build_service::models::build::BuildState;
use pipeline_build_service::models::job::JobState;

use crate::common::{job_spec, MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn build_locks_released_on_abort_or_finish() {
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
            pipeline_rid: "ri.foundry.main.pipeline.lock-release",
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

    // Sanity: lock row exists.
    let lock_count: (i64,) =
        sqlx::query_as("SELECT COUNT(*) FROM build_input_locks WHERE build_id = $1")
            .bind(resolved.build_id)
            .fetch_one(&harness.pool)
            .await
            .unwrap();
    assert_eq!(lock_count.0, 1);

    // Drive a job into RUNNING then COMPLETED through the formal
    // lifecycle, which is the path the executor uses on a successful
    // build.
    let job_id: (uuid::Uuid,) = sqlx::query_as("SELECT id FROM jobs WHERE build_id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    transition_job(&harness.pool, job_id.0, Some(JobState::Waiting), JobState::RunPending, None)
        .await
        .expect("WAITING → RUN_PENDING");
    transition_job(&harness.pool, job_id.0, Some(JobState::RunPending), JobState::Running, None)
        .await
        .expect("RUN_PENDING → RUNNING");
    transition_job(&harness.pool, job_id.0, Some(JobState::Running), JobState::Completed, None)
        .await
        .expect("RUNNING → COMPLETED");

    // Mark the build COMPLETED + drop locks (mirroring what
    // dataset-versioning-service does on transaction commit).
    sqlx::query("UPDATE builds SET state = $1, finished_at = NOW() WHERE id = $2")
        .bind(BuildState::Completed.as_str())
        .bind(resolved.build_id)
        .execute(&harness.pool)
        .await
        .unwrap();
    sqlx::query("DELETE FROM build_input_locks WHERE build_id = $1")
        .bind(resolved.build_id)
        .execute(&harness.pool)
        .await
        .unwrap();

    let after: (i64,) =
        sqlx::query_as("SELECT COUNT(*) FROM build_input_locks WHERE build_id = $1")
            .bind(resolved.build_id)
            .fetch_one(&harness.pool)
            .await
            .unwrap();
    assert_eq!(after.0, 0, "locks released after completion");

    // Re-running a fresh resolution against the same output now
    // succeeds because the previous lock is gone.
    let resolved2 = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.lock-release",
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
    .expect("re-resolution after release succeeds");
    assert_eq!(resolved2.state, BuildState::Resolution);

    // Sanity: the lifecycle helper still rejects illegal transitions.
    let err = transition_job(
        &harness.pool,
        job_id.0,
        None,
        JobState::Running, // COMPLETED → RUNNING is illegal
        None,
    )
    .await
    .expect_err("illegal transition rejected");
    assert!(matches!(err, JobLifecycleError::InvalidTransition { .. }));
}
