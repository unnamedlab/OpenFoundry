//! Composite [`super::LogSink`] that fans out to N inner sinks.
//!
//! Production wires `[PostgresLogSink, BroadcastLogSink]` so every
//! entry lands on disk first (the `sequence` returned by Postgres is
//! authoritative), then is broadcast with the persisted sequence
//! attached.

use std::sync::Arc;

use async_trait::async_trait;

use super::{LogEntry, LogSink, LogSinkError};

#[derive(Clone)]
pub struct CompositeLogSink {
    pub persistent: Arc<dyn LogSink>,
    pub broadcaster: Arc<dyn LogSink>,
}

impl CompositeLogSink {
    pub fn new(persistent: Arc<dyn LogSink>, broadcaster: Arc<dyn LogSink>) -> Self {
        Self {
            persistent,
            broadcaster,
        }
    }
}

#[async_trait]
impl LogSink for CompositeLogSink {
    async fn emit(&self, mut entry: LogEntry) -> Result<i64, LogSinkError> {
        // Persist first so `sequence` is authoritative; then mirror
        // the (now sequence-stamped) entry to the broadcast channel.
        let sequence = match self.persistent.emit(entry.clone()).await {
            Ok(seq) => seq,
            Err(err) => {
                tracing::warn!(error = %err, "log persist failed; broadcasting without sequence");
                0
            }
        };
        entry.sequence = sequence;
        if let Err(err) = self.broadcaster.emit(entry).await {
            tracing::warn!(error = %err, "log broadcast failed");
        }
        Ok(sequence)
    }
}
