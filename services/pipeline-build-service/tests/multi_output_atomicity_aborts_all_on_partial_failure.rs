//! Multi-output invariant (Foundry doc): "if a job defines multiple
//! output datasets, they will always update together and it is not
//! possible to build only some of the datasets without running the
//! full job."
//!
//! When the executor fails to commit one of N outputs, the remaining
//! N-1 must be aborted and the job marked FAILED.

mod common;

use core_models::dataset::transaction::BranchName;
use std::collections::HashMap;
use std::sync::Arc;

use pipeline_build_service::domain::build_executor::{ExecuteBuildArgs, execute_build};
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, InputSpec, JobSpec, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::BuildState;

use crate::common::{
    MockDatasetClient, MockJobRunner, MockJobSpecRepo, MockOutputClient, RunnerScript, arc_output,
    arc_runner, spawn,
};

fn multi_output_spec() -> JobSpec {
    JobSpec {
        rid: "ri.spec.multi".into(),
        pipeline_rid: "ri.foundry.main.pipeline.multi".into(),
        branch_name: "master".into(),
        inputs: vec![InputSpec {
            dataset_rid: "raw.in".into(),
            fallback_chain: vec!["master".into()],
            view_filter: vec![],
            require_fresh: false,
        }],
        output_dataset_rids: vec!["out.alpha".into(), "out.beta".into()],
        logic_kind: "TRANSFORM".into(),
        logic_payload: serde_json::Value::Null,
        content_hash: "hash-multi".into(),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn multi_output_atomicity_aborts_all_on_partial_failure() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(multi_output_spec());
    versioning.add_branch(
        "raw.in",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["out.alpha".into(), "out.beta".into()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.multi",
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
    runner.add("ri.spec.multi", RunnerScript::ok("hash-multi"));
    let output_client = MockOutputClient::default();
    output_client.fail_commit_for("out.beta");
    let arc_output_client: Arc<MockOutputClient> = Arc::new(output_client);
    let arc_output = arc_output_client.clone()
        as Arc<dyn pipeline_build_service::domain::build_executor::OutputTransactionClient>;

    let outcome = execute_build(
        &harness.pool,
        ExecuteBuildArgs {
            build_id: resolved.build_id,
            parallelism: 2,
            runner: arc_runner(runner),
            output_client: arc_output,
            job_inputs: HashMap::from([(
                "ri.spec.multi".to_string(),
                resolved.input_views.clone(),
            )]),
            job_specs: resolved.job_specs.clone(),
        },
    )
    .await
    .expect("execute_build");

    assert_eq!(outcome.final_state, BuildState::Failed);
    assert_eq!(
        outcome.failed, 1,
        "the multi-output job fails on partial commit"
    );

    // Both outputs must be `aborted = TRUE` (multi-output atomicity).
    let outputs_state: Vec<(String, bool, bool)> = sqlx::query_as(
        "SELECT output_dataset_rid, committed, aborted FROM job_outputs
            WHERE job_id = (SELECT id FROM jobs WHERE build_id = $1)
            ORDER BY output_dataset_rid",
    )
    .bind(resolved.build_id)
    .fetch_all(&harness.pool)
    .await
    .unwrap();
    assert_eq!(outputs_state.len(), 2);
    for (rid, committed, aborted) in &outputs_state {
        assert!(!committed, "no output should remain committed in {rid}");
        assert!(aborted, "every output should be aborted in {rid}");
    }

    // The mock client should have observed an abort for the
    // un-committed output. (`out.alpha` got committed before the
    // executor noticed `out.beta` failed; that one is left in its
    // committed state at the mock layer — Foundry doesn't roll back
    // committed transactions.)
    let aborted = arc_output_client.aborts.lock().unwrap().clone();
    assert!(
        aborted.iter().any(|(rid, _)| rid == "out.beta"),
        "the failing output must be aborted; got {aborted:?}"
    );
}
