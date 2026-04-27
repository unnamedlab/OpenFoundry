use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// A logical time-series channel: identifies what is being measured, the value type and the
/// canonical retention/granularity contract.
#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct TimeSeries {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    /// One of: numeric, boolean, categorical, geospatial
    pub value_kind: String,
    pub unit: Option<String>,
    pub granularity_seconds: i32,
    pub retention_days: Option<i32>,
    pub source_kind: Option<String>,
    pub source_ref: Option<String>,
    pub tags: serde_json::Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RegisterTimeSeriesRequest {
    pub slug: String,
    pub display_name: String,
    #[serde(default = "default_value_kind")]
    pub value_kind: String,
    pub unit: Option<String>,
    #[serde(default = "default_granularity")]
    pub granularity_seconds: i32,
    pub retention_days: Option<i32>,
    pub source_kind: Option<String>,
    pub source_ref: Option<String>,
    #[serde(default)]
    pub tags: serde_json::Value,
}

fn default_value_kind() -> String {
    "numeric".to_string()
}

fn default_granularity() -> i32 {
    60
}

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct TimeSeriesPoint {
    pub series_id: Uuid,
    pub recorded_at: DateTime<Utc>,
    pub value_numeric: Option<f64>,
    pub value_text: Option<String>,
    pub attributes: serde_json::Value,
}

#[derive(Debug, Clone, Deserialize)]
pub struct IngestPointsRequest {
    pub points: Vec<IngestPoint>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct IngestPoint {
    pub recorded_at: DateTime<Utc>,
    pub value_numeric: Option<f64>,
    pub value_text: Option<String>,
    #[serde(default)]
    pub attributes: serde_json::Value,
}

#[derive(Debug, Clone, Deserialize)]
pub struct QueryTimeSeriesRequest {
    pub from: DateTime<Utc>,
    pub to: DateTime<Utc>,
    /// One of: raw, mean, min, max, sum, count
    #[serde(default = "default_aggregation")]
    pub aggregation: String,
    /// Bucket size in seconds (used when aggregation != raw).
    pub bucket_seconds: Option<i32>,
    pub limit: Option<i64>,
}

fn default_aggregation() -> String {
    "raw".to_string()
}

/// Coarse partition entry describing where time-series chunks live (warm vs cold tier).
#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct TimeSeriesStoragePartition {
    pub id: Uuid,
    pub series_id: Uuid,
    /// One of: hot, warm, cold
    pub tier: String,
    pub partition_start: DateTime<Utc>,
    pub partition_end: DateTime<Utc>,
    pub storage_uri: Option<String>,
    pub byte_size: Option<i64>,
    pub point_count: i64,
    pub created_at: DateTime<Utc>,
}
