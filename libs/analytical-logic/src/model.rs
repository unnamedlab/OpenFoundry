//! Domain model for saved analytical expressions.
//!
//! `payload` is intentionally `serde_json::Value` because the Foundry
//! "visual function templates" surface stores arbitrary JSON
//! (display name, parameters, body, dependencies, …); validating the
//! shape is the consumer's responsibility.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

/// A saved analytical expression (Foundry "visual function template").
#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct AnalyticalExpression {
    pub id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

/// One historical version of an [`AnalyticalExpression`].
#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct AnalyticalExpressionVersion {
    pub id: Uuid,
    pub parent_id: Uuid,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

/// Insert payload accepted by [`crate::repo::AnalyticalExpressionRepo::create`].
#[derive(Debug, Clone, Deserialize)]
pub struct NewExpression {
    pub payload: serde_json::Value,
}

/// Insert payload accepted by
/// [`crate::repo::AnalyticalExpressionRepo::add_version`].
#[derive(Debug, Clone, Deserialize)]
pub struct NewExpressionVersion {
    pub payload: serde_json::Value,
}
