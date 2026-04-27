use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct Enrollment {
    pub id: Uuid,
    pub organization_id: Uuid,
    pub user_id: Uuid,
    pub workspace_slug: Option<String>,
    pub role_slug: String,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateEnrollmentRequest {
    pub organization_id: Uuid,
    pub user_id: Uuid,
    pub workspace_slug: Option<String>,
    pub role_slug: String,
    pub status: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateEnrollmentRequest {
    pub workspace_slug: Option<Option<String>>,
    pub role_slug: Option<String>,
    pub status: Option<String>,
}
