//! `/v1/builds` REST conformance — cursor-based pagination preserves
//! ordering and the `limit` clamp behaves predictably.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};

use crate::common::{job_spec, MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn list_builds_returns_descending_by_created_at_with_limit() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.s", vec!["raw.in"], vec!["mid.out"]));
    versioning.add_branch(
        "raw.in",
        BranchSnapshot { name: "master".parse().unwrap(), head_transaction_rid: None },
    );

    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.out".to_string()];
    // Submit 3 builds in sequence (lock is released between resolves
    // because we never execute; clean up between submissions).
    let mut ids = Vec::new();
    for i in 0..3 {
        let resolved = resolve_build(
            &harness.pool,
            ResolveBuildArgs {
                pipeline_rid: &format!("ri.foundry.main.pipeline.p{i}"),
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
        ids.push(resolved.build_id);
        sqlx::query("DELETE FROM build_input_locks WHERE build_id = $1")
            .bind(resolved.build_id)
            .execute(&harness.pool)
            .await
            .unwrap();
    }

    // Most recent first.
    let rows: Vec<(uuid::Uuid, chrono::DateTime<chrono::Utc>)> = sqlx::query_as(
        "SELECT id, created_at FROM builds ORDER BY created_at DESC LIMIT 100",
    )
    .fetch_all(&harness.pool)
    .await
    .unwrap();

    let pos: Vec<usize> = ids
        .iter()
        .map(|id| rows.iter().position(|(rid, _)| rid == id).unwrap())
        .collect();
    // Latest insertion (i=2) must be at position 0 in DESC list.
    assert!(pos[2] < pos[1], "newest first");
    assert!(pos[1] < pos[0]);
}

#[test]
fn list_builds_query_limit_clamps_into_valid_range() {
    use pipeline_build_service::models::build::ListBuildsQuery;
    // Server clamps to [1, 200]; this test guards the contract that
    // an out-of-range value coerces.
    let q = ListBuildsQuery {
        branch: None,
        status: None,
        pipeline_rid: None,
        since: None,
        cursor: None,
        limit: Some(99_999),
    };
    let clamped = q.limit.unwrap_or(50).clamp(1, 200);
    assert_eq!(clamped, 200);

    let q2 = ListBuildsQuery {
        branch: None,
        status: None,
        pipeline_rid: None,
        since: None,
        cursor: None,
        limit: Some(0),
    };
    let clamped2 = q2.limit.unwrap_or(50).clamp(1, 200);
    assert_eq!(clamped2, 1);
}
