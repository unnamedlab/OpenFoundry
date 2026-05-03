//! Load test: 1000 concurrent schedules against Temporal with two real
//! pipeline workers. Closes DoD-S2 last gate ("exactly one dispatch per
//! schedule") by asserting that no schedule fires more than twice in a
//! 90s window — a double-firing scheduler would surface as 4 or 6
//! actions per schedule (2 workers × 1–2 cron ticks).
//!
//! Reproducibility: must pass three runs in a row. Each run boots a
//! fresh Temporal dev-server container and two `go run .` worker
//! processes, so state cannot leak between iterations.

#![cfg(feature = "it-temporal")]

use std::{path::PathBuf, sync::Arc, time::Duration};

use futures::{StreamExt, stream};
use temporal_client::{
    GrpcWorkflowClient, Namespace, PipelineRunInput, PipelineScheduleClient, RuntimeClientConfig,
    WorkflowClient,
};
use testing::{go_workers::GoWorker, temporal::boot_temporal};
use uuid::Uuid;

const SCHEDULE_COUNT: usize = 1000;
const OBSERVATION_WINDOW: Duration = Duration::from_secs(90);
const CREATE_CONCURRENCY: usize = 32;
const DESCRIBE_CONCURRENCY: usize = 32;
const DELETE_CONCURRENCY: usize = 64;
const MAX_ACTIONS_PER_SCHEDULE: i64 = 2;

fn repo_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .ancestors()
        .nth(2)
        .expect("repo root from service manifest")
        .to_path_buf()
}

fn run_input() -> PipelineRunInput {
    PipelineRunInput {
        pipeline_id: Uuid::now_v7(),
        tenant_id: "tenant-load".to_string(),
        revision: None,
        parameters: serde_json::json!({"source": "temporal-load"}),
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 8)]
async fn one_thousand_schedules_never_double_fire_with_two_workers() {
    let harness = boot_temporal().await;
    let repo_root = repo_root();
    let mut worker_a = GoWorker::spawn(
        &repo_root,
        "pipeline",
        &harness.frontend,
        &harness.namespace,
    )
    .await;
    let mut worker_b = GoWorker::spawn(
        &repo_root,
        "pipeline",
        &harness.frontend,
        &harness.namespace,
    )
    .await;

    let namespace = Namespace::new(harness.namespace.clone());
    let grpc = GrpcWorkflowClient::connect(RuntimeClientConfig {
        host_port: Some(harness.frontend.clone()),
        namespace: harness.namespace.clone(),
        identity: "pipeline-schedule-service-load".to_string(),
        api_key: None,
    })
    .await
    .expect("Temporal gRPC client");
    let shared: Arc<dyn WorkflowClient> = Arc::new(grpc.clone());
    let scheduler = PipelineScheduleClient::new(shared, namespace.clone());

    let schedule_ids: Vec<String> = (0..SCHEDULE_COUNT)
        .map(|_| format!("pipeline-load-{}", Uuid::now_v7()))
        .collect();

    eprintln!("creating {SCHEDULE_COUNT} schedules with cron `* * * * *`…");
    let create_started = std::time::Instant::now();
    let create_errors: Vec<String> = stream::iter(schedule_ids.clone())
        .map(|id| {
            let scheduler = scheduler.clone();
            async move {
                scheduler
                    .create(
                        id.clone(),
                        vec!["* * * * *".to_string()],
                        Some("UTC".to_string()),
                        run_input(),
                        Uuid::now_v7(),
                    )
                    .await
                    .err()
                    .map(|err| format!("{id}: {err}"))
            }
        })
        .buffer_unordered(CREATE_CONCURRENCY)
        .filter_map(|opt| async move { opt })
        .collect()
        .await;
    eprintln!(
        "schedule creation completed in {:?} ({} errors)",
        create_started.elapsed(),
        create_errors.len()
    );
    if !create_errors.is_empty() {
        cleanup(&scheduler, &schedule_ids, &mut worker_a, &mut worker_b).await;
        panic!(
            "schedule creation failed for {} schedules; first 5: {:?}",
            create_errors.len(),
            &create_errors[..create_errors.len().min(5)]
        );
    }

    eprintln!(
        "observing schedule firings for {}s…",
        OBSERVATION_WINDOW.as_secs()
    );
    tokio::time::sleep(OBSERVATION_WINDOW).await;

    eprintln!("describing all {SCHEDULE_COUNT} schedules…");
    let describe_started = std::time::Instant::now();
    let action_counts: Vec<(String, i64)> = stream::iter(schedule_ids.clone())
        .map(|id| {
            let grpc = grpc.clone();
            let namespace = namespace.clone();
            async move {
                let desc = grpc
                    .describe_schedule(&namespace, &id)
                    .await
                    .unwrap_or_else(|err| panic!("describe schedule {id} failed: {err}"));
                (id, desc.action_count)
            }
        })
        .buffer_unordered(DESCRIBE_CONCURRENCY)
        .collect()
        .await;
    eprintln!(
        "describe completed in {:?}",
        describe_started.elapsed()
    );

    let max_actions = action_counts.iter().map(|(_, n)| *n).max().unwrap_or(0);
    let total_actions: i64 = action_counts.iter().map(|(_, n)| *n).sum();
    let fired = action_counts.iter().filter(|(_, n)| *n > 0).count();
    let violators: Vec<&(String, i64)> = action_counts
        .iter()
        .filter(|(_, n)| *n > MAX_ACTIONS_PER_SCHEDULE)
        .collect();
    eprintln!(
        "fired={fired}/{SCHEDULE_COUNT} total_actions={total_actions} max_actions_per_schedule={max_actions} violators={}",
        violators.len()
    );

    cleanup(&scheduler, &schedule_ids, &mut worker_a, &mut worker_b).await;

    assert!(
        violators.is_empty(),
        "{} schedules dispatched more than {} times — first 5: {:?}",
        violators.len(),
        MAX_ACTIONS_PER_SCHEDULE,
        violators.iter().take(5).collect::<Vec<_>>()
    );
    assert!(
        fired > 0,
        "no schedule fired in the {}s observation window — Temporal scheduler did not run",
        OBSERVATION_WINDOW.as_secs()
    );
}

async fn cleanup(
    scheduler: &PipelineScheduleClient,
    schedule_ids: &[String],
    worker_a: &mut GoWorker,
    worker_b: &mut GoWorker,
) {
    eprintln!("deleting {} schedules…", schedule_ids.len());
    stream::iter(schedule_ids.to_vec())
        .for_each_concurrent(DELETE_CONCURRENCY, |id| {
            let scheduler = scheduler.clone();
            async move {
                let _ = scheduler.delete(&id).await;
            }
        })
        .await;
    worker_a.stop().await;
    worker_b.stop().await;
}
