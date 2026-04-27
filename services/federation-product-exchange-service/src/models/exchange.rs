use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sqlx::FromRow;
use uuid::Uuid;

use crate::models::decode_json;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InstallActivation {
    pub kind: String,
    pub status: String,
    pub resource_id: Option<Uuid>,
    pub resource_slug: Option<String>,
    pub public_url: Option<String>,
    pub notes: Option<String>,
}

impl Default for InstallActivation {
    fn default() -> Self {
        Self {
            kind: "marketplace_record".to_string(),
            status: "recorded".to_string(),
            resource_id: None,
            resource_slug: None,
            public_url: None,
            notes: Some("Install recorded by federation-product-exchange-service".to_string()),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct DependencyRequirement {
    pub package_slug: String,
    pub version_req: String,
    pub required: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct MaintenanceWindow {
    #[serde(default = "default_timezone")]
    pub timezone: String,
    #[serde(default)]
    pub days: Vec<String>,
    #[serde(default)]
    pub start_hour_utc: u8,
    #[serde(default = "default_duration_minutes")]
    pub duration_minutes: i32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct InstallRecord {
    pub id: Uuid,
    pub listing_id: Uuid,
    pub listing_name: String,
    pub version: String,
    pub release_channel: String,
    pub workspace_name: String,
    pub status: String,
    pub dependency_plan: Vec<DependencyRequirement>,
    pub activation: InstallActivation,
    pub fleet_id: Option<Uuid>,
    pub fleet_name: Option<String>,
    pub auto_upgrade_enabled: bool,
    pub maintenance_window: Option<MaintenanceWindow>,
    pub enrollment_branch: Option<String>,
    pub installed_at: DateTime<Utc>,
    pub ready_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateInstallRequest {
    pub listing_id: Uuid,
    #[serde(default)]
    pub version: String,
    pub workspace_name: String,
    #[serde(default = "default_release_channel")]
    pub release_channel: String,
    #[serde(default)]
    pub fleet_id: Option<Uuid>,
    #[serde(default)]
    pub enrollment_branch: Option<String>,
}

#[derive(Debug, Clone, FromRow)]
pub struct InstallRow {
    pub id: Uuid,
    pub listing_id: Uuid,
    pub listing_name: String,
    pub version: String,
    pub release_channel: String,
    pub workspace_name: String,
    pub status: String,
    pub dependency_plan: Value,
    pub activation: Value,
    pub fleet_id: Option<Uuid>,
    pub fleet_name: Option<String>,
    pub maintenance_window: Value,
    pub auto_upgrade_enabled: bool,
    pub enrollment_branch: Option<String>,
    pub installed_at: DateTime<Utc>,
    pub ready_at: Option<DateTime<Utc>>,
}

impl TryFrom<InstallRow> for InstallRecord {
    type Error = String;

    fn try_from(row: InstallRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: row.id,
            listing_id: row.listing_id,
            listing_name: row.listing_name,
            version: row.version,
            release_channel: row.release_channel,
            workspace_name: row.workspace_name,
            status: row.status,
            dependency_plan: decode_json(row.dependency_plan, "dependency_plan")?,
            activation: if row.activation.is_null() || row.activation == json!({}) {
                InstallActivation::default()
            } else {
                decode_json(row.activation, "activation")?
            },
            fleet_id: row.fleet_id,
            fleet_name: row.fleet_name,
            auto_upgrade_enabled: row.auto_upgrade_enabled,
            maintenance_window: if row.maintenance_window.is_null()
                || row.maintenance_window == json!({})
            {
                None
            } else {
                Some(decode_json(row.maintenance_window, "maintenance_window")?)
            },
            enrollment_branch: row.enrollment_branch,
            installed_at: row.installed_at,
            ready_at: row.ready_at,
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnrollmentBranchRecord {
    pub id: Uuid,
    pub fleet_id: Uuid,
    pub fleet_name: String,
    pub listing_id: Uuid,
    pub listing_name: String,
    pub name: String,
    pub repository_branch: String,
    pub source_release_channel: String,
    pub source_version: Option<String>,
    pub workspace_targets: Vec<String>,
    pub status: String,
    pub notes: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateEnrollmentBranchRequest {
    pub fleet_id: Uuid,
    pub name: String,
    #[serde(default)]
    pub repository_branch: Option<String>,
    #[serde(default)]
    pub notes: String,
}

#[derive(Debug, Clone, FromRow)]
pub struct EnrollmentBranchRow {
    pub id: Uuid,
    pub fleet_id: Uuid,
    pub fleet_name: String,
    pub listing_id: Uuid,
    pub listing_name: String,
    pub name: String,
    pub repository_branch: String,
    pub source_release_channel: String,
    pub source_version: Option<String>,
    pub workspace_targets: Value,
    pub status: String,
    pub notes: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl TryFrom<EnrollmentBranchRow> for EnrollmentBranchRecord {
    type Error = String;

    fn try_from(row: EnrollmentBranchRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: row.id,
            fleet_id: row.fleet_id,
            fleet_name: row.fleet_name,
            listing_id: row.listing_id,
            listing_name: row.listing_name,
            name: row.name,
            repository_branch: row.repository_branch,
            source_release_channel: row.source_release_channel,
            source_version: row.source_version,
            workspace_targets: decode_json(row.workspace_targets, "workspace_targets")?,
            status: row.status,
            notes: row.notes,
            created_at: row.created_at,
            updated_at: row.updated_at,
        })
    }
}

#[derive(Debug, Clone, FromRow)]
pub struct MarketplaceListingRow {
    pub id: Uuid,
    pub name: String,
}

#[derive(Debug, Clone, FromRow)]
pub struct FleetLookupRow {
    pub id: Uuid,
    pub listing_id: Uuid,
    pub fleet_name: String,
    pub release_channel: String,
    pub workspace_targets: Value,
}

#[derive(Debug, Clone, FromRow)]
pub struct PackageVersionLookupRow {
    pub version: String,
    pub release_channel: String,
    pub dependencies: Value,
    pub published_at: DateTime<Utc>,
}

fn default_release_channel() -> String {
    "stable".to_string()
}

fn default_timezone() -> String {
    "UTC".to_string()
}

fn default_duration_minutes() -> i32 {
    60
}
