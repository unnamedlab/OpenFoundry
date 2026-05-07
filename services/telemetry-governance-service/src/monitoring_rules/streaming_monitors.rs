//! Foundry-parity Stream Monitoring (Bloque P4).
//!
//! Hosts the typed monitor model (views, rules, evaluations) and the
//! HTTP handlers wired by the Axum binary in `main.rs`. The evaluator
//! lives in a sibling module so it can be exercised by integration
//! tests without a running webserver.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

// ---------------------------------------------------------------------
// Enum types
// ---------------------------------------------------------------------

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ResourceType {
    StreamingDataset,
    StreamingPipeline,
    TimeSeriesSync,
    GeotemporalObservations,
}

impl ResourceType {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::StreamingDataset => "STREAMING_DATASET",
            Self::StreamingPipeline => "STREAMING_PIPELINE",
            Self::TimeSeriesSync => "TIME_SERIES_SYNC",
            Self::GeotemporalObservations => "GEOTEMPORAL_OBSERVATIONS",
        }
    }
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "STREAMING_DATASET" => Ok(Self::StreamingDataset),
            "STREAMING_PIPELINE" => Ok(Self::StreamingPipeline),
            "TIME_SERIES_SYNC" => Ok(Self::TimeSeriesSync),
            "GEOTEMPORAL_OBSERVATIONS" => Ok(Self::GeotemporalObservations),
            other => Err(format!("unknown resource_type: {other}")),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MonitorKind {
    IngestRecords,
    OutputRecords,
    CheckpointLiveness,
    LastCheckpointDuration,
    CheckpointTriggerFailures,
    ConsecutiveCheckpointFailures,
    TotalLag,
    TotalThroughput,
    Utilization,
    PointsWrittenToTs,
    GeotemporalObsSent,
}

impl MonitorKind {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::IngestRecords => "INGEST_RECORDS",
            Self::OutputRecords => "OUTPUT_RECORDS",
            Self::CheckpointLiveness => "CHECKPOINT_LIVENESS",
            Self::LastCheckpointDuration => "LAST_CHECKPOINT_DURATION",
            Self::CheckpointTriggerFailures => "CHECKPOINT_TRIGGER_FAILURES",
            Self::ConsecutiveCheckpointFailures => "CONSECUTIVE_CHECKPOINT_FAILURES",
            Self::TotalLag => "TOTAL_LAG",
            Self::TotalThroughput => "TOTAL_THROUGHPUT",
            Self::Utilization => "UTILIZATION",
            Self::PointsWrittenToTs => "POINTS_WRITTEN_TO_TS",
            Self::GeotemporalObsSent => "GEOTEMPORAL_OBS_SENT",
        }
    }
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "INGEST_RECORDS" => Ok(Self::IngestRecords),
            "OUTPUT_RECORDS" => Ok(Self::OutputRecords),
            "CHECKPOINT_LIVENESS" => Ok(Self::CheckpointLiveness),
            "LAST_CHECKPOINT_DURATION" => Ok(Self::LastCheckpointDuration),
            "CHECKPOINT_TRIGGER_FAILURES" => Ok(Self::CheckpointTriggerFailures),
            "CONSECUTIVE_CHECKPOINT_FAILURES" => Ok(Self::ConsecutiveCheckpointFailures),
            "TOTAL_LAG" => Ok(Self::TotalLag),
            "TOTAL_THROUGHPUT" => Ok(Self::TotalThroughput),
            "UTILIZATION" => Ok(Self::Utilization),
            "POINTS_WRITTEN_TO_TS" => Ok(Self::PointsWrittenToTs),
            "GEOTEMPORAL_OBS_SENT" => Ok(Self::GeotemporalObsSent),
            other => Err(format!("unknown monitor_kind: {other}")),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum Comparator {
    Lt,
    Lte,
    Gt,
    Gte,
    Eq,
}

