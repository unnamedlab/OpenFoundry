//! Three independent jobs must run in parallel (Foundry doc § Job
//! execution: "Jobs that do not depend on each other are run in
//! parallel.").

mod common;

use core_models::dataset::transaction::BranchName;
use std::collections::HashMap;
use std::time::{Duration, Instant};

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
async fn parallel_execution_independent_jobs() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    // Three jobs, each with its own input dataset and output. No
    // dependencies between them — should fan out to 3 parallel slots.
    for letter in ["a", "b", "c"] {
        let input = format!("raw.{letter}");
        specs.add(job_spec(
            &format!("ri.spec.{letter}"),
            vec![&input],
            vec![&format!("out.{letter}")],
        ));
        versioning.add_branch(
            &input,
            BranchSnapshot {
                name: "master".parse().unwrap(),
                head_transaction_rid: None,
            },
        );
    }

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["out.a".into(), "out.b".into(), "out.c".into()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.parallel",
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

    // Each job sleeps 200ms; parallel execution should finish in
    // roughly 200ms (single slot would take ~600ms).
    let runner = MockJobRunner::default();
    for letter in ["a", "b", "c"] {
        runner.add(
            &format!("ri.spec.{letter}"),
            RunnerScript::ok(&format!("hash-{letter}")).with_sleep(Duration::from_millis(200)),
        );
    }
    let output = MockOutputClient::default();

    let mut job_inputs = HashMap::new();
    for spec in &resolved.job_specs {
        job_inputs.insert(spec.rid.clone(), resolved.input_views.clone());
    }

    let start = Instant::now();
    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved.build_id,
            parallelism: 4,
            runner: arc_runner(runner),
            output_client: arc_output(output),
            job_inputs,
            job_specs: resolved.job_specs.clone(),
        },
    )
    .await
    .expect("execute_build");
    let elapsed = start.elapsed();

    assert_eq!(outcome.final_state, BuildState::Completed);
    assert_eq!(outcome.completed, 3);
    assert!(
        elapsed < Duration::from_millis(550),
        "parallel run should be < ~550ms, got {elapsed:?}"
    );
}
