use axum::{Json, extract::State};
use chrono::{Duration, Utc};
use serde_json::Value;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, internal_error, not_found},
    models::{
        ListResponse,
        exchange::{
            CreateEnrollmentBranchRequest, CreateInstallRequest, DependencyRequirement,
            EnrollmentBranchRecord, EnrollmentBranchRow, FleetLookupRow, InstallActivation,
            InstallRecord, InstallRow, MarketplaceListingRow, PackageVersionLookupRow,
        },
    },
};

pub async fn list_installs(
    State(state): State<AppState>,
) -> ServiceResult<ListResponse<InstallRecord>> {
    let rows = load_installs(&state.marketplace_db)
        .await
        .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse { items: rows }))
}

pub async fn create_install(
    State(state): State<AppState>,
    Json(request): Json<CreateInstallRequest>,
) -> ServiceResult<InstallRecord> {
    if request.workspace_name.trim().is_empty() {
        return Err(bad_request("workspace name is required"));
    }

    let listing = sqlx::query_as::<_, MarketplaceListingRow>(
        "SELECT id, name FROM marketplace_listings WHERE id = $1",
    )
    .bind(request.listing_id)
    .fetch_optional(&state.marketplace_db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("listing not found"))?;

    let fleet = if let Some(fleet_id) = request.fleet_id {
        Some(
            load_fleet(&state.marketplace_db, fleet_id)
                .await
                .map_err(|cause| db_error(&cause))?
                .ok_or_else(|| not_found("fleet not found"))?,
        )
    } else {
        None
    };

    let requested_channel = fleet
        .as_ref()
        .map(|fleet| normalize_release_channel(&fleet.release_channel))
        .unwrap_or_else(|| normalize_release_channel(&request.release_channel));
    let versions = load_versions(&state.marketplace_db, request.listing_id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let version = resolve_version(&versions, &request.version, &requested_channel)
        .ok_or_else(|| bad_request("listing has no published versions"))?;

    let install_id = uuid::Uuid::now_v7();
    let now = Utc::now();
    let workspace_name = request.workspace_name.trim().to_string();
    let dependency_plan =
        serde_json::from_value::<Vec<DependencyRequirement>>(version.dependencies.clone())
            .unwrap_or_default();
    let dependency_json = serde_json::to_value(&dependency_plan)
        .map_err(|cause| internal_error(cause.to_string()))?;
    let activation = InstallActivation::default();
    let activation_json =
        serde_json::to_value(&activation).map_err(|cause| internal_error(cause.to_string()))?;
    let maintenance_window = Value::Object(Default::default());
    let ready_at = Some(now + Duration::minutes(2));

    sqlx::query(
        "INSERT INTO marketplace_installs
         (id, listing_id, listing_name, version, release_channel, workspace_name, status, dependency_plan, activation, fleet_id, maintenance_window, auto_upgrade_enabled, enrollment_branch, installed_at, ready_at)
         VALUES ($1, $2, $3, $4, $5, $6, 'installed', $7::jsonb, $8::jsonb, $9, $10::jsonb, $11, $12, $13, $14)",
    )
    .bind(install_id)
    .bind(listing.id)
    .bind(&listing.name)
    .bind(&version.version)
    .bind(&version.release_channel)
    .bind(&workspace_name)
    .bind(dependency_json)
    .bind(activation_json)
    .bind(request.fleet_id)
    .bind(maintenance_window)
    .bind(fleet.is_some())
    .bind(request.enrollment_branch.clone())
    .bind(now)
    .bind(ready_at)
    .execute(&state.marketplace_db)
    .await
    .map_err(|cause| db_error(&cause))?;

    sqlx::query(
        "UPDATE marketplace_listings SET install_count = install_count + 1, updated_at = NOW() WHERE id = $1",
    )
    .bind(listing.id)
    .execute(&state.marketplace_db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let install = load_installs(&state.marketplace_db)
        .await
        .map_err(|cause| db_error(&cause))?
        .into_iter()
        .find(|item| item.id == install_id)
        .ok_or_else(|| internal_error("created install could not be reloaded"))?;

    Ok(Json(install))
}

pub async fn list_enrollment_branches(
    State(state): State<AppState>,
) -> ServiceResult<ListResponse<EnrollmentBranchRecord>> {
    let rows = load_enrollment_branches(&state.marketplace_db)
        .await
        .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse { items: rows }))
}

pub async fn create_enrollment_branch(
    State(state): State<AppState>,
    Json(request): Json<CreateEnrollmentBranchRequest>,
) -> ServiceResult<EnrollmentBranchRecord> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }

    let fleet = load_fleet(&state.marketplace_db, request.fleet_id)
        .await
        .map_err(|cause| db_error(&cause))?
        .ok_or_else(|| not_found("fleet not found"))?;
    let versions = load_versions(&state.marketplace_db, fleet.listing_id)
        .await
        .map_err(|cause| db_error(&cause))?;
    let source_version =
        latest_version_for_channel(&versions, &fleet.release_channel).map(|entry| entry.version);
    let branch_id = uuid::Uuid::now_v7();
    let repository_branch = request
        .repository_branch
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned)
        .unwrap_or_else(|| derive_repository_branch(&fleet.fleet_name, &request.name));

    sqlx::query(
        "INSERT INTO marketplace_enrollment_branches
         (id, fleet_id, listing_id, name, repository_branch, source_release_channel, source_version, workspace_targets, status, notes, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, 'active', $9, NOW(), NOW())",
    )
    .bind(branch_id)
    .bind(fleet.id)
    .bind(fleet.listing_id)
    .bind(request.name.trim())
    .bind(repository_branch)
    .bind(&fleet.release_channel)
    .bind(source_version)
    .bind(fleet.workspace_targets)
    .bind(request.notes.trim())
    .execute(&state.marketplace_db)
    .await
    .map_err(|cause| db_error(&cause))?;

    let branch = load_enrollment_branches(&state.marketplace_db)
        .await
        .map_err(|cause| db_error(&cause))?
        .into_iter()
        .find(|item| item.id == branch_id)
        .ok_or_else(|| internal_error("created enrollment branch could not be reloaded"))?;

    Ok(Json(branch))
}

