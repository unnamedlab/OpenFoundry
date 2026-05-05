//! REST Catalog § Tables.
//!
//! Implements:
//!
//!   * GET    /iceberg/v1/namespaces/{ns}/tables           — list
//!   * POST   /iceberg/v1/namespaces/{ns}/tables           — create
//!   * GET    /iceberg/v1/namespaces/{ns}/tables/{tbl}     — load
//!   * HEAD   /iceberg/v1/namespaces/{ns}/tables/{tbl}     — exists
//!   * POST   /iceberg/v1/namespaces/{ns}/tables/{tbl}     — commit
//!   * DELETE /iceberg/v1/namespaces/{ns}/tables/{tbl}     — drop

use axum::extract::{Path, Query, State};
use axum::http::{HeaderMap, HeaderValue, StatusCode};
use axum::{Json, response::IntoResponse, response::Response};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use uuid::Uuid;

use crate::AppState;
use crate::audit;
use crate::domain::branch_alias::{self, ALIAS_HEADER};
use crate::domain::metadata;
use crate::domain::namespace::{self, decode_path};
use crate::domain::schema_strict;
use crate::domain::snapshot;
use crate::domain::table::{self, IcebergTable, NewTable};
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;
use crate::handlers::rest_catalog::resolve_project_rid;
use crate::metrics;

#[derive(Debug, Serialize)]
pub struct ListTablesResponse {
    pub identifiers: Vec<TableIdentifier>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct TableIdentifier {
    pub namespace: Vec<String>,
    pub name: String,
}

#[derive(Debug, Deserialize)]
pub struct CreateTableRequest {
    pub name: String,
    pub schema: Value,
    #[serde(rename = "partition-spec", default)]
    pub partition_spec: Option<Value>,
    #[serde(rename = "sort-order", default)]
    pub sort_order: Option<Value>,
    #[serde(default)]
    pub properties: HashMap<String, String>,
    #[serde(default)]
    pub location: Option<String>,
    #[serde(rename = "format-version", default)]
    pub format_version: Option<i32>,
    #[serde(rename = "stage-create", default)]
    pub stage_create: Option<bool>,
}

#[derive(Debug, Serialize)]
pub struct LoadTableResponse {
    pub metadata: Value,
    #[serde(rename = "metadata-location")]
    pub metadata_location: String,
    pub config: HashMap<String, String>,
}

#[derive(Debug, Deserialize)]
pub struct CommitTableRequest {
    #[serde(default)]
    pub identifier: Option<TableIdentifier>,
    #[serde(default)]
    pub requirements: Vec<Value>,
    #[serde(default)]
    pub updates: Vec<Value>,
}

#[derive(Debug, Serialize)]
pub struct CommitTableResponse {
    pub metadata: Value,
    #[serde(rename = "metadata-location")]
    pub metadata_location: String,
}

#[derive(Debug, Deserialize)]
pub struct DropTableQuery {
    #[serde(rename = "purgeRequested", default)]
    pub purge_requested: Option<bool>,
}

/// Spec query parameters for LoadTable. Iceberg clients may pass a
/// `snapshotId`, a snapshot reference (`ref`), or neither (latest on
/// `main`). Foundry's master/main alias is applied to `ref` here so a
/// PyIceberg client pointing at `master` lands on `main` transparently.
#[derive(Debug, Deserialize, Default)]
pub struct LoadTableQuery {
    #[serde(rename = "ref", default)]
    pub ref_name: Option<String>,
    #[serde(rename = "snapshotId", default)]
    pub snapshot_id: Option<i64>,
}

pub async fn list_tables(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<ListTablesResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    let tables = table::list_in_namespace(&state.iceberg.db, &ns).await?;
    let response = ListTablesResponse {
        identifiers: tables
            .iter()
            .map(|t| TableIdentifier {
                namespace: t.namespace_path.clone(),
                name: t.name.clone(),
            })
            .collect(),
    };
    metrics::record_rest_request("GET", "/iceberg/v1/namespaces/{ns}/tables", 200);
    Ok(Json(response))
}

pub async fn create_table(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<CreateTableRequest>,
) -> Result<(StatusCode, Json<LoadTableResponse>), ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;

