//! ALL_NON_DEPENDENT abort policy: a single failure tears down every
//! still-pending job, including those independent of the failure.

mod common;

use core_models::dataset::transaction::BranchName;
use std::collections::HashMap;
use std::time::Duration;

use pipeline_build_service::domain::build_executor::{ExecuteBuildArgs, execute_build};
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;

use crate::common::{
    MockDatasetClient, MockJobRunner, MockJobSpecRepo, MockOutputClient, RunnerScript, arc_output,
    arc_runner, job_spec, spawn,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn failure_cascade_all_non_dependent() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.a", vec!["raw.x"], vec!["mid.a"]));
    specs.add(job_spec("ri.spec.c", vec!["raw.y"], vec!["out.c"]));
    versioning.add_branch(
        "raw.x",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );
    versioning.add_branch(
        "raw.y",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.a".into(), "out.c".into()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.cascade-all",
            build_branch: &build_branch,
            job_spec_fallback: &[],
            output_dataset_rids: &outputs,
            force_build: false,
            requested_by: "tester",
            trigger_kind: "MANUAL",
            abort_policy: "ALL_NON_DEPENDENT",
        },
        &specs,
        &versioning,
    )
    .await
    .expect("resolution succeeds");

    // a fails fast; c sleeps long enough that the orchestrator
    // observes a's failure first and gets to abort c before it runs.
    let runner = MockJobRunner::default();
    runner.add("ri.spec.a", RunnerScript::fail("boom"));
    runner.add(
        "ri.spec.c",
        RunnerScript::ok("hash-c").with_sleep(Duration::from_millis(500)),
    );

    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved.build_id,
            parallelism: 1, // Force serialization so a runs first.
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
    assert_eq!(outcome.failed, 1);
    assert_eq!(outcome.aborted, 1, "ALL_NON_DEPENDENT aborts c too");

    let states: Vec<(String, String)> = sqlx::query_as(
        "SELECT job_spec_rid, state FROM jobs WHERE build_id = $1 ORDER BY job_spec_rid",
    )
    .bind(resolved.build_id)
    .fetch_all(&harness.pool)
    .await
    .unwrap();
    let map: HashMap<String, String> = states.into_iter().collect();
    assert_eq!(map["ri.spec.a"], "FAILED");
    assert_eq!(map["ri.spec.c"], "ABORTED");
}
