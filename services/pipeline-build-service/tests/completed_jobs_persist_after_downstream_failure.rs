//! Foundry doc § Job execution: "Note that if a job in a build
//! fails, previously completed jobs may still have written data to
//! their output datasets."
//!
//! Validates that an early-completed job's output transactions remain
//! committed even when a later sibling job fails.

mod common;

use core_models::dataset::transaction::BranchName;
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use pipeline_build_service::domain::build_executor::{
    ExecuteBuildArgs, OutputTransactionClient, execute_build,
};
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;

use crate::common::{
    MockDatasetClient, MockJobRunner, MockJobSpecRepo, MockOutputClient, RunnerScript, arc_runner,
    job_spec, spawn,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
#[ignore = "requires docker"]
async fn completed_jobs_persist_after_downstream_failure() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    // a → b. a completes, b fails.
    specs.add(job_spec("ri.spec.a", vec!["raw.x"], vec!["mid.a"]));
    specs.add(job_spec("ri.spec.b", vec!["mid.a"], vec!["out.b"]));
    versioning.add_branch(
        "raw.x",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.a".into(), "out.b".into()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.persist",
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
    .expect("resolution");

    let runner = MockJobRunner::default();
    // a finishes quickly, b fails after a small delay.
    runner.add("ri.spec.a", RunnerScript::ok("hash-a"));
    runner.add(
        "ri.spec.b",
        RunnerScript::fail("downstream boom").with_sleep(Duration::from_millis(50)),
    );

    let output_client = Arc::new(MockOutputClient::default());
    let arc_output: Arc<dyn OutputTransactionClient> = output_client.clone();

    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved.build_id,
            parallelism: 2,
            runner: arc_runner(runner),
            output_client: arc_output,
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
    assert_eq!(outcome.completed, 1);
    assert_eq!(outcome.failed, 1);

    // a's outputs must be `committed = TRUE` (Foundry: previously
    // completed jobs may still have written data); b's must be
    // `aborted = TRUE`.
    let rows: Vec<(String, String, bool, bool)> = sqlx::query_as(
        r#"SELECT j.job_spec_rid, jo.output_dataset_rid, jo.committed, jo.aborted
             FROM job_outputs jo
             JOIN jobs j ON j.id = jo.job_id
            WHERE j.build_id = $1
            ORDER BY j.job_spec_rid, jo.output_dataset_rid"#,
    )
    .bind(resolved.build_id)
    .fetch_all(&harness.pool)
    .await
    .unwrap();

    for (spec_rid, output, committed, aborted) in &rows {
        match spec_rid.as_str() {
            "ri.spec.a" => {
                assert!(committed, "a's {output} stays committed");
                assert!(!aborted);
            }
            "ri.spec.b" => {
                assert!(!committed, "b's {output} did not commit");
                assert!(aborted);
            }
            other => panic!("unexpected spec {other}"),
        }
    }

    // The mock output client should also have committed a's output
    // before b failed.
    let commits = output_client.commits.lock().unwrap().clone();
    assert!(
        commits.iter().any(|(rid, _)| rid == "mid.a"),
        "mid.a must have been committed; got {commits:?}"
    );
}