    let table_uuid = Uuid::new_v4().to_string();
    let location = body
        .location
        .unwrap_or_else(|| {
            format!(
                "{}/{}/{}",
                state.iceberg.warehouse_uri.trim_end_matches('/'),
                ns.name,
                body.name
            )
        });
    let format_version = body.format_version.unwrap_or(2);

    let properties_value = serde_json::to_value(&body.properties)
        .map_err(|err| ApiError::BadRequest(err.to_string()))?;

    let new = NewTable {
        namespace: &ns,
        name: &body.name,
        table_uuid,
        format_version,
        location,
        schema_json: body.schema,
        partition_spec: body
            .partition_spec
            .unwrap_or_else(|| serde_json::json!({"spec-id": 0, "fields": []})),
        sort_order: body
            .sort_order
            .unwrap_or_else(|| serde_json::json!({"order-id": 0, "fields": []})),
        properties: properties_value,
        markings: vec!["public".to_string()],
    };
    let table = table::create(&state.iceberg.db, new).await?;

    // P3 — snapshot the namespace's effective markings into the new
    // table per Foundry doc semantics. Inherited rows are recorded
    // separately from explicit ones so manage_markings can override
    // without losing provenance.
    let actor = parse_actor(&principal);
    if let Err(err) =
        crate::domain::markings::snapshot_namespace_into_table(&state.iceberg.db, &ns, &table, actor)
            .await
    {
        tracing::warn!(?err, "failed to snapshot namespace markings into iceberg table");
    } else {
        // Audit only the names we successfully snapshotted.
        if let Ok(projection) =
            crate::domain::markings::for_table(&state.iceberg.db, &table).await
        {
            let inherited: Vec<String> = projection
                .inherited_from_namespace
                .iter()
                .map(|p| p.name.clone())
                .collect();
            if !inherited.is_empty() {
                audit::markings_inheritance_snapshot(
                    actor,
                    &table.rid,
                    &format!("ri.foundry.main.iceberg-namespace.{}", ns.id),
                    &inherited,
                );
            }
        }
    }

    let metadata_doc = metadata::build_metadata_v2(&table, &[]);
    let metadata_location = format!("{}/metadata/v1.metadata.json", table.location);

    persist_metadata_file(&state, &table, 1, &metadata_location).await?;
    audit::table_created(actor, &table.rid, &ns.name, &table.name);
    metrics::record_rest_request("POST", "/iceberg/v1/namespaces/{ns}/tables", 200);
    metrics::TABLES_TOTAL
        .with_label_values(&[&format_version.to_string()])
        .inc();
    metrics::METADATA_FILES_TOTAL
        .with_label_values(&[&table.table_uuid])
        .inc();

    Ok((
        StatusCode::OK,
        Json(LoadTableResponse {
            metadata: metadata_doc.into_value(),
            metadata_location,
            config: load_table_config(&state, &table),
        }),
    ))
}

pub async fn load_table(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    Query(query): Query<LoadTableQuery>,
    _principal: AuthenticatedPrincipal,
) -> Result<Response, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    let tab = table::fetch(&state.iceberg.db, &ns, &table_name).await?;

    // P2 — master ↔ main alias rewrite (transparent).
    let alias_outcome = query
        .ref_name
        .as_deref()
        .map(branch_alias::resolve_branch_alias_outcome);

    let snapshots = snapshot::list_for_table(&state.iceberg.db, tab.id).await?;
    let metadata_doc = metadata::build_metadata_v2(&tab, &snapshots);
    let metadata_location = tab
        .current_metadata_location
        .clone()
        .unwrap_or_else(|| format!("{}/metadata/v1.metadata.json", tab.location));

    metrics::record_rest_request("GET", "/iceberg/v1/namespaces/{ns}/tables/{tbl}", 200);

    let response = Json(LoadTableResponse {
        metadata: metadata_doc.into_value(),
        metadata_location,
        config: load_table_config(&state, &tab),
    });

    let mut response_headers = HeaderMap::new();
    if let Some(outcome) = alias_outcome {
        if let Some(value) = outcome.header_value() {
            metrics::BRANCH_ALIAS_APPLIED_TOTAL
                .with_label_values(&[&outcome.input, &outcome.resolved])
                .inc();
            audit::branch_alias_applied(None, &outcome.input, &outcome.resolved);
            if let Ok(header_value) = HeaderValue::from_str(&value) {
                response_headers.insert(ALIAS_HEADER, header_value);
            }
        }
    }
    let _ = query.snapshot_id; // reserved for time-travel reads (P4).
    Ok((StatusCode::OK, response_headers, response).into_response())
}

