//! Persistent models for the saved-queries side router.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct SavedQuery {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub sql: String,
    pub owner_id: Uuid,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateSavedQueryRequest {
    pub name: String,
    pub description: Option<String>,
    /// SQL body. May be omitted when the caller passes
    /// `?seed_dataset_rid=` — the handler then pre-fills it with
    /// `SELECT * FROM <dataset>`. Mirrors Foundry's "Open in SQL
    /// workbench" entry point.
    #[serde(default)]
    pub sql: Option<String>,
}

/// P5 — query string for `POST /api/v1/queries/saved`.
/// Setting `seed_dataset_rid` is the "Open in SQL workbench" path:
/// when the body's `sql` is empty the handler auto-fills it with
/// `SELECT * FROM <dataset>` so the user lands on a runnable query.
#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct CreateSavedQueryParams {
    pub seed_dataset_rid: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct ListQueriesQuery {
    pub page: Option<i64>,
    pub per_page: Option<i64>,
    pub search: Option<String>,
}
