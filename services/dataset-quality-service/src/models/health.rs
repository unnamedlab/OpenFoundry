//! P6 — Foundry "Data Health" + "Health checks" model.
//!
//! Wire shape consumed by the UI's QualityDashboard cards. Mirrors
//! `migrations/20260503000004_dataset_health.sql`.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use std::collections::BTreeMap;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow)]
pub struct DatasetHealthRow {
    pub dataset_rid: String,
    pub dataset_id: Option<Uuid>,
    pub row_count: i64,
    pub col_count: i32,
    pub null_pct_by_column: Value,
    pub freshness_seconds: i64,
    pub last_commit_at: Option<DateTime<Utc>>,
    pub txn_failure_rate_24h: f64,
    pub last_build_status: String,
    pub schema_drift_flag: bool,
    pub extras: Value,
    pub last_computed_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetHealth {
    pub dataset_rid: String,
    pub dataset_id: Option<Uuid>,
    pub row_count: i64,
    pub col_count: i32,
    pub null_pct_by_column: BTreeMap<String, f64>,
    pub freshness_seconds: i64,
    pub last_commit_at: Option<DateTime<Utc>>,
    pub txn_failure_rate_24h: f64,
    pub last_build_status: String,
    pub schema_drift_flag: bool,
    /// Free-form payload for dashboard cards. Common keys:
    ///   * `failure_breakdown_24h: { tx_type: count }`
    ///   * `row_count_history: [{ ts, value }]`  (30-day sparkline)
    pub extras: Value,
    pub last_computed_at: DateTime<Utc>,
}

impl TryFrom<DatasetHealthRow> for DatasetHealth {
    type Error = String;
    fn try_from(row: DatasetHealthRow) -> Result<Self, Self::Error> {
        let null_pct: BTreeMap<String, f64> = serde_json::from_value(row.null_pct_by_column.clone())
            .map_err(|e| format!("null_pct_by_column decode: {e}"))?;
        let txn_failure: f64 = row.txn_failure_rate_24h;
        Ok(Self {
            dataset_rid: row.dataset_rid,
            dataset_id: row.dataset_id,
            row_count: row.row_count,
            col_count: row.col_count,
            null_pct_by_column: null_pct,
            freshness_seconds: row.freshness_seconds,
            last_commit_at: row.last_commit_at,
            txn_failure_rate_24h: txn_failure,
            last_build_status: row.last_build_status,
            schema_drift_flag: row.schema_drift_flag,
            extras: row.extras,
            last_computed_at: row.last_computed_at,
        })
    }
}

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct DatasetHealthPolicy {
    pub id: Uuid,
    pub name: String,
    pub dataset_rid: Option<String>,
    pub all_datasets: bool,
    pub check_kind: String,
    pub threshold: Value,
    pub severity: String,
    pub active: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateHealthPolicyRequest {
    pub name: String,
    #[serde(default)]
    pub dataset_rid: Option<String>,
    #[serde(default)]
    pub all_datasets: bool,
    pub check_kind: String,
    #[serde(default)]
    pub threshold: Value,
    #[serde(default = "default_severity")]
    pub severity: String,
    #[serde(default = "default_true")]
    pub active: bool,
}

fn default_severity() -> String {
    "warning".into()
}
fn default_true() -> bool {
    true
}