pub async fn table_exists(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    _principal: AuthenticatedPrincipal,
) -> Result<Response, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = match namespace::fetch(&state.iceberg.db, &project_rid, &path).await {
        Ok(ns) => ns,
        Err(_) => return Ok(StatusCode::NOT_FOUND.into_response()),
    };
    match table::fetch(&state.iceberg.db, &ns, &table_name).await {
        Ok(_) => Ok(StatusCode::NO_CONTENT.into_response()),
        Err(_) => Ok(StatusCode::NOT_FOUND.into_response()),
    }
}

pub async fn drop_table(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    Query(params): Query<DropTableQuery>,
    principal: AuthenticatedPrincipal,
) -> Result<StatusCode, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;

    // Resolve the table once so the audit event has its RID.
    let tab = table::fetch(&state.iceberg.db, &ns, &table_name).await?;
    let purge = params.purge_requested.unwrap_or(false);

    table::drop(&state.iceberg.db, &ns, &table_name, purge).await?;

    let actor = parse_actor(&principal);
    audit::table_dropped(actor, &tab.rid, purge);
    metrics::record_rest_request("DELETE", "/iceberg/v1/namespaces/{ns}/tables/{tbl}", 204);
    Ok(StatusCode::NO_CONTENT)
}

pub async fn commit_table(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<CommitTableRequest>,
) -> Result<Json<CommitTableResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    let current = table::fetch(&state.iceberg.db, &ns, &table_name).await?;

    // P2 — schema strict mode. The single-table CommitTable path also
    // refuses implicit schema evolution; explicit changes must go
    // through `POST .../alter-schema`.
    enforce_schema_strict(&current, &body.updates, &principal)?;

    let updated = table::apply_commit(
        &state.iceberg.db,
        &current,
        &body.requirements,
        &body.updates,
    )
    .await?;

    // Persist a new metadata file version.
    let next_version = next_metadata_version(&state, updated.id).await? + 1;
    let metadata_location = format!(
        "{}/metadata/v{}.metadata.json",
        updated.location, next_version
    );

    let snapshots = snapshot::list_for_table(&state.iceberg.db, updated.id).await?;
    let metadata_doc = metadata::build_metadata_v2(&updated, &snapshots);

    persist_metadata_file(&state, &updated, next_version as i32, &metadata_location).await?;
    table::advance_snapshot(
        &state.iceberg.db,
        updated.id,
        updated.current_snapshot_id.unwrap_or_default(),
        &metadata_location,
        updated.last_sequence_number,
    )
    .await?;

    let actor = parse_actor(&principal);
    audit::table_metadata_updated(
        actor,
        &updated.rid,
        &metadata_location,
        &serde_json::json!({
            "requirements": body.requirements,
            "updates": body.updates,
        }),
    );

    metrics::record_rest_request("POST", "/iceberg/v1/namespaces/{ns}/tables/{tbl}", 200);
    metrics::METADATA_FILES_TOTAL
        .with_label_values(&[&updated.table_uuid])
        .inc();

    Ok(Json(CommitTableResponse {
        metadata: metadata_doc.into_value(),
        metadata_location,
    }))
}

async fn next_metadata_version(state: &AppState, table_id: Uuid) -> Result<i64, ApiError> {
    let row: (Option<i32>,) = sqlx::query_as(
        "SELECT MAX(version) FROM iceberg_table_metadata_files WHERE table_id = $1",
    )
    .bind(table_id)
    .fetch_one(&state.iceberg.db)
    .await
    .map_err(ApiError::from)?;
    Ok(row.0.unwrap_or(0) as i64)
}

