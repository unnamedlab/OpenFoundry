//! Live logs (Foundry Builds.md § Live logs).
//!
//! The runner emits structured log entries through a [`LogSink`].
//! Production wires up [`CompositeLogSink`] which fans out to:
//!
//!   * [`PostgresLogSink`] — persistent append-only history backing the
//!     REST `/v1/jobs/{rid}/logs` endpoint and the catch-up phase of
//!     the SSE stream.
//!   * [`BroadcastLogSink`] — in-memory `tokio::sync::broadcast` per
//!     job RID. SSE / WebSocket subscribers tail it for real-time
//!     updates.
//!
//! Color coding (Foundry doc): `INFO` = blue, `WARN` = orange,
//! `ERROR` / `FATAL` = red, `DEBUG` / `TRACE` = gray. The colour is
//! decided client-side; the backend only emits the canonical level
//! strings.

mod broadcast;
mod composite;
mod postgres_sink;

pub use broadcast::BroadcastLogSink;
pub use composite::CompositeLogSink;
pub use postgres_sink::PostgresLogSink;

use std::fmt;
use std::str::FromStr;
use std::sync::Arc;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize, PartialOrd, Ord)]
#[serde(rename_all = "UPPERCASE")]
pub enum LogLevel {
    Trace,
    Debug,
    Info,
    Warn,
    Error,
    Fatal,
}

impl LogLevel {
    pub const ALL: &'static [LogLevel] = &[
        LogLevel::Trace,
        LogLevel::Debug,
        LogLevel::Info,
        LogLevel::Warn,
        LogLevel::Error,
        LogLevel::Fatal,
    ];

    pub fn as_str(&self) -> &'static str {
        match self {
            LogLevel::Trace => "TRACE",
            LogLevel::Debug => "DEBUG",
            LogLevel::Info => "INFO",
            LogLevel::Warn => "WARN",
            LogLevel::Error => "ERROR",
            LogLevel::Fatal => "FATAL",
        }
    }

    /// Map to the closest `tracing::Level` so callers that bridge to
    /// OpenTelemetry can re-emit. `FATAL` collapses to `ERROR`
    /// (tracing has no `FATAL`).
    pub fn to_tracing(self) -> tracing::Level {
        match self {
            LogLevel::Trace => tracing::Level::TRACE,
            LogLevel::Debug => tracing::Level::DEBUG,
            LogLevel::Info => tracing::Level::INFO,
            LogLevel::Warn => tracing::Level::WARN,
            LogLevel::Error | LogLevel::Fatal => tracing::Level::ERROR,
        }
    }
}

impl fmt::Display for LogLevel {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

impl FromStr for LogLevel {
    type Err = UnknownLogLevel;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        match s.to_ascii_uppercase().as_str() {
            "TRACE" => Ok(LogLevel::Trace),
            "DEBUG" => Ok(LogLevel::Debug),
            "INFO" => Ok(LogLevel::Info),
            "WARN" | "WARNING" => Ok(LogLevel::Warn),
            "ERROR" => Ok(LogLevel::Error),
            "FATAL" => Ok(LogLevel::Fatal),
            other => Err(UnknownLogLevel(other.to_string())),
        }
    }
}

#[derive(Debug, thiserror::Error)]
#[error("unknown log level: {0}")]
pub struct UnknownLogLevel(pub String);

/// One log entry as emitted by the runner / persisted in `job_logs`
/// / serialized over SSE.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct LogEntry {
    /// Monotonic sequence per-job. Clients resume from
    /// `last_sequence_seen` after a disconnect. Server-assigned.
    #[serde(default)]
    pub sequence: i64,
    pub job_rid: String,
    pub ts: DateTime<Utc>,
    pub level: LogLevel,
    pub message: String,
    /// Optional safe-parameters block surfaced by the "Format as JSON"
    /// affordance in the live-log viewer.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub params: Option<serde_json::Value>,
}

#[derive(Debug, thiserror::Error)]
pub enum LogSinkError {
    #[error("db: {0}")]
    Db(#[from] sqlx::Error),
    #[error("broadcast: {0}")]
    Broadcast(String),
    #[error("job not found: {0}")]
    JobNotFound(String),
}

/// A consumer of structured log events. Implementations may persist,
/// broadcast, or both.
#[async_trait]
pub trait LogSink: Send + Sync {
    /// Emit one log entry. Implementations must be tolerant of
    /// transient errors (the runner does not block on log emission);
    /// log them via `tracing::warn!` rather than propagating.
    async fn emit(&self, entry: LogEntry) -> Result<i64, LogSinkError>;
}

/// Convenience wrapper so the runners can emit without constructing
/// a full [`LogEntry`] each time.
pub async fn emit_now(
    sink: &Arc<dyn LogSink>,
    job_rid: &str,
    level: LogLevel,
    message: impl Into<String>,
    params: Option<serde_json::Value>,
) -> Result<i64, LogSinkError> {
    sink.emit(LogEntry {
        sequence: 0,
        job_rid: job_rid.to_string(),
        ts: Utc::now(),
        level,
        message: message.into(),
        params,
    })
    .await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn level_round_trip_and_aliases() {
        for level in LogLevel::ALL {
            let parsed: LogLevel = level.as_str().parse().unwrap();
            assert_eq!(parsed, *level);
        }
        assert_eq!("warning".parse::<LogLevel>().unwrap(), LogLevel::Warn);
        assert!("VERBOSE".parse::<LogLevel>().is_err());
    }

    #[test]
    fn fatal_maps_to_tracing_error() {
        assert_eq!(LogLevel::Fatal.to_tracing(), tracing::Level::ERROR);
        assert_eq!(LogLevel::Error.to_tracing(), tracing::Level::ERROR);
        assert_eq!(LogLevel::Info.to_tracing(), tracing::Level::INFO);
    }

    #[test]
    fn level_serde_uppercases() {
        let raw = serde_json::to_value(LogLevel::Warn).unwrap();
        assert_eq!(raw, serde_json::Value::String("WARN".into()));
    }
}