impl Comparator {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Lt => "LT",
            Self::Lte => "LTE",
            Self::Gt => "GT",
            Self::Gte => "GTE",
            Self::Eq => "EQ",
        }
    }
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "LT" => Ok(Self::Lt),
            "LTE" => Ok(Self::Lte),
            "GT" => Ok(Self::Gt),
            "GTE" => Ok(Self::Gte),
            "EQ" => Ok(Self::Eq),
            other => Err(format!("unknown comparator: {other}")),
        }
    }
    /// True when the observation crosses the threshold.
    pub fn evaluate(self, observed: f64, threshold: f64) -> bool {
        match self {
            Self::Lt => observed < threshold,
            Self::Lte => observed <= threshold,
            Self::Gt => observed > threshold,
            Self::Gte => observed >= threshold,
            // f64 EQ is rare in practice; we use a small relative
            // tolerance so floating-point noise doesn't mask matches.
            Self::Eq => (observed - threshold).abs() <= f64::EPSILON.max(threshold.abs() * 1e-9),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum Severity {
    Info,
    Warn,
    Critical,
}

impl Severity {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Info => "INFO",
            Self::Warn => "WARN",
            Self::Critical => "CRITICAL",
        }
    }
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "INFO" => Ok(Self::Info),
            "WARN" => Ok(Self::Warn),
            "CRITICAL" => Ok(Self::Critical),
            other => Err(format!("unknown severity: {other}")),
        }
    }
}

// ---------------------------------------------------------------------
// Records / requests / responses
// ---------------------------------------------------------------------

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct MonitoringView {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub project_rid: String,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateMonitoringViewRequest {
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    pub project_rid: String,
}

#[derive(Debug, Clone, Serialize)]
pub struct MonitorRule {
    pub id: Uuid,
    pub view_id: Uuid,
    pub name: String,
    pub resource_type: ResourceType,
    pub resource_rid: String,
    pub monitor_kind: MonitorKind,
    pub window_seconds: i32,
    pub comparator: Comparator,
    pub threshold: f64,
    pub severity: Severity,
    pub enabled: bool,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow)]
pub struct MonitorRuleRow {
    pub id: Uuid,
    pub view_id: Uuid,
    pub name: String,
    pub resource_type: String,
    pub resource_rid: String,
    pub monitor_kind: String,
    pub window_seconds: i32,
    pub comparator: String,
    pub threshold: f64,
    pub severity: String,
    pub enabled: bool,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<MonitorRuleRow> for MonitorRule {
    fn from(row: MonitorRuleRow) -> Self {
        Self {
            id: row.id,
            view_id: row.view_id,
            name: row.name,
            resource_type: ResourceType::from_str(&row.resource_type)
                .unwrap_or(ResourceType::StreamingDataset),
            resource_rid: row.resource_rid,
            monitor_kind: MonitorKind::from_str(&row.monitor_kind)
                .unwrap_or(MonitorKind::IngestRecords),
            window_seconds: row.window_seconds,
            comparator: Comparator::from_str(&row.comparator).unwrap_or(Comparator::Lt),
            threshold: row.threshold,
            severity: Severity::from_str(&row.severity).unwrap_or(Severity::Warn),
            enabled: row.enabled,
            created_by: row.created_by,
            created_at: row.created_at,
            updated_at: row.updated_at,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateMonitorRuleRequest {
    pub view_id: Uuid,
    #[serde(default)]
    pub name: Option<String>,
    pub resource_type: ResourceType,
    pub resource_rid: String,
    pub monitor_kind: MonitorKind,
    pub window_seconds: i32,
    pub comparator: Comparator,
    pub threshold: f64,
    #[serde(default = "default_severity")]
    pub severity: Severity,
}

fn default_severity() -> Severity {
    Severity::Warn
}

impl CreateMonitorRuleRequest {
    pub fn validate(&self) -> Result<(), String> {
        if !(60..=86_400).contains(&self.window_seconds) {
            return Err(format!(
                "window_seconds must be between 60 and 86400 (got {})",
                self.window_seconds
            ));
        }
        if self.resource_rid.trim().is_empty() {
            return Err("resource_rid must not be empty".into());
        }
        if self.threshold.is_nan() || self.threshold.is_infinite() {
            return Err("threshold must be a finite number".into());
        }
        Ok(())
    }
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct MonitorEvaluation {
    pub id: Uuid,
    pub rule_id: Uuid,
    pub evaluated_at: DateTime<Utc>,
    pub observed_value: f64,
    pub fired: bool,
    pub alert_id: Option<Uuid>,
}