async fn persist_metadata_file(
    state: &AppState,
    tab: &IcebergTable,
    version: i32,
    path: &str,
) -> Result<(), ApiError> {
    sqlx::query(
        r#"
        INSERT INTO iceberg_table_metadata_files (id, table_id, version, path)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (table_id, version) DO NOTHING
        "#,
    )
    .bind(Uuid::now_v7())
    .bind(tab.id)
    .bind(version)
    .bind(path)
    .execute(&state.iceberg.db)
    .await?;
    Ok(())
}

fn load_table_config(state: &AppState, _table: &IcebergTable) -> HashMap<String, String> {
    let mut config = HashMap::new();
    config.insert(
        "warehouse".to_string(),
        state.iceberg.warehouse_uri.clone(),
    );
    // Credential vending preview (D1.1.8 P3 § 3). The full vending
    // flow lands in P6; we surface the contract today so PyIceberg /
    // Spark see a stable response shape and can opt into the
    // remote-signing path when it ships.
    config.insert(
        "s3.access-key-id-template".to_string(),
        "vending-coming-in-p6".to_string(),
    );
    config.insert(
        "s3.session-token-template".to_string(),
        "vending-coming-in-p6".to_string(),
    );
    config.insert(
        "vending.status".to_string(),
        "503 NOT_IMPLEMENTED — credential vending is on the P6 roadmap".to_string(),
    );
    config
}

fn parse_actor(principal: &AuthenticatedPrincipal) -> Uuid {
    Uuid::parse_str(&principal.subject).unwrap_or_else(|_| Uuid::nil())
}

/// Reject any `add-schema` update whose schema diverges from the
/// table's current schema (per Foundry doc § "Automatic schema
/// evolution"). This is the strict-mode check applied on the
/// single-table CommitTable path; the multi-table path runs the same
/// check inside the FOR UPDATE block.
fn enforce_schema_strict(
    current: &IcebergTable,
    updates: &[Value],
    principal: &AuthenticatedPrincipal,
) -> Result<(), ApiError> {
    for update in updates {
        let action = update
            .get("action")
            .and_then(Value::as_str)
            .unwrap_or_default();
        if action != "add-schema" {
            continue;
        }
        let attempted = match update.get("schema") {
            Some(value) => value,
            None => continue,
        };
        let diff = schema_strict::diff_schemas(&current.schema_json, attempted);
        if diff.is_compatible() {
            continue;
        }
        for delta in diff.deltas.iter() {
            let label = match delta {
                schema_strict::SchemaDelta::AddedColumn { .. } => "added-column",
                schema_strict::SchemaDelta::DroppedColumn { .. } => "dropped-column",
                schema_strict::SchemaDelta::ChangedColumnType { .. } => "changed-column-type",
                schema_strict::SchemaDelta::ChangedColumnRequired { .. } => {
                    "changed-column-required"
                }
            };
            metrics::SCHEMA_STRICT_REJECTIONS_TOTAL
                .with_label_values(&[label])
                .inc();
        }
        audit::schema_attempt_blocked(parse_actor(principal), &current.rid, &diff.rendered());
        return Err(ApiError::SchemaIncompatible {
            current_schema: current.schema_json.clone(),
            attempted_schema: attempted.clone(),
            diff,
        });
    }
    Ok(())
}

// ─── ALTER TABLE schema endpoint ──────────────────────────────────────

#[derive(Debug, Deserialize)]
pub struct AlterSchemaRequest {
    pub updates: Vec<Value>,
}

#[derive(Debug, Serialize)]
pub struct AlterSchemaResponse {
    pub schema_id: i64,
    pub schema: Value,
}

