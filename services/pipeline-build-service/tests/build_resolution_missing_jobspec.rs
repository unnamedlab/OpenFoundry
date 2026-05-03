//! When the JobSpec repo has no spec for a declared output, resolution
//! must fail with `MissingJobSpec` and the tried branch chain.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BuildResolutionError, ResolveBuildArgs, resolve_build,
};

use crate::common::{MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test]
#[ignore = "requires docker"]
async fn build_resolution_missing_jobspec_lists_tried_branches() {
    let harness = spawn().await;
    let specs = MockJobSpecRepo::default(); // empty — repo will return None
    let versioning = MockDatasetClient::default();

    let build_branch: BranchName = "feature".parse().unwrap();
    let fallback = vec!["develop".to_string(), "master".to_string()];
    let outputs = vec!["ri.foundry.main.dataset.unknown".to_string()];

    let result = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.x",
            build_branch: &build_branch,
            job_spec_fallback: &fallback,
            output_dataset_rids: &outputs,
            force_build: false,
            requested_by: "tester",
            trigger_kind: "MANUAL",
            abort_policy: "DEPENDENT_ONLY",
        },
        &specs,
        &versioning,
    )
    .await;

    match result {
        Err(BuildResolutionError::MissingJobSpec { dataset_rid, tried }) => {
            assert_eq!(dataset_rid, "ri.foundry.main.dataset.unknown");
            assert_eq!(tried, vec!["feature", "develop", "master"]);
        }
        other => panic!("expected MissingJobSpec, got {other:?}"),
    }
}
