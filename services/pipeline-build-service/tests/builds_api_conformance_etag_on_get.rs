//! `/v1/builds/{rid}` GET should be cache-friendly: identical
//! responses for the same lifecycle state should produce identical
//! payload digests so callers can compute deterministic ETags
//! client-side. This test pins the contract: the JSON serialization
//! of `BuildEnvelope` is stable for stable inputs.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::models::build::{Build, BuildEnvelope};
use pipeline_build_service::models::job::Job;
use sha2::{Digest, Sha256};

use crate::common::{MockDatasetClient, MockJobSpecRepo, job_spec, spawn};

fn etag(value: &serde_json::Value) -> String {
    let canonical = serde_json::to_string(value).unwrap();
    let mut hasher = Sha256::new();
    hasher.update(canonical.as_bytes());
    format!("\"{}\"", hex_encode(&hasher.finalize()))
}

fn hex_encode(bytes: &[u8]) -> String {
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push_str(&format!("{b:02x}"));
    }
    s
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn build_envelope_serialisation_yields_stable_etag() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.e", vec!["raw.in"], vec!["mid.out"]));
    versioning.add_branch(
        "raw.in",
        BranchSnapshot {
            name: "master".parse().unwrap(),
            head_transaction_rid: None,
        },
    );
    let build_branch: BranchName = "master".parse().unwrap();
    let outputs = vec!["mid.out".to_string()];
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.foundry.main.pipeline.etag",
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

    let build: Build = sqlx::query_as("SELECT * FROM builds WHERE id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    let jobs: Vec<Job> =
        sqlx::query_as("SELECT * FROM jobs WHERE build_id = $1 ORDER BY created_at")
            .bind(resolved.build_id)
            .fetch_all(&harness.pool)
            .await
            .unwrap();

    let env1 = BuildEnvelope {
        build: build.clone(),
        jobs: jobs.clone(),
    };
    let env2 = BuildEnvelope { build, jobs };

    let etag1 = etag(&serde_json::to_value(&env1).unwrap());
    let etag2 = etag(&serde_json::to_value(&env2).unwrap());
    assert_eq!(etag1, etag2, "identical envelope must hash identically");
}
