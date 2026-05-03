//! `view_filter = AT_TIMESTAMP` survives the resolver and is
//! persisted into `jobs.input_view_resolutions` so the runner can
//! replay the exact view the orchestrator saw.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, InputSpec, JobSpec, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::runners::ViewFilter;
use serde_json::Value;

use crate::common::{MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn input_view_filter_at_timestamp_persists_resolution() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    let spec = JobSpec {
        rid: "ri.spec.ts".into(),
        pipeline_rid: "ri.p".into(),
        branch_name: "master".into(),
        inputs: vec![InputSpec {
            dataset_rid: "ri.in".into(),
            fallback_chain: vec!["master".into()],
            view_filter: vec![ViewFilter::AtTimestamp {
                value: "2026-04-01T00:00:00Z".into(),
            }],
            require_fresh: false,
        }],
        output_dataset_rids: vec!["ri.out".into()],
        logic_kind: "TRANSFORM".into(),
        logic_payload: Value::Null,
        content_hash: "h".into(),
    };
    specs.add(spec.clone());
    versioning.add_branch(
        "ri.in",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["ri.out".to_string()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.p",
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

    let resolutions: Value = sqlx::query_scalar(
        "SELECT input_view_resolutions FROM jobs WHERE build_id = $1",
    )
    .bind(resolved.build_id)
    .fetch_one(&harness.pool)
    .await
    .unwrap();

    let arr = resolutions.as_array().expect("array");
    assert_eq!(arr.len(), 1);
    let entry = &arr[0];
    assert_eq!(entry["dataset_rid"], "ri.in");
    assert_eq!(entry["filter"]["kind"], "AT_TIMESTAMP");
    assert_eq!(entry["filter"]["value"], "2026-04-01T00:00:00Z");
    assert_eq!(entry["branch"], "master");
}
