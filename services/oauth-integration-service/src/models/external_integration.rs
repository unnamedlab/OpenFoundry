use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct ExternalIntegration {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    pub provider_kind: String,
    pub auth_strategy: String,
    pub connector_profile: Option<String>,
    pub oauth_support: bool,
    pub metadata: serde_json::Value,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateExternalIntegrationRequest {
    pub slug: String,
    pub display_name: String,
    pub provider_kind: String,
    pub auth_strategy: String,
    pub connector_profile: Option<String>,
    pub oauth_support: bool,
    #[serde(default)]
    pub metadata: serde_json::Value,
    pub status: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateExternalIntegrationRequest {
    pub display_name: Option<String>,
    pub provider_kind: Option<String>,
    pub auth_strategy: Option<String>,
    pub connector_profile: Option<Option<String>>,
    pub oauth_support: Option<bool>,
    pub metadata: Option<serde_json::Value>,
    pub status: Option<String>,
}
