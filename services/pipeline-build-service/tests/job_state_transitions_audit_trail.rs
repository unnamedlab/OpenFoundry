//! `job_state_transitions` is an append-only audit trail. Every
//! successful transition through the lifecycle must be logged with
//! the correct (from, to) pair.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::job_lifecycle::transition_job;
use pipeline_build_service::models::job::JobState;

use crate::common::{MockDatasetClient, MockJobSpecRepo, job_spec, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn job_state_transitions_audit_trail_complete() {
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
            pipeline_rid: "ri.foundry.main.pipeline.audit",
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
    let (job_id,): (uuid::Uuid,) = sqlx::query_as("SELECT id FROM jobs WHERE build_id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();

    // Drive the job through the canonical happy path.
    transition_job(
        &harness.pool,
        job_id,
        Some(JobState::Waiting),
        JobState::RunPending,
        Some("dispatched"),
    )
    .await
    .unwrap();
    transition_job(
        &harness.pool,
        job_id,
        Some(JobState::RunPending),
        JobState::Running,
        None,
    )
    .await
    .unwrap();
    transition_job(
        &harness.pool,
        job_id,
        Some(JobState::Running),
        JobState::Completed,
        Some("ok"),
    )
    .await
    .unwrap();

    // The audit table should show: initial NULL → WAITING (from
    // resolve_build), plus the three transitions above.
    let rows: Vec<(Option<String>, String)> = sqlx::query_as(
        "SELECT from_state, to_state FROM job_state_transitions
         WHERE job_id = $1 ORDER BY occurred_at ASC, id ASC",
    )
    .bind(job_id)
    .fetch_all(&harness.pool)
    .await
    .unwrap();

    assert_eq!(rows.len(), 4, "{rows:?}");
    assert_eq!(rows[0], (None, "WAITING".to_string()));
    assert_eq!(rows[1], (Some("WAITING".into()), "RUN_PENDING".into()));
    assert_eq!(rows[2], (Some("RUN_PENDING".into()), "RUNNING".into()));
    assert_eq!(rows[3], (Some("RUNNING".into()), "COMPLETED".into()));
}
