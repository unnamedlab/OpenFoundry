// Health-check models. Consolidated from the retired
// `health-check-service` (S8.1.a, ADR-0030).

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct PrimaryItem {
    pub id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreatePrimaryRequest {
    pub payload: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct SecondaryItem {
    pub id: Uuid,
    pub parent_id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateSecondaryRequest {
    pub payload: serde_json::Value,
}