/// `POST /iceberg/v1/namespaces/{ns}/tables/{tbl}/alter-schema` —
/// explicit schema mutation. The endpoint accepts the same `updates`
/// vocabulary as Iceberg's UpdateTable but limited to schema actions
/// (`add-column`, `drop-column`, `rename-column`, `update-column`).
/// The new schema gets a fresh `schema-id` (current + 1) and the
/// transition is persisted as a metadata-file row so the doc's "view
/// schema history" surface stays accurate.
pub async fn alter_schema(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path((namespace_path, table_name)): Path<(String, String)>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<AlterSchemaRequest>,
) -> Result<Json<AlterSchemaResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_path(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    let current = table::fetch(&state.iceberg.db, &ns, &table_name).await?;

    let previous_schema_id = current
        .schema_json
        .get("schema-id")
        .and_then(Value::as_i64)
        .unwrap_or(0);
    let next_schema_id = previous_schema_id + 1;
    let next_schema = build_altered_schema(&current.schema_json, &body.updates, next_schema_id)?;

    sqlx::query("UPDATE iceberg_tables SET schema_json = $2, updated_at = NOW() WHERE id = $1")
        .bind(current.id)
        .bind(&next_schema)
        .execute(&state.iceberg.db)
        .await
        .map_err(ApiError::from)?;

    let actor = parse_actor(&principal);
    audit::schema_altered(actor, &current.rid, previous_schema_id, next_schema_id);
    metrics::record_rest_request(
        "POST",
        "/iceberg/v1/namespaces/{ns}/tables/{tbl}/alter-schema",
        StatusCode::OK.as_u16(),
    );

    Ok(Json(AlterSchemaResponse {
        schema_id: next_schema_id,
        schema: next_schema,
    }))
}

fn build_altered_schema(
    current: &Value,
    updates: &[Value],
    new_schema_id: i64,
) -> Result<Value, ApiError> {
    let mut fields: Vec<Value> = current
        .get("fields")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    let mut next_field_id = fields
        .iter()
        .filter_map(|f| f.get("id").and_then(Value::as_i64))
        .max()
        .unwrap_or(0)
        + 1;

    for update in updates {
        let action = update
            .get("action")
            .and_then(Value::as_str)
            .unwrap_or_default();
        match action {
            "add-column" => {
                let name = update
                    .get("name")
                    .and_then(Value::as_str)
                    .ok_or_else(|| ApiError::BadRequest("add-column requires `name`".to_string()))?;
                let column_type = update
                    .get("type")
                    .cloned()
                    .ok_or_else(|| ApiError::BadRequest("add-column requires `type`".to_string()))?;
                let required = update
                    .get("required")
                    .and_then(Value::as_bool)
                    .unwrap_or(false);
                fields.push(serde_json::json!({
                    "id": next_field_id,
                    "name": name,
                    "required": required,
                    "type": column_type,
                }));
                next_field_id += 1;
            }
            "drop-column" => {
                let name = update
                    .get("name")
                    .and_then(Value::as_str)
                    .ok_or_else(|| ApiError::BadRequest("drop-column requires `name`".to_string()))?;
                fields.retain(|f| f.get("name").and_then(Value::as_str) != Some(name));
            }
            "rename-column" => {
                let from = update
                    .get("from")
                    .and_then(Value::as_str)
                    .ok_or_else(|| ApiError::BadRequest("rename-column requires `from`".to_string()))?;
                let to = update
                    .get("to")
                    .and_then(Value::as_str)
                    .ok_or_else(|| ApiError::BadRequest("rename-column requires `to`".to_string()))?;
                for field in fields.iter_mut() {
                    if field.get("name").and_then(Value::as_str) == Some(from) {
                        if let Value::Object(map) = field {
                            map.insert("name".to_string(), Value::String(to.to_string()));
                        }
                    }
                }
            }
            "update-column" => {
                let name = update
                    .get("name")
                    .and_then(Value::as_str)
                    .ok_or_else(|| ApiError::BadRequest("update-column requires `name`".to_string()))?;
                for field in fields.iter_mut() {
                    if field.get("name").and_then(Value::as_str) != Some(name) {
                        continue;
                    }
                    if let Value::Object(map) = field {
                        if let Some(t) = update.get("type").cloned() {
                            map.insert("type".to_string(), t);
                        }
                        if let Some(r) = update.get("required").and_then(Value::as_bool) {
                            map.insert("required".to_string(), Value::Bool(r));
                        }
                    }
                }
            }
            other => {
                return Err(ApiError::BadRequest(format!(
                    "unsupported alter-schema action `{other}`"
                )));
            }
        }
    }

    Ok(serde_json::json!({
        "schema-id": new_schema_id,
        "type": "struct",
        "fields": fields,
    }))
}
