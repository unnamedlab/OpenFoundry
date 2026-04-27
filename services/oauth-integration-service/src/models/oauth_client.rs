use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct OAuthInboundClient {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    pub application_id: Option<Uuid>,
    pub client_id: String,
    pub secret_hint: String,
    pub redirect_uris: serde_json::Value,
    pub allowed_scopes: serde_json::Value,
    pub grant_types: serde_json::Value,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OAuthInboundClientWithSecret {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    pub application_id: Option<Uuid>,
    pub client_id: String,
    pub client_secret: String,
    pub redirect_uris: Vec<String>,
    pub allowed_scopes: Vec<String>,
    pub grant_types: Vec<String>,
    pub status: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateOAuthInboundClientRequest {
    pub slug: String,
    pub display_name: String,
    pub application_id: Option<Uuid>,
    #[serde(default)]
    pub redirect_uris: Vec<String>,
    #[serde(default)]
    pub allowed_scopes: Vec<String>,
    #[serde(default)]
    pub grant_types: Vec<String>,
    pub status: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateOAuthInboundClientRequest {
    pub display_name: Option<String>,
    pub application_id: Option<Option<Uuid>>,
    pub redirect_uris: Option<Vec<String>>,
    pub allowed_scopes: Option<Vec<String>>,
    pub grant_types: Option<Vec<String>>,
    pub status: Option<String>,
}
