//! DEPENDENT_ONLY abort policy: failed job aborts only its
//! transitive dependents, leaving independent jobs to run.

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

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn failure_cascade_dependent_only() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    // a → b ; c independent.
    specs.add(job_spec("ri.spec.a", vec!["raw.x"], vec!["mid.a"]));
    specs.add(job_spec("ri.spec.b", vec!["mid.a"], vec!["out.b"]));
    specs.add(job_spec("ri.spec.c", vec!["raw.y"], vec!["out.c"]));
    versioning.add_branch("raw.x", BranchSnapshot { name: "master".parse().unwrap(), head_transaction_rid: None });
    versioning.add_branch("raw.y", BranchSnapshot { name: "master".parse().unwrap(), head_transaction_rid: None });

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.a".into(), "out.b".into(), "out.c".into()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.cascade-dep",
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

    let runner = MockJobRunner::default();
    runner.add("ri.spec.a", RunnerScript::fail("boom"));
    runner.add("ri.spec.b", RunnerScript::ok("hash-b"));
    runner.add("ri.spec.c", RunnerScript::ok("hash-c"));

    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved.build_id,
            parallelism: 3,
            runner: arc_runner(runner),
            output_client: arc_output(MockOutputClient::default()),
            job_inputs: resolved
                .job_specs
                .iter()
                .map(|s| (s.rid.clone(), vec![]))
                .collect::<HashMap<_, _>>(),
            job_specs: resolved.job_specs.clone(),
        },
    )
    .await
    .expect("execute_build");

    assert_eq!(outcome.final_state, BuildState::Failed);
    assert_eq!(outcome.completed, 1, "c finishes independently");
    assert_eq!(outcome.failed, 1, "a fails");
    assert_eq!(outcome.aborted, 1, "b cascaded");

    // Verify states in DB.
    let states: Vec<(String, String)> = sqlx::query_as(
        "SELECT job_spec_rid, state FROM jobs WHERE build_id = $1 ORDER BY job_spec_rid",
    )
    .bind(resolved.build_id)
    .fetch_all(&harness.pool)
    .await
    .unwrap();
    let map: HashMap<String, String> = states.into_iter().collect();
    assert_eq!(map["ri.spec.a"], "FAILED");
    assert_eq!(map["ri.spec.b"], "ABORTED");
    assert_eq!(map["ri.spec.c"], "COMPLETED");
}