async fn load_installs(db: &sqlx::PgPool) -> Result<Vec<InstallRecord>, sqlx::Error> {
    let rows = sqlx::query_as::<_, InstallRow>(
        "SELECT installs.id,
                installs.listing_id,
                installs.listing_name,
                installs.version,
                installs.release_channel,
                installs.workspace_name,
                installs.status,
                installs.dependency_plan,
                installs.activation,
                installs.fleet_id,
                fleets.name AS fleet_name,
                installs.maintenance_window,
                installs.auto_upgrade_enabled,
                installs.enrollment_branch,
                installs.installed_at,
                installs.ready_at
         FROM marketplace_installs installs
         LEFT JOIN marketplace_product_fleets fleets ON fleets.id = installs.fleet_id
         ORDER BY installs.installed_at DESC",
    )
    .fetch_all(db)
    .await?;

    rows.into_iter()
        .map(InstallRecord::try_from)
        .collect::<Result<Vec<_>, _>>()
        .map_err(to_decode_sqlx_error)
}

async fn load_enrollment_branches(
    db: &sqlx::PgPool,
) -> Result<Vec<EnrollmentBranchRecord>, sqlx::Error> {
    let rows = sqlx::query_as::<_, EnrollmentBranchRow>(
        "SELECT branches.id,
                branches.fleet_id,
                fleets.name AS fleet_name,
                branches.listing_id,
                listings.name AS listing_name,
                branches.name,
                branches.repository_branch,
                branches.source_release_channel,
                branches.source_version,
                branches.workspace_targets,
                branches.status,
                branches.notes,
                branches.created_at,
                branches.updated_at
         FROM marketplace_enrollment_branches branches
         INNER JOIN marketplace_product_fleets fleets ON fleets.id = branches.fleet_id
         INNER JOIN marketplace_listings listings ON listings.id = branches.listing_id
         ORDER BY branches.updated_at DESC, branches.created_at DESC",
    )
    .fetch_all(db)
    .await?;

    rows.into_iter()
        .map(EnrollmentBranchRecord::try_from)
        .collect::<Result<Vec<_>, _>>()
        .map_err(to_decode_sqlx_error)
}

async fn load_fleet(
    db: &sqlx::PgPool,
    id: uuid::Uuid,
) -> Result<Option<FleetLookupRow>, sqlx::Error> {
    sqlx::query_as::<_, FleetLookupRow>(
        "SELECT fleets.id,
                fleets.listing_id,
                fleets.name AS fleet_name,
                fleets.release_channel,
                fleets.workspace_targets
         FROM marketplace_product_fleets fleets
         WHERE fleets.id = $1",
    )
    .bind(id)
    .fetch_optional(db)
    .await
}

async fn load_versions(
    db: &sqlx::PgPool,
    listing_id: uuid::Uuid,
) -> Result<Vec<PackageVersionLookupRow>, sqlx::Error> {
    sqlx::query_as::<_, PackageVersionLookupRow>(
        "SELECT version, release_channel, dependencies, published_at
         FROM marketplace_package_versions
         WHERE listing_id = $1
         ORDER BY published_at DESC",
    )
    .bind(listing_id)
    .fetch_all(db)
    .await
}

fn resolve_version(
    versions: &[PackageVersionLookupRow],
    requested_version: &str,
    requested_channel: &str,
) -> Option<PackageVersionLookupRow> {
    versions
        .iter()
        .find(|entry| {
            entry.version == requested_version
                && normalize_release_channel(&entry.release_channel) == requested_channel
        })
        .cloned()
        .or_else(|| latest_version_for_channel(versions, requested_channel))
}

fn latest_version_for_channel(
    versions: &[PackageVersionLookupRow],
    channel: &str,
) -> Option<PackageVersionLookupRow> {
    let requested = normalize_release_channel(channel);
    versions
        .iter()
        .filter(|entry| normalize_release_channel(&entry.release_channel) == requested)
        .max_by(|left, right| left.published_at.cmp(&right.published_at))
        .cloned()
        .or_else(|| {
            versions
                .iter()
                .max_by(|left, right| left.published_at.cmp(&right.published_at))
                .cloned()
        })
}

fn normalize_release_channel(value: &str) -> String {
    let trimmed = value.trim().to_ascii_lowercase();
    if trimmed.is_empty() {
        "stable".to_string()
    } else {
        trimmed
    }
}

fn derive_repository_branch(fleet_name: &str, branch_name: &str) -> String {
    format!("release/{}/{}", slugify(fleet_name), slugify(branch_name))
}

fn slugify(value: &str) -> String {
    value
        .trim()
        .to_ascii_lowercase()
        .chars()
        .map(|char| {
            if char.is_ascii_alphanumeric() {
                char
            } else {
                '-'
            }
        })
        .collect::<String>()
        .split('-')
        .filter(|segment| !segment.is_empty())
        .collect::<Vec<_>>()
        .join("-")
}

fn to_decode_sqlx_error(cause: String) -> sqlx::Error {
    sqlx::Error::Decode(Box::new(std::io::Error::new(
        std::io::ErrorKind::InvalidData,
        cause,
    )))
}
