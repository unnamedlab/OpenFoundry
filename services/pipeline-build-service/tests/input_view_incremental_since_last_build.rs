//! `INCREMENTAL_SINCE_LAST_BUILD` walks the history: the first build
//! resolves with no lower bound; the second build's resolver sees
//! the first build's resolution row and uses it.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, InputSpec, JobSpec, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::runners::ViewFilter;
use serde_json::Value;

use crate::common::{MockDatasetClient, MockJobSpecRepo, spawn};

fn incremental_spec() -> JobSpec {
    JobSpec {
        rid: "ri.spec.inc".into(),
        pipeline_rid: "ri.p.inc".into(),
        branch_name: "master".into(),
        inputs: vec![InputSpec {
            dataset_rid: "ri.in.inc".into(),
            fallback_chain: vec!["master".into()],
            view_filter: vec![ViewFilter::IncrementalSinceLastBuild],
            require_fresh: false,
        }],
        output_dataset_rids: vec!["ri.out.inc".into()],
        logic_kind: "TRANSFORM".into(),
        logic_payload: Value::Null,
        content_hash: "h".into(),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn first_incremental_build_has_no_lower_bound() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(incremental_spec());
    versioning.add_branch(
        "ri.in.inc",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["ri.out.inc".to_string()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.p.inc",
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

    let entries: Value =
        sqlx::query_scalar("SELECT input_view_resolutions FROM jobs WHERE build_id = $1")
            .bind(resolved.build_id)
            .fetch_one(&harness.pool)
            .await
            .unwrap();
    let entry = &entries.as_array().unwrap()[0];
    assert_eq!(entry["filter"]["kind"], "INCREMENTAL_SINCE_LAST_BUILD");
    assert!(
        entry.get("range_from_transaction_rid").is_none(),
        "no prior build, no lower bound: {entry}"
    );
    assert_eq!(
        entry["note"].as_str().unwrap_or(""),
        "incremental: no prior build, runner will read full view"
    );
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn second_incremental_build_inherits_lower_bound_from_prior_completed_job() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(incremental_spec());
    versioning.add_branch(
        "ri.in.inc",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["ri.out.inc".to_string()];
    let first = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.p.inc",
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

    // Mark the first job COMPLETED with a synthetic upper bound so
    // the second resolver's incremental lookup has a lower bound to
    // pick up.
    let job_id: (uuid::Uuid,) = sqlx::query_as("SELECT id FROM jobs WHERE build_id = $1")
        .bind(first.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    let synthetic = serde_json::json!([{
        "dataset_rid": "ri.in.inc",
        "branch": "master",
        "filter": { "kind": "INCREMENTAL_SINCE_LAST_BUILD" },
        "range_to_transaction_rid": "ri.foundry.main.transaction.upper-bound-1"
    }]);
    sqlx::query(
        "UPDATE jobs SET state = 'COMPLETED', state_changed_at = NOW(),
                          input_view_resolutions = $2
            WHERE id = $1",
    )
    .bind(job_id.0)
    .bind(&synthetic)
    .execute(&harness.pool)
    .await
    .unwrap();
    sqlx::query("UPDATE builds SET state = 'BUILD_COMPLETED' WHERE id = $1")
        .bind(first.build_id)
        .execute(&harness.pool)
        .await
        .unwrap();
    sqlx::query("DELETE FROM build_input_locks WHERE build_id = $1")
        .bind(first.build_id)
        .execute(&harness.pool)
        .await
        .unwrap();

    let second = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.p.inc",
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
    .expect("second resolution");

    let entries: Value =
        sqlx::query_scalar("SELECT input_view_resolutions FROM jobs WHERE build_id = $1")
            .bind(second.build_id)
            .fetch_one(&harness.pool)
            .await
            .unwrap();
    let entry = &entries.as_array().unwrap()[0];
    assert_eq!(
        entry["range_from_transaction_rid"].as_str(),
        Some("ri.foundry.main.transaction.upper-bound-1"),
        "second build inherits prior upper bound as new lower bound: {entry}"
    );
}
