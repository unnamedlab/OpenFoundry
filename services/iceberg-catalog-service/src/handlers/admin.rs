//! Foundry-internal admin endpoints. These power the `/iceberg-tables`
//! UI in `apps/web` and aren't part of the Iceberg REST Catalog spec.
//!
//! Authentication still goes through the bearer extractor so the same
//! auth middleware applies — but with `read` scope sufficing for every
//! admin endpoint (none of them mutate data).

use axum::extract::{Path, Query, State};
use axum::{Json, http::StatusCode};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::Row;
use uuid::Uuid;

use crate::AppState;
use crate::domain::branch;
use crate::domain::metadata;
use crate::domain::namespace::decode_path;
use crate::domain::snapshot;
use crate::domain::table;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;
use crate::metrics;

#[derive(Debug, Deserialize)]
pub struct ListIcebergTablesQuery {
    #[serde(default)]
    pub project_rid: Option<String>,
    #[serde(default)]
    pub namespace: Option<String>,
    #[serde(default)]
    pub name: Option<String>,
    #[serde(default)]
    pub sort: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct IcebergTableSummary {
    pub id: Uuid,
    pub rid: String,
    pub project_rid: String,
    pub namespace: Vec<String>,
    pub name: String,
    pub format_version: i32,
    pub location: String,
    pub markings: Vec<String>,
    pub last_snapshot_at: Option<DateTime<Utc>>,
    pub row_count_estimate: Option<i64>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Serialize)]
pub struct IcebergTableListResponse {
    pub tables: Vec<IcebergTableSummary>,
}

