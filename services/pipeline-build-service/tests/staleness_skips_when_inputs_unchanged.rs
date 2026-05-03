//! Foundry doc § Staleness: a fresh output dataset (logic + inputs
//! unchanged since last build) is not recomputed. The new job is
//! marked `stale_skipped = TRUE`, transitioned straight to COMPLETED,
//! and its output transactions are aborted (no view change).

mod common;

use core_models::dataset::transaction::BranchName;
use std::collections::HashMap;

use pipeline_build_service::domain::build_executor::{
    execute_build, ExecuteBuildArgs,
};
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;

use crate::common::{
    arc_output, arc_runner, job_spec, MockDatasetClient, MockJobRunner, MockJobSpecRepo,
    MockOutputClient, RunnerScript, spawn,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn staleness_skips_when_inputs_unchanged() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.s", vec!["raw.in"], vec!["out.s"]));
    versioning.add_branch(
        "raw.in",
        BranchSnapshot { name: "master".parse().unwrap(), head_transaction_rid: None },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["out.s".into()];

    // First build — runs normally and writes the canonical hash.
    let resolved1 = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.stale",
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
    .expect("resolution 1");
    let runner1 = MockJobRunner::default();
    runner1.add("ri.spec.s", RunnerScript::ok("hash-out-s"));
    let _ = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved1.build_id,
            parallelism: 1,
            runner: arc_runner(runner1),
            output_client: arc_output(MockOutputClient::default()),
            job_inputs: HashMap::from([("ri.spec.s".to_string(), resolved1.input_views.clone())]),
            job_specs: resolved1.job_specs.clone(),
        },
    )
    .await
    .expect("first build runs");

    // Second build — same logic, same inputs. Should skip.
    let resolved2 = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.stale",
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
    .expect("resolution 2");

    let runner2 = MockJobRunner::default();
    runner2.add("ri.spec.s", RunnerScript::fail("must not be invoked"));
    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved2.build_id,
            parallelism: 1,
            runner: arc_runner(runner2),
            output_client: arc_output(MockOutputClient::default()),
            job_inputs: HashMap::from([("ri.spec.s".to_string(), resolved2.input_views.clone())]),
            job_specs: resolved2.job_specs.clone(),
        },
    )
    .await
    .expect("second build runs");

    assert_eq!(outcome.final_state, BuildState::Completed);
    assert_eq!(outcome.stale_skipped, 1);

    let job: (String, bool, Option<String>) = sqlx::query_as(
        "SELECT state, stale_skipped, output_content_hash FROM jobs WHERE build_id = $1",
    )
    .bind(resolved2.build_id)
    .fetch_one(&harness.pool)
    .await
    .unwrap();
    assert_eq!(job.0, "COMPLETED");
    assert!(job.1, "stale_skipped must be true");
    assert_eq!(
        job.2.as_deref(),
        Some("hash-out-s"),
        "previous output_content_hash propagated forward"
    );
}
