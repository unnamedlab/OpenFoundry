use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Deserialize)]
pub struct HashContentRequest {
    pub content: String,
    pub salt: Option<String>,
    pub channel: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct HashContentResponse {
    pub algorithm: String,
    pub digest: String,
}

#[derive(Debug, Deserialize)]
pub struct SignContentRequest {
    pub content: String,
    pub key_material: String,
    pub channel: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct SignContentResponse {
    pub algorithm: String,
    pub signature: String,
}

#[derive(Debug, Deserialize)]
pub struct VerifySignatureRequest {
    pub content: String,
    pub key_material: String,
    pub signature: String,
    pub channel: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct VerifySignatureResponse {
    pub algorithm: String,
    pub valid: bool,
}

#[derive(Debug, Deserialize)]
pub struct EncryptContentRequest {
    pub content: String,
    pub key_material: String,
    pub channel: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct EncryptContentResponse {
    pub algorithm: String,
    pub ciphertext: String,
}

#[derive(Debug, Deserialize)]
pub struct DecryptContentRequest {
    pub ciphertext: String,
    pub key_material: String,
    pub channel: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct DecryptContentResponse {
    pub algorithm: String,
    pub content: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct CipherPermission {
    pub id: Uuid,
    pub resource: String,
    pub action: String,
    pub description: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct CipherChannel {
    pub id: Uuid,
    pub name: String,
    pub release_channel: String,
    pub allowed_operations: Value,
    pub license_tier: String,
    pub enabled: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct CipherLicense {
    pub id: Uuid,
    pub name: String,
    pub tier: String,
    pub features: Value,
    pub issued_by: Option<Uuid>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateCipherChannelRequest {
    pub name: String,
    pub release_channel: String,
    #[serde(default)]
    pub allowed_operations: Vec<String>,
    pub license_tier: String,
    #[serde(default = "default_true")]
    pub enabled: bool,
}

#[derive(Debug, Deserialize)]
pub struct CreateCipherLicenseRequest {
    pub name: String,
    pub tier: String,
    #[serde(default)]
    pub features: Vec<String>,
    pub issued_by: Option<Uuid>,
}

fn default_true() -> bool {
    true
}
