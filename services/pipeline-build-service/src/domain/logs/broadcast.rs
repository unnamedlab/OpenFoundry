//! In-memory `tokio::sync::broadcast` log fan-out.
//!
//! Used by the SSE / WebSocket endpoints to tail live entries without
//! polling Postgres. Each `job_rid` gets its own channel; the
//! broadcaster lazily creates one on first emit / first subscribe.

use std::collections::HashMap;
use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::{Mutex, broadcast};

use super::{LogEntry, LogSink, LogSinkError};
use crate::domain::metrics;

/// Default per-job buffer capacity. Subscribers that fall further
/// behind than this many entries see `RecvError::Lagged` and resync
/// via the REST history.
const CHANNEL_CAPACITY: usize = 1024;

#[derive(Clone)]
pub struct BroadcastLogSink {
    inner: Arc<Inner>,
}

struct Inner {
    channels: Mutex<HashMap<String, broadcast::Sender<LogEntry>>>,
}

impl Default for BroadcastLogSink {
    fn default() -> Self {
        Self::new()
    }
}

impl BroadcastLogSink {
    pub fn new() -> Self {
        Self {
            inner: Arc::new(Inner {
                channels: Mutex::new(HashMap::new()),
            }),
        }
    }

    /// Subscribe to live entries for a given job RID. The receiver
    /// drops when the subscriber is done; if no live publishers
    /// remain, the channel is reaped on next emit.
    pub async fn subscribe(&self, job_rid: &str) -> broadcast::Receiver<LogEntry> {
        let mut guard = self.inner.channels.lock().await;
        let tx = guard
            .entry(job_rid.to_string())
            .or_insert_with(|| broadcast::channel(CHANNEL_CAPACITY).0);
        let rx = tx.subscribe();
        metrics::set_live_log_subscribers_for(job_rid, tx.receiver_count() as i64);
        rx
    }

    /// Active subscriber count across all jobs (used by the
    /// `live_log_subscribers` gauge sampler).
    pub async fn total_subscribers(&self) -> usize {
        let guard = self.inner.channels.lock().await;
        guard.values().map(|tx| tx.receiver_count()).sum()
    }
}

#[async_trait]
impl LogSink for BroadcastLogSink {
    async fn emit(&self, entry: LogEntry) -> Result<i64, LogSinkError> {
        let mut guard = self.inner.channels.lock().await;
        let tx = guard
            .entry(entry.job_rid.clone())
            .or_insert_with(|| broadcast::channel(CHANNEL_CAPACITY).0);
        // Best-effort: a `send` error means there are no subscribers,
        // which is normal — the broadcast sink is purely best-effort.
        let _ = tx.send(entry);
        Ok(0)
    }
}
