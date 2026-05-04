//! After a disconnect the client reconnects with
//! `from_sequence=last_sequence_seen+1`. The REST history query
//! powering the SSE catch-up phase must respect that filter so the
//! stream doesn't replay entries the client already consumed.

mod common;

use core_models::dataset::transaction::BranchName;
use pipeline_build_service::domain::build_resolution::{
    BranchSnapshot, ResolveBuildArgs, resolve_build,
};
use pipeline_build_service::domain::logs::{LogEntry, LogLevel, LogSink, PostgresLogSink};

use crate::common::{MockDatasetClient, MockJobSpecRepo, job_spec, spawn};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires docker"]
async fn rest_history_only_returns_entries_at_or_after_from_sequence() {
    let harness = spawn().await;
    let versioning = MockDatasetClient::default();
    let specs = MockJobSpecRepo::default();
    specs.add(job_spec("ri.spec.s", vec!["raw.in"], vec!["mid.out"]));
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
            pipeline_rid: "ri.p.resume",
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
    let job_rid: String = sqlx::query_scalar("SELECT rid FROM jobs WHERE build_id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();
    let job_id: uuid::Uuid = sqlx::query_scalar("SELECT id FROM jobs WHERE build_id = $1")
        .bind(resolved.build_id)
        .fetch_one(&harness.pool)
        .await
        .unwrap();

    // Persist five entries.
    let sink = PostgresLogSink::new(harness.pool.clone());
    let mut sequences = Vec::new();
    for i in 0..5 {
        let seq = sink
            .emit(LogEntry {
                sequence: 0,
                job_rid: job_rid.clone(),
                ts: chrono::Utc::now(),
                level: LogLevel::Info,
                message: format!("msg-{i}"),
                params: None,
            })
            .await
            .unwrap();
        sequences.push(seq);
    }

    // Simulate disconnect after consuming the first three entries.
    let resume_from = sequences[2] + 1;

    // Replay-from-resume_from should return entries 4 and 5 only.
    let rows: Vec<(i64, String)> = sqlx::query_as(
        "SELECT sequence, message FROM job_logs
            WHERE job_id = $1 AND sequence >= $2 ORDER BY sequence ASC",
    )
    .bind(job_id)
    .bind(resume_from)
    .fetch_all(&harness.pool)
    .await
    .unwrap();
    let messages: Vec<&str> = rows.iter().map(|(_, m)| m.as_str()).collect();
    assert_eq!(messages, vec!["msg-3", "msg-4"]);
}
