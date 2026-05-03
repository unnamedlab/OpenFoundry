//! Simple reconcile loop. Periodically scans the declarative `ingest_jobs`
//! inventory and re-applies each rendered resource to the cluster
//! (server-side apply is idempotent, so this is safe and cheap).
//!
//! The loop is deliberately straightforward — it does not yet watch CRD
//! status fields. It exists so that resources reappear automatically if a
//! human (or another controller) deletes them out from under us. The target
//! architecture keeps this only as desired-state persistence, not as the
//! authoritative execution runtime.

use std::time::Duration;

use kube::Client;
use sqlx::PgPool;
use tokio::time::sleep;

use crate::control_plane::{apply_resources, render_resources};
use crate::repository;
use crate::runtime_state::{self, IngestJobRuntimeState};

/// Run the reconcile loop forever. Cancellable by dropping the future.
pub async fn run(pool: PgPool, client: Client, period: Duration) {
    tracing::info!(?period, "ingestion-replication reconcile loop starting");
    loop {
        if let Err(err) = tick(&pool, &client).await {
            tracing::warn!("reconcile tick failed: {err}");
        }
        sleep(period).await;
    }
}

/// Run a single iteration. Exposed for testing.
pub async fn tick(pool: &PgPool, client: &Client) -> anyhow::Result<()> {
    let jobs = repository::list_reconcilable(pool).await?;
    for job in jobs {
        match render_resources(&job.spec) {
            Ok(rendered) => {
                if let Err(err) = apply_resources(client, &rendered).await {
                    tracing::warn!(job_id = %job.id, "reconcile apply failed: {err}");
                    let _ = runtime_state::upsert_job_runtime_state(
                        client,
                        &job,
                        &IngestJobRuntimeState::failed(err.to_string()),
                    )
                    .await;
                } else {
                    let kc = rendered
                        .kafka_connector
                        .metadata
                        .name
                        .clone()
                        .unwrap_or_default();
                    let fl = rendered
                        .flink_deployment
                        .as_ref()
                        .and_then(|f| f.metadata.name.clone());
                    let _ = repository::mark_materialized(pool, job.id, &kc, fl.as_deref()).await;
                    let _ = runtime_state::upsert_job_runtime_state(
                        client,
                        &job,
                        &IngestJobRuntimeState::materialized(kc, fl.as_deref()),
                    )
                    .await;
                }
            }
            Err(err) => {
                tracing::warn!(job_id = %job.id, "reconcile render failed: {err}");
                let _ = runtime_state::upsert_job_runtime_state(
                    client,
                    &job,
                    &IngestJobRuntimeState::failed(err.to_string()),
                )
                .await;
            }
        }
    }
    Ok(())
}
