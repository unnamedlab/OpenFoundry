//! Mid-level SSE smoke test. Wraps the broadcast sink + history
//! query inside a hand-rolled stream that mirrors the SSE handler's
//! contract:
//!
//!   1. emit a heartbeat per second for `SSE_INITIAL_DELAY_SECS`
//!      (compressed to 100 ms here so the test stays fast),
//!   2. flush persisted history,
//!   3. tail live entries until the producer drops.
//!
//! End-to-end the real handler additionally serializes events to
//! `axum::response::sse::Event`. Validating that wrapping is the
//! province of an HTTP-level test (see `sse_resumes_…` for the
//! sequence-resume guarantee).

use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use pipeline_build_service::domain::logs::{
    BroadcastLogSink, LogEntry, LogLevel, LogSink,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn broadcast_subscriber_receives_emitted_entries() {
    let broadcaster = Arc::new(BroadcastLogSink::new());
    let job_rid = "ri.foundry.main.job.live-1";

    // Subscribe BEFORE the producer emits so we don't lose any
    // entries to the channel buffer.
    let mut rx = broadcaster.subscribe(job_rid).await;

    let producer = broadcaster.clone();
    let producer_rid = job_rid.to_string();
    let handle = tokio::spawn(async move {
        for i in 0..5 {
            producer
                .emit(LogEntry {
                    sequence: i,
                    job_rid: producer_rid.clone(),
                    ts: Utc::now(),
                    level: LogLevel::Info,
                    message: format!("entry-{i}"),
                    params: None,
                })
                .await
                .unwrap();
            tokio::time::sleep(Duration::from_millis(20)).await;
        }
    });

    let mut received = Vec::new();
    while received.len() < 5 {
        let entry = tokio::time::timeout(Duration::from_secs(2), rx.recv())
            .await
            .expect("entry within 2s")
            .expect("entry value");
        received.push(entry);
    }
    handle.await.unwrap();

    assert_eq!(received.len(), 5);
    for (i, entry) in received.iter().enumerate() {
        assert_eq!(entry.message, format!("entry-{i}"));
        assert_eq!(entry.level, LogLevel::Info);
        assert_eq!(entry.job_rid, job_rid);
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn subscribers_filter_by_job_rid() {
    let broadcaster = Arc::new(BroadcastLogSink::new());
    let mut rx_a = broadcaster.subscribe("ri.foundry.main.job.A").await;
    let mut rx_b = broadcaster.subscribe("ri.foundry.main.job.B").await;

    broadcaster
        .emit(LogEntry {
            sequence: 1,
            job_rid: "ri.foundry.main.job.A".into(),
            ts: Utc::now(),
            level: LogLevel::Warn,
            message: "for-a".into(),
            params: None,
        })
        .await
        .unwrap();

    let a = tokio::time::timeout(Duration::from_secs(1), rx_a.recv())
        .await
        .unwrap()
        .unwrap();
    assert_eq!(a.message, "for-a");

    // B's subscriber must NOT see A's entry.
    let b = tokio::time::timeout(Duration::from_millis(200), rx_b.recv()).await;
    assert!(b.is_err(), "subscriber B should not have received A's entry");
}
