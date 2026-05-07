use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct RegisteredApplication {
    pub id: Uuid,
    pub slug: String,
    pub display_name: String,
    pub description: String,
    pub redirect_uris: serde_json::Value,
    pub allowed_scopes: serde_json::Value,
    pub owner_user_id: Uuid,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct ApplicationCredential {
    pub id: Uuid,
    pub application_id: Uuid,
    pub credential_name: String,
    pub client_id: String,
    pub secret_hint: String,
    pub revoked_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ApplicationCredentialWithSecret {
    pub id: Uuid,
    pub application_id: Uuid,
    pub credential_name: String,
    pub client_id: String,
    pub client_secret: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateApplicationRequest {
    pub slug: String,
    pub display_name: String,
    #[serde(default)]
    pub description: String,
    #[serde(default)]
    pub redirect_uris: Vec<String>,
    #[serde(default)]
    pub allowed_scopes: Vec<String>,
    pub status: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdateApplicationRequest {
    pub display_name: Option<String>,
    pub description: Option<String>,
    pub redirect_uris: Option<Vec<String>>,
    pub allowed_scopes: Option<Vec<String>>,
    pub status: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct CreateApplicationCredentialRequest {
    pub credential_name: String,
}