pub async fn list_iceberg_tables(
    State(state): State<AppState>,
    Query(query): Query<ListIcebergTablesQuery>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<IcebergTableListResponse>, ApiError> {
    let mut sql = String::from(
        r#"
        SELECT t.id, t.rid, n.project_rid, n.name AS namespace_name, t.name,
               t.format_version, t.location, t.markings, t.created_at,
               (SELECT MAX(timestamp_ms) FROM iceberg_snapshots WHERE table_id = t.id) AS last_ts_ms,
               (SELECT (summary->>'total-records')::BIGINT
                  FROM iceberg_snapshots
                  WHERE table_id = t.id
                  ORDER BY timestamp_ms DESC LIMIT 1) AS row_count_estimate
        FROM iceberg_tables t
        JOIN iceberg_namespaces n ON n.id = t.namespace_id
        WHERE 1 = 1
        "#,
    );
    let mut binds: Vec<String> = Vec::new();
    if let Some(prid) = &query.project_rid {
        sql.push_str(&format!(" AND n.project_rid = ${}", binds.len() + 1));
        binds.push(prid.clone());
    }
    if let Some(ns) = &query.namespace {
        sql.push_str(&format!(" AND n.name = ${}", binds.len() + 1));
        binds.push(ns.clone());
    }
    if let Some(name) = &query.name {
        sql.push_str(&format!(" AND t.name ILIKE ${}", binds.len() + 1));
        binds.push(format!("%{name}%"));
    }
    sql.push_str(" ORDER BY ");
    sql.push_str(match query.sort.as_deref() {
        Some("name") => "t.name ASC",
        Some("created_at") => "t.created_at DESC",
        _ => "t.updated_at DESC",
    });

    let mut q = sqlx::query(&sql);
    for bind in binds {
        q = q.bind(bind);
    }
    let rows = q
        .fetch_all(&state.iceberg.db)
        .await
        .map_err(ApiError::from)?;

    let tables = rows
        .into_iter()
        .map(|row| {
            let namespace_name: String = row.try_get("namespace_name").unwrap_or_default();
            let last_ts_ms: Option<i64> = row.try_get("last_ts_ms").ok().flatten();
            IcebergTableSummary {
                id: row.try_get("id").unwrap_or(Uuid::nil()),
                rid: row.try_get("rid").unwrap_or_default(),
                project_rid: row.try_get("project_rid").unwrap_or_default(),
                namespace: decode_path(&namespace_name),
                name: row.try_get("name").unwrap_or_default(),
                format_version: row.try_get("format_version").unwrap_or(2),
                location: row.try_get("location").unwrap_or_default(),
                markings: row.try_get("markings").unwrap_or_default(),
                last_snapshot_at: last_ts_ms.and_then(|ms| DateTime::<Utc>::from_timestamp_millis(ms)),
                row_count_estimate: row.try_get("row_count_estimate").ok().flatten(),
                created_at: row.try_get("created_at").unwrap_or_else(|_| Utc::now()),
            }
        })
        .collect();

    metrics::record_rest_request("GET", "/api/v1/iceberg-tables", 200);
    Ok(Json(IcebergTableListResponse { tables }))
}

#[derive(Debug, Serialize)]
pub struct IcebergTableDetail {
    pub summary: IcebergTableSummary,
    pub schema: Value,
    pub properties: Value,
    pub partition_spec: Value,
    pub sort_order: Value,
    pub current_metadata_location: Option<String>,
    pub current_snapshot_id: Option<i64>,
    pub last_sequence_number: i64,
}

pub async fn get_iceberg_table_detail(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<IcebergTableDetail>, ApiError> {
    let tab = table::fetch_by_rid(
        &state.iceberg.db,
        &format!("ri.foundry.main.iceberg-table.{id}"),
    )
    .await?;
    let snapshots = snapshot::list_for_table(&state.iceberg.db, tab.id).await?;
    let last_snapshot_at = snapshots
        .iter()
        .map(|s| s.timestamp_ms)
        .max()
        .and_then(DateTime::<Utc>::from_timestamp_millis);
    let row_count = snapshots
        .last()
        .and_then(|s| s.summary.get("total-records"))
        .and_then(Value::as_str)
        .and_then(|s| s.parse::<i64>().ok());

    let project_rid = sqlx::query_scalar::<_, String>(
        "SELECT project_rid FROM iceberg_namespaces WHERE id = $1",
    )
    .bind(tab.namespace_id)
    .fetch_one(&state.iceberg.db)
    .await
    .unwrap_or_default();

    let summary = IcebergTableSummary {
        id: tab.id,
        rid: tab.rid.clone(),
        project_rid,
        namespace: tab.namespace_path.clone(),
        name: tab.name.clone(),
        format_version: tab.format_version,
        location: tab.location.clone(),
        markings: tab.markings.clone(),
        last_snapshot_at,
        row_count_estimate: row_count,
        created_at: Utc::now(),
    };

    Ok(Json(IcebergTableDetail {
        schema: tab.schema_json.clone(),
        properties: tab.properties.clone(),
        partition_spec: tab.partition_spec.clone(),
        sort_order: tab.sort_order.clone(),
        current_metadata_location: tab.current_metadata_location.clone(),
        current_snapshot_id: tab.current_snapshot_id,
        last_sequence_number: tab.last_sequence_number,
        summary,
    }))
}

#[derive(Debug, Serialize)]
pub struct SnapshotEntry {
    pub snapshot_id: i64,
    pub parent_snapshot_id: Option<i64>,
    pub operation: String,
    pub timestamp: Option<DateTime<Utc>>,
    pub sequence_number: i64,
    pub manifest_list: String,
    pub schema_id: i32,
    pub summary: Value,
}

#[derive(Debug, Serialize)]
pub struct SnapshotListResponse {
    pub snapshots: Vec<SnapshotEntry>,
}

pub async fn list_iceberg_table_snapshots(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<SnapshotListResponse>, ApiError> {
    let tab = table::fetch_by_rid(
        &state.iceberg.db,
        &format!("ri.foundry.main.iceberg-table.{id}"),
    )
    .await?;
    let snapshots = snapshot::list_for_table(&state.iceberg.db, tab.id).await?;

    let entries = snapshots
        .into_iter()
        .map(|s| SnapshotEntry {
            snapshot_id: s.snapshot_id,
            parent_snapshot_id: s.parent_snapshot_id,
            operation: s.operation,
            timestamp: DateTime::<Utc>::from_timestamp_millis(s.timestamp_ms),
            sequence_number: s.sequence_number,
            manifest_list: s.manifest_list_location,
            schema_id: s.schema_id,
            summary: s.summary,
        })
        .collect();

    Ok(Json(SnapshotListResponse { snapshots: entries }))
}

#[derive(Debug, Serialize)]
pub struct MetadataResponse {
    pub metadata: Value,
    pub metadata_location: String,
    pub history: Vec<MetadataHistoryEntry>,
}

#[derive(Debug, Serialize)]
pub struct MetadataHistoryEntry {
    pub version: i32,
    pub path: String,
    pub created_at: DateTime<Utc>,
}

pub async fn get_iceberg_table_metadata(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<MetadataResponse>, ApiError> {
    let tab = table::fetch_by_rid(
        &state.iceberg.db,
        &format!("ri.foundry.main.iceberg-table.{id}"),
    )
    .await?;
    let snapshots = snapshot::list_for_table(&state.iceberg.db, tab.id).await?;
    let document = metadata::build_metadata_v2(&tab, &snapshots).into_value();
    let metadata_location = tab
        .current_metadata_location
        .clone()
        .unwrap_or_else(|| format!("{}/metadata/v1.metadata.json", tab.location));

    let history = sqlx::query(
        r#"
        SELECT version, path, created_at
        FROM iceberg_table_metadata_files
        WHERE table_id = $1
        ORDER BY version DESC
        "#,
    )
    .bind(tab.id)
    .fetch_all(&state.iceberg.db)
    .await
    .map_err(ApiError::from)?;
    let history: Vec<MetadataHistoryEntry> = history
        .into_iter()
        .map(|row| MetadataHistoryEntry {
            version: row.try_get("version").unwrap_or(1),
            path: row.try_get("path").unwrap_or_default(),
            created_at: row.try_get("created_at").unwrap_or_else(|_| Utc::now()),
        })
        .collect();

    Ok(Json(MetadataResponse {
        metadata: document,
        metadata_location,
        history,
    }))
}

#[derive(Debug, Serialize)]
pub struct BranchEntry {
    pub name: String,
    pub kind: String,
    pub snapshot_id: i64,
}

#[derive(Debug, Serialize)]
pub struct BranchListResponse {
    pub branches: Vec<BranchEntry>,
}

pub async fn list_iceberg_table_branches(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<BranchListResponse>, ApiError> {
    let tab = table::fetch_by_rid(
        &state.iceberg.db,
        &format!("ri.foundry.main.iceberg-table.{id}"),
    )
    .await?;
    let branches = branch::list(&state.iceberg.db, tab.id).await.map_err(|err| {
        ApiError::Internal(format!("branch listing failed: {err}"))
    })?;

    let entries = branches
        .into_iter()
        .map(|b| BranchEntry {
            name: b.name,
            kind: b.kind,
            snapshot_id: b.snapshot_id,
        })
        .collect();

    Ok(Json(BranchListResponse { branches: entries }))
}

#[allow(dead_code)]
pub async fn placeholder_status() -> StatusCode {
    StatusCode::OK
}
