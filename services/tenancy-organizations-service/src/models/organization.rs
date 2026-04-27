use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct Organization {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    pub organization_type: String,
    pub default_workspace: Option<String>,
    pub tenant_tier: Option<String>,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateOrganizationRequest {
    pub slug: String,
    pub display_name: String,
    pub organization_type: Option<String>,
    pub default_workspace: Option<String>,
    pub tenant_tier: Option<String>,
    pub status: Option<String>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateOrganizationRequest {
    pub display_name: Option<String>,
    pub organization_type: Option<String>,
    pub default_workspace: Option<Option<String>>,
    pub tenant_tier: Option<Option<String>>,
    pub status: Option<String>,
}
