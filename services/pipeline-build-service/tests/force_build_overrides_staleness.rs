//! Foundry doc § Staleness: "you can run a force build, which
//! recomputes all datasets as part of the build, regardless of
//! whether they are already up-to-date."

mod common;

use core_models::dataset::transaction::BranchName;
use std::collections::HashMap;

use pipeline_build_service::domain::build_executor::{ExecuteBuildArgs, execute_build};
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;

use crate::common::{
    MockDatasetClient, MockJobRunner, MockJobSpecRepo, MockOutputClient, RunnerScript, arc_output,
    arc_runner, job_spec, spawn,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn force_build_overrides_staleness() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.f", vec!["raw.in"], vec!["out.f"]));
    versioning.add_branch(
        "raw.in",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["out.f".into()];

    // First build — establishes the staleness baseline.
    let r1 = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.force",
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
    .expect("first resolution");
    let runner1 = MockJobRunner::default();
    runner1.add("ri.spec.f", RunnerScript::ok("hash-1"));
    let _ = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: r1.build_id,
            parallelism: 1,
            runner: arc_runner(runner1),
            output_client: arc_output(MockOutputClient::default()),
            job_inputs: HashMap::from([("ri.spec.f".to_string(), r1.input_views.clone())]),
            job_specs: r1.job_specs.clone(),
        },
    )
    .await
    .expect("first build");

    // Second build with `force_build = true` — must execute, NOT
    // mark stale_skipped, and refresh output_content_hash.
    let r2 = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.force",
            build_branch: &build_branch,
            job_spec_fallback: &[],
            output_dataset_rids: &outputs,
            force_build: true,
            requested_by: "tester",
            trigger_kind: "FORCE",
            abort_policy: "DEPENDENT_ONLY",
        },
        &specs,
        &versioning,
    )
    .await
    .expect("force resolution");

    let runner2 = MockJobRunner::default();
    runner2.add("ri.spec.f", RunnerScript::ok("hash-2"));
    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: r2.build_id,
            parallelism: 1,
            runner: arc_runner(runner2),
            output_client: arc_output(MockOutputClient::default()),
            job_inputs: HashMap::from([("ri.spec.f".to_string(), r2.input_views.clone())]),
            job_specs: r2.job_specs.clone(),
        },
    )
    .await
    .expect("force build");

    assert_eq!(outcome.final_state, BuildState::Completed);
    assert_eq!(
        outcome.stale_skipped, 0,
        "force_build skips the staleness check"
    );

    let job: (bool, Option<String>) =
        sqlx::query_as("SELECT stale_skipped, output_content_hash FROM jobs WHERE build_id = $1")
            .bind(r2.build_id)
            .fetch_one(&harness.pool)
            .await
            .unwrap();
    assert!(!job.0, "stale_skipped is false on force build");
    assert_eq!(
        job.1.as_deref(),
        Some("hash-2"),
        "force build wrote a fresh hash"
    );
}
