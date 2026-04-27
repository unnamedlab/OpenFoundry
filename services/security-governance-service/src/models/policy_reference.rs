use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, FromRow, Serialize, Deserialize)]
pub struct AuthorizationPolicyReference {
    pub id: Uuid,
    pub name: String,
    pub resource: String,
    pub action: String,
    pub conditions: Value,
    pub enabled: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow)]
pub struct RestrictedViewReferenceRow {
    pub id: Uuid,
    pub name: String,
    pub resource: String,
    pub action: String,
    pub hidden_columns: Value,
    pub allowed_markings: Value,
    pub enabled: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestrictedViewReference {
    pub id: Uuid,
    pub name: String,
    pub resource: String,
    pub action: String,
    pub hidden_columns: Vec<String>,
    pub allowed_markings: Vec<String>,
    pub enabled: bool,
}

impl TryFrom<RestrictedViewReferenceRow> for RestrictedViewReference {
    type Error = String;

    fn try_from(row: RestrictedViewReferenceRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: row.id,
            name: row.name,
            resource: row.resource,
            action: row.action,
            hidden_columns: serde_json::from_value(row.hidden_columns)
                .map_err(|error| format!("invalid hidden_columns: {error}"))?,
            allowed_markings: serde_json::from_value(row.allowed_markings)
                .map_err(|error| format!("invalid allowed_markings: {error}"))?,
            enabled: row.enabled,
        })
    }
}
