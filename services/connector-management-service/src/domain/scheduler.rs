use std::time::Duration;

use crate::{AppState, domain::sync_engine};

/// Legacy scheduler shim. The local sync runtime is disabled, so ticking this
/// loop is a no-op kept only for compatibility with older callers/tests.
pub async fn run_scheduler(state: AppState, poll_interval: Duration) {
    let mut interval = tokio::time::interval(poll_interval);
    loop {
        interval.tick().await;
        if let Err(error) = tick(&state).await {
            tracing::warn!("sync scheduler tick failed: {error}");
        }
    }
}

pub async fn tick(state: &AppState) -> Result<usize, String> {
    sync_engine::run_due_jobs(state).await
}
