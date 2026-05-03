//! Composite log sink writes to Postgres AND fan-outs to broadcast
//! subscribers in lockstep.

mod common;

use std::sync::Arc;

use chrono::Utc;
use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::logs::{
    BroadcastLogSink, CompositeLogSink, LogEntry, LogLevel, LogSink, PostgresLogSink,
};

use crate::common::{job_spec, MockDatasetClient, MockJobSpecRepo, spawn};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn composite_sink_persists_and_broadcasts() {
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
    let resolved = resolve_build(
        &harness.pool,
        ResolveBuildArgs {
            pipeline_rid: "ri.p.logs",
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

    let job_rid: String = sqlx::query_scalar(
        "SELECT rid FROM jobs WHERE build_id = $1",
    )
    .bind(resolved.build_id)
    .fetch_one(&harness.pool)
    .await
    .unwrap();

    let postgres_sink: Arc<dyn LogSink> = Arc::new(PostgresLogSink::new(harness.pool.clone()));
    let broadcaster = Arc::new(BroadcastLogSink::new());
    let composite = CompositeLogSink::new(postgres_sink, broadcaster.clone());

    let mut rx = broadcaster.subscribe(&job_rid).await;

    composite
        .emit(LogEntry {
            sequence: 0,
            job_rid: job_rid.clone(),
            ts: Utc::now(),
            level: LogLevel::Info,
            message: "hello".into(),
            params: Some(serde_json::json!({"k": "v"})),
        })
        .await
        .expect("emit succeeds");

    let received = tokio::time::timeout(std::time::Duration::from_secs(2), rx.recv())
        .await
        .expect("broadcast within 2s")
        .expect("broadcast value");
    assert_eq!(received.message, "hello");
    assert_eq!(received.level, LogLevel::Info);
    // Composite stamps the persisted sequence onto the broadcast
    // entry — must be > 0.
    assert!(received.sequence > 0);

    // Persisted row exists with same sequence.
    let row: (i64, String, String) = sqlx::query_as(
        "SELECT sequence, level, message FROM job_logs WHERE message = $1",
    )
    .bind("hello")
    .fetch_one(&harness.pool)
    .await
    .unwrap();
    assert_eq!(row.0, received.sequence);
    assert_eq!(row.1, "INFO");
    assert_eq!(row.2, "hello");
}
