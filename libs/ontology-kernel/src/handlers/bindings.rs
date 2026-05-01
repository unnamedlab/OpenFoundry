//! `ObjectType` ↔ dataset binding handlers (Foundry "Models in the Ontology").
//!
//! Endpoints are mounted under `/object-types/:id/bindings` and provide:
//!
//! * CRUD over `object_type_bindings`.
//! * `POST /:bid/materialize` which reads the source dataset preview and
//!   projects rows into `object_instances` and `object_revisions`.
//!
//! The materialise path mirrors the existing
//! [`crate::handlers::funnel`] implementation but is declarative: the binding
//! itself stores the mapping rather than running a pipeline.
//!
//! # TODO
//! The dataset is currently fetched via the `dataset-service` HTTP preview
//! endpoint (same path used by the funnel). Future work will swap this for an
//! Apache Arrow Flight SQL client against `sql-warehousing-service` to support
//! large datasets without paginating the JSON preview.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use auth_middleware::{
    Claims,
    jwt::{build_access_claims, encode_token},
    layer::AuthUser,
};
use serde::Deserialize;
use serde_json::{Value, json};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        project_access::{
            OntologyResourceKind, ensure_resource_manage_access, load_resource_project_id,
        },
        schema::{load_effective_properties, validate_object_properties},
    },
    models::object_type_binding::{
        CreateObjectTypeBindingRequest, ListObjectTypeBindingsResponse,
        MaterializeBindingRequest, MaterializeBindingResponse, ObjectTypeBinding,
        ObjectTypeBindingPropertyMapping, ObjectTypeBindingRow, ObjectTypeBindingSyncMode,
        UpdateObjectTypeBindingRequest,
    },
    models::object_type::ObjectType,
};

// --- error helpers ---------------------------------------------------------

fn json_error(status: StatusCode, message: impl Into<String>) -> Response {
    (status, Json(json!({ "error": message.into() }))).into_response()
}

fn invalid(message: impl Into<String>) -> Response {
    json_error(StatusCode::BAD_REQUEST, message)
}

fn not_found(message: impl Into<String>) -> Response {
    json_error(StatusCode::NOT_FOUND, message)
}

fn forbidden(message: impl Into<String>) -> Response {
    json_error(StatusCode::FORBIDDEN, message)
}

fn internal(message: impl Into<String>) -> Response {
    json_error(StatusCode::INTERNAL_SERVER_ERROR, message)
}

// --- common loaders --------------------------------------------------------

async fn load_object_type(state: &AppState, id: Uuid) -> Result<Option<ObjectType>, String> {
    sqlx::query_as::<_, ObjectType>("SELECT * FROM object_types WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
        .map_err(|error| format!("failed to load object type: {error}"))
}

async fn load_binding(
    state: &AppState,
    object_type_id: Uuid,
    binding_id: Uuid,
) -> Result<Option<ObjectTypeBinding>, String> {
    let row = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"SELECT id, object_type_id, dataset_id, dataset_branch, dataset_version,
                  primary_key_column, property_mapping, sync_mode, default_marking,
                  preview_limit, owner_id, last_materialized_at, last_run_status,
                  last_run_summary, created_at, updated_at
           FROM object_type_bindings
           WHERE id = $1 AND object_type_id = $2"#,
    )
    .bind(binding_id)
    .bind(object_type_id)
    .fetch_optional(&state.db)
    .await
    .map_err(|error| format!("failed to load binding: {error}"))?;

    row.map(ObjectTypeBinding::try_from).transpose()
}

async fn ensure_can_manage(
    state: &AppState,
    claims: &Claims,
    object_type: &ObjectType,
) -> Result<(), Response> {
    if claims.has_role("admin") {
        return Ok(());
    }
    let project_id = load_resource_project_id(
        &state.db,
        OntologyResourceKind::ObjectType,
        object_type.id,
    )
    .await
    .map_err(|error| internal(format!("failed to load project binding: {error}")))?;
    ensure_resource_manage_access(&state.db, claims, object_type.owner_id, project_id)
        .await
        .map_err(forbidden)
}

async fn ensure_can_manage_by_id(
    state: &AppState,
    claims: &Claims,
    object_type_id: Uuid,
) -> Result<ObjectType, Response> {
    let object_type = match load_object_type(state, object_type_id).await {
        Ok(Some(ot)) => ot,
        Ok(None) => return Err(not_found("object type not found")),
        Err(error) => return Err(internal(error)),
    };
    ensure_can_manage(state, claims, &object_type).await?;
    Ok(object_type)
}

fn validate_marking(marking: &str) -> Result<(), String> {
    match marking {
        "public" | "internal" | "confidential" | "pii" | "restricted" => Ok(()),
        other => Err(format!(
            "marking '{other}' is not supported; expected one of: public, internal, confidential, pii, restricted"
        )),
    }
}

fn validate_mapping_targets(mapping: &[ObjectTypeBindingPropertyMapping]) -> Result<(), String> {
    use std::collections::HashSet;
    let mut seen = HashSet::new();
    for entry in mapping {
        if entry.source_field.trim().is_empty() {
            return Err("property_mapping.source_field cannot be empty".to_string());
        }
        if entry.target_property.trim().is_empty() {
            return Err("property_mapping.target_property cannot be empty".to_string());
        }
        if !seen.insert(entry.target_property.clone()) {
            return Err(format!(
                "property_mapping.target_property '{}' is duplicated",
                entry.target_property
            ));
        }
    }
    Ok(())
}

// --- CRUD ------------------------------------------------------------------

pub async fn create_object_type_binding(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(object_type_id): Path<Uuid>,
    Json(body): Json<CreateObjectTypeBindingRequest>,
) -> Response {
    let object_type = match ensure_can_manage_by_id(&state, &claims, object_type_id).await {
        Ok(ot) => ot,
        Err(response) => return response,
    };

    if body.primary_key_column.trim().is_empty() {
        return invalid("primary_key_column is required");
    }
    if let Err(error) = validate_mapping_targets(&body.property_mapping) {
        return invalid(error);
    }
    let marking = body
        .default_marking
        .clone()
        .unwrap_or_else(|| "public".to_string());
    if let Err(error) = validate_marking(&marking) {
        return invalid(error);
    }

    // If the target object_type declares its own primary key, ensure the
    // mapping projects something into it.
    if let Some(pk_property) = object_type.primary_key_property.as_ref() {
        let has_pk = body
            .property_mapping
            .iter()
            .any(|m| &m.target_property == pk_property);
        if !has_pk && body.property_mapping.is_empty() {
            // empty mapping = pass-through; that's allowed
        } else if !has_pk {
            return invalid(format!(
                "property_mapping must project to the object type's primary key property '{}'",
                pk_property
            ));
        }
    }

    let preview_limit = body.preview_limit.unwrap_or(1000).clamp(1, 100_000);
    let mapping_value = match serde_json::to_value(&body.property_mapping) {
        Ok(value) => value,
        Err(error) => return internal(format!("failed to encode property_mapping: {error}")),
    };

    let id = Uuid::now_v7();
    let row = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"INSERT INTO object_type_bindings (
               id, object_type_id, dataset_id, dataset_branch, dataset_version,
               primary_key_column, property_mapping, sync_mode, default_marking,
               preview_limit, owner_id
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
           RETURNING id, object_type_id, dataset_id, dataset_branch, dataset_version,
                     primary_key_column, property_mapping, sync_mode, default_marking,
                     preview_limit, owner_id, last_materialized_at, last_run_status,
                     last_run_summary, created_at, updated_at"#,
    )
    .bind(id)
    .bind(object_type_id)
    .bind(body.dataset_id)
    .bind(body.dataset_branch.as_deref())
    .bind(body.dataset_version)
    .bind(&body.primary_key_column)
    .bind(&mapping_value)
    .bind(body.sync_mode.as_str())
    .bind(&marking)
    .bind(preview_limit)
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await;

    match row {
        Ok(row) => match ObjectTypeBinding::try_from(row) {
            Ok(binding) => (StatusCode::CREATED, Json(binding)).into_response(),
            Err(error) => internal(error),
        },
        Err(sqlx::Error::Database(db_err)) if db_err.constraint().is_some() => {
            invalid(format!(
                "binding violates constraint '{}'",
                db_err.constraint().unwrap_or("unknown")
            ))
        }
        Err(error) => internal(format!("failed to insert binding: {error}")),
    }
}

pub async fn list_object_type_bindings(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(object_type_id): Path<Uuid>,
) -> Response {
    if let Err(response) = ensure_can_manage_by_id(&state, &claims, object_type_id).await {
        return response;
    }

    let rows = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"SELECT id, object_type_id, dataset_id, dataset_branch, dataset_version,
                  primary_key_column, property_mapping, sync_mode, default_marking,
                  preview_limit, owner_id, last_materialized_at, last_run_status,
                  last_run_summary, created_at, updated_at
           FROM object_type_bindings
           WHERE object_type_id = $1
           ORDER BY created_at DESC"#,
    )
    .bind(object_type_id)
    .fetch_all(&state.db)
    .await;

    let rows = match rows {
        Ok(rows) => rows,
        Err(error) => return internal(format!("failed to list bindings: {error}")),
    };

    let mut data = Vec::with_capacity(rows.len());
    for row in rows {
        match ObjectTypeBinding::try_from(row) {
            Ok(binding) => data.push(binding),
            Err(error) => return internal(error),
        }
    }
    Json(ListObjectTypeBindingsResponse { data }).into_response()
}

pub async fn get_object_type_binding(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((object_type_id, binding_id)): Path<(Uuid, Uuid)>,
) -> Response {
    if let Err(response) = ensure_can_manage_by_id(&state, &claims, object_type_id).await {
        return response;
    }
    match load_binding(&state, object_type_id, binding_id).await {
        Ok(Some(binding)) => Json(binding).into_response(),
        Ok(None) => not_found("binding not found"),
        Err(error) => internal(error),
    }
}

pub async fn update_object_type_binding(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((object_type_id, binding_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<UpdateObjectTypeBindingRequest>,
) -> Response {
    if let Err(response) = ensure_can_manage_by_id(&state, &claims, object_type_id).await {
        return response;
    }
    let existing = match load_binding(&state, object_type_id, binding_id).await {
        Ok(Some(binding)) => binding,
        Ok(None) => return not_found("binding not found"),
        Err(error) => return internal(error),
    };

    let dataset_branch = body.dataset_branch.clone().or(existing.dataset_branch);
    let dataset_version = body.dataset_version.or(existing.dataset_version);
    let primary_key_column = body
        .primary_key_column
        .clone()
        .unwrap_or(existing.primary_key_column);
    let property_mapping = body
        .property_mapping
        .clone()
        .unwrap_or(existing.property_mapping);
    if let Err(error) = validate_mapping_targets(&property_mapping) {
        return invalid(error);
    }
    let sync_mode = body.sync_mode.unwrap_or(existing.sync_mode);
    let default_marking = body
        .default_marking
        .clone()
        .unwrap_or(existing.default_marking);
    if let Err(error) = validate_marking(&default_marking) {
        return invalid(error);
    }
    let preview_limit = body
        .preview_limit
        .unwrap_or(existing.preview_limit)
        .clamp(1, 100_000);

    let mapping_value = match serde_json::to_value(&property_mapping) {
        Ok(value) => value,
        Err(error) => return internal(format!("failed to encode property_mapping: {error}")),
    };

    let row = sqlx::query_as::<_, ObjectTypeBindingRow>(
        r#"UPDATE object_type_bindings
           SET dataset_branch = $2,
               dataset_version = $3,
               primary_key_column = $4,
               property_mapping = $5,
               sync_mode = $6,
               default_marking = $7,
               preview_limit = $8,
               updated_at = NOW()
           WHERE id = $1
           RETURNING id, object_type_id, dataset_id, dataset_branch, dataset_version,
                     primary_key_column, property_mapping, sync_mode, default_marking,
                     preview_limit, owner_id, last_materialized_at, last_run_status,
                     last_run_summary, created_at, updated_at"#,
    )
    .bind(binding_id)
    .bind(dataset_branch.as_deref())
    .bind(dataset_version)
    .bind(&primary_key_column)
    .bind(&mapping_value)
    .bind(sync_mode.as_str())
    .bind(&default_marking)
    .bind(preview_limit)
    .fetch_one(&state.db)
    .await;

    match row {
        Ok(row) => match ObjectTypeBinding::try_from(row) {
            Ok(binding) => Json(binding).into_response(),
            Err(error) => internal(error),
        },
        Err(error) => internal(format!("failed to update binding: {error}")),
    }
}

pub async fn delete_object_type_binding(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((object_type_id, binding_id)): Path<(Uuid, Uuid)>,
) -> Response {
    if let Err(response) = ensure_can_manage_by_id(&state, &claims, object_type_id).await {
        return response;
    }
    match sqlx::query(
        "DELETE FROM object_type_bindings WHERE id = $1 AND object_type_id = $2",
    )
    .bind(binding_id)
    .bind(object_type_id)
    .execute(&state.db)
    .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => not_found("binding not found"),
        Err(error) => internal(format!("failed to delete binding: {error}")),
    }
}

// --- Materialise -----------------------------------------------------------

#[derive(Debug, Deserialize)]
struct DatasetPreviewPayload {
    #[serde(default)]
    rows: Vec<Value>,
}

fn issue_service_token(state: &AppState, claims: &Claims) -> Result<String, String> {
    let service_claims = build_access_claims(
        &state.jwt_config,
        Uuid::now_v7(),
        "ontology-service@internal.openfoundry",
        "ontology-service",
        vec!["admin".to_string()],
        vec!["*:*".to_string()],
        claims.org_id,
        json!({
            "service": "ontology-service",
            "classification_clearance": "pii",
            "impersonated_actor_id": claims.sub,
        }),
        vec!["service".to_string()],
    );
    let token = encode_token(&state.jwt_config, &service_claims)
        .map_err(|error| format!("failed to issue service token: {error}"))?;
    Ok(format!("Bearer {token}"))
}

async fn fetch_dataset_preview(
    state: &AppState,
    claims: &Claims,
    binding: &ObjectTypeBinding,
    limit: i32,
    branch: Option<&str>,
    version: Option<i32>,
) -> Result<DatasetPreviewPayload, String> {
    let auth_header = issue_service_token(state, claims)?;
    let mut url = reqwest::Url::parse(&format!(
        "{}/api/v1/datasets/{}/preview",
        state.dataset_service_url, binding.dataset_id
    ))
    .map_err(|error| format!("failed to build dataset preview URL: {error}"))?;
    {
        let mut query = url.query_pairs_mut();
        query.append_pair("limit", &limit.to_string());
        if let Some(branch) = branch {
            query.append_pair("branch", branch);
        }
        if let Some(version) = version {
            query.append_pair("version", &version.to_string());
        }
    }

    let response = state
        .http_client
        .get(url)
        .header(reqwest::header::AUTHORIZATION, auth_header)
        .send()
        .await
        .map_err(|error| format!("failed to fetch dataset preview: {error}"))?;
    let status = response.status();
    let body = response
        .text()
        .await
        .map_err(|error| format!("failed to read dataset preview response: {error}"))?;
    if !status.is_success() {
        return Err(format!(
            "dataset preview failed with HTTP {}: {}",
            status.as_u16(),
            body
        ));
    }
    serde_json::from_str(&body)
        .map_err(|error| format!("failed to decode dataset preview payload: {error}"))
}

fn project_row(
    row: &Value,
    mapping: &[ObjectTypeBindingPropertyMapping],
) -> Result<Value, String> {
    let Some(object) = row.as_object() else {
        return Err("dataset preview row is not a JSON object".to_string());
    };
    if mapping.is_empty() {
        return Ok(Value::Object(object.clone()));
    }
    let mut projected = serde_json::Map::new();
    for entry in mapping {
        if let Some(value) = object.get(&entry.source_field) {
            projected.insert(entry.target_property.clone(), value.clone());
        }
    }
    Ok(Value::Object(projected))
}

fn extract_primary_key(row: &Value, primary_key_column: &str) -> Result<String, String> {
    let value = row
        .get(primary_key_column)
        .ok_or_else(|| format!("row is missing primary key column '{primary_key_column}'"))?;
    match value {
        Value::Null => Err(format!(
            "primary key column '{primary_key_column}' cannot be null"
        )),
        Value::String(value) => Ok(value.clone()),
        other => serde_json::to_string(other)
            .map_err(|error| format!("failed to serialize primary key: {error}")),
    }
}

async fn find_existing_object_id(
    state: &AppState,
    object_type_id: Uuid,
    primary_key_property: &str,
    primary_key_value: &str,
) -> Result<Option<Uuid>, String> {
    sqlx::query_scalar::<_, Uuid>(
        r#"SELECT id FROM object_instances
           WHERE object_type_id = $1
             AND properties ->> $2 = $3
           ORDER BY updated_at DESC
           LIMIT 1"#,
    )
    .bind(object_type_id)
    .bind(primary_key_property)
    .bind(primary_key_value)
    .fetch_optional(&state.db)
    .await
    .map_err(|error| format!("failed to look up existing object: {error}"))
}

async fn next_revision_number(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    object_id: Uuid,
) -> Result<i64, String> {
    let max: Option<i64> = sqlx::query_scalar(
        "SELECT MAX(revision_number) FROM object_revisions WHERE object_id = $1",
    )
    .bind(object_id)
    .fetch_one(&mut **tx)
    .await
    .map_err(|error| format!("failed to read revision counter: {error}"))?;
    Ok(max.unwrap_or(0) + 1)
}

async fn write_object_revision(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    object_id: Uuid,
    object_type_id: Uuid,
    operation: &str,
    properties: &Value,
    marking: &str,
    organization_id: Option<Uuid>,
    changed_by: Uuid,
) -> Result<(), String> {
    let revision_number = next_revision_number(tx, object_id).await?;
    sqlx::query(
        r#"INSERT INTO object_revisions
            (id, object_id, object_type_id, operation, properties, marking,
             organization_id, changed_by, revision_number)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)"#,
    )
    .bind(Uuid::now_v7())
    .bind(object_id)
    .bind(object_type_id)
    .bind(operation)
    .bind(properties)
    .bind(marking)
    .bind(organization_id)
    .bind(changed_by)
    .bind(revision_number)
    .execute(&mut **tx)
    .await
    .map_err(|error| format!("failed to write object_revisions row: {error}"))?;
    Ok(())
}

async fn upsert_instance(
    state: &AppState,
    claims: &Claims,
    binding: &ObjectTypeBinding,
    object_id: Option<Uuid>,
    properties: &Value,
) -> Result<&'static str, String> {
    let mut tx = state
        .db
        .begin()
        .await
        .map_err(|error| format!("failed to start transaction: {error}"))?;

    let (object_id, operation) = if let Some(id) = object_id {
        sqlx::query(
            r#"UPDATE object_instances
               SET properties = $2,
                   marking = $3,
                   updated_at = NOW()
               WHERE id = $1"#,
        )
        .bind(id)
        .bind(properties)
        .bind(&binding.default_marking)
        .execute(&mut *tx)
        .await
        .map_err(|error| format!("failed to update object instance: {error}"))?;
        (id, "update")
    } else {
        let new_id = Uuid::now_v7();
        sqlx::query(
            r#"INSERT INTO object_instances
                (id, object_type_id, properties, created_by, organization_id, marking)
               VALUES ($1, $2, $3, $4, $5, $6)"#,
        )
        .bind(new_id)
        .bind(binding.object_type_id)
        .bind(properties)
        .bind(claims.sub)
        .bind(claims.org_id)
        .bind(&binding.default_marking)
        .execute(&mut *tx)
        .await
        .map_err(|error| format!("failed to insert object instance: {error}"))?;
        (new_id, "insert")
    };

    write_object_revision(
        &mut tx,
        object_id,
        binding.object_type_id,
        operation,
        properties,
        &binding.default_marking,
        claims.org_id,
        claims.sub,
    )
    .await?;

    tx.commit()
        .await
        .map_err(|error| format!("failed to commit binding upsert: {error}"))?;
    Ok(operation)
}

pub async fn materialize_object_type_binding(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((object_type_id, binding_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<MaterializeBindingRequest>,
) -> Response {
    let object_type = match ensure_can_manage_by_id(&state, &claims, object_type_id).await {
        Ok(ot) => ot,
        Err(response) => return response,
    };

    let binding = match load_binding(&state, object_type_id, binding_id).await {
        Ok(Some(binding)) => binding,
        Ok(None) => return not_found("binding not found"),
        Err(error) => return internal(error),
    };

    if binding.sync_mode == ObjectTypeBindingSyncMode::View {
        return invalid(
            "view-mode bindings are read-through; materialise is not applicable".to_string(),
        );
    }

    let Some(primary_key_property) = object_type.primary_key_property.clone() else {
        return invalid(
            "object type must define primary_key_property to materialise a binding".to_string(),
        );
    };

    let definitions = match load_effective_properties(&state.db, object_type_id).await {
        Ok(definitions) => definitions,
        Err(error) => return internal(format!("failed to load object type properties: {error}")),
    };

    let limit = body
        .limit
        .unwrap_or(binding.preview_limit)
        .clamp(1, binding.preview_limit);
    let preview = match fetch_dataset_preview(
        &state,
        &claims,
        &binding,
        limit,
        body.dataset_branch.as_deref().or(binding.dataset_branch.as_deref()),
        body.dataset_version.or(binding.dataset_version),
    )
    .await
    {
        Ok(preview) => preview,
        Err(error) => return internal(error),
    };

    let mut rows_read = 0i32;
    let mut inserted = 0i32;
    let mut updated = 0i32;
    let mut skipped = 0i32;
    let mut errors = 0i32;
    let mut error_details = Vec::new();

    for (index, row) in preview.rows.iter().enumerate() {
        rows_read += 1;
        let projected = match project_row(row, &binding.property_mapping) {
            Ok(value) => value,
            Err(error) => {
                errors += 1;
                error_details.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };
        let normalized = match validate_object_properties(&definitions, &projected) {
            Ok(normalized) => normalized,
            Err(error) => {
                errors += 1;
                error_details.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };
        let primary_key_value = match extract_primary_key(&normalized, &primary_key_property) {
            Ok(value) => value,
            Err(error) => {
                errors += 1;
                error_details.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };

        if body.dry_run {
            // Count what *would* happen but do not write.
            match find_existing_object_id(
                &state,
                object_type_id,
                &primary_key_property,
                &primary_key_value,
            )
            .await
            {
                Ok(Some(_)) => updated += 1,
                Ok(None) => inserted += 1,
                Err(error) => {
                    errors += 1;
                    error_details.push(json!({ "row_index": index, "error": error }));
                }
            }
            continue;
        }

        let existing_id = match find_existing_object_id(
            &state,
            object_type_id,
            &primary_key_property,
            &primary_key_value,
        )
        .await
        {
            Ok(value) => value,
            Err(error) => {
                errors += 1;
                error_details.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };

        if existing_id.is_some() && binding.sync_mode == ObjectTypeBindingSyncMode::Snapshot {
            // snapshot mode is implemented as upsert here (per-row); a strict
            // truncate-and-reload variant can be added later.
            skipped += 0;
        }

        match upsert_instance(&state, &claims, &binding, existing_id, &normalized).await {
            Ok("insert") => inserted += 1,
            Ok("update") => updated += 1,
            Ok(_) => skipped += 1,
            Err(error) => {
                errors += 1;
                error_details.push(json!({ "row_index": index, "error": error }));
            }
        }
    }

    let status = if errors == 0 {
        "completed"
    } else if inserted + updated > 0 {
        "completed_with_errors"
    } else {
        "failed"
    };
    let summary = json!({
        "rows_read": rows_read,
        "inserted": inserted,
        "updated": updated,
        "skipped": skipped,
        "errors": errors,
        "dry_run": body.dry_run,
    });

    if !body.dry_run {
        let _ = sqlx::query(
            r#"UPDATE object_type_bindings
               SET last_materialized_at = NOW(),
                   last_run_status = $2,
                   last_run_summary = $3,
                   updated_at = NOW()
               WHERE id = $1"#,
        )
        .bind(binding_id)
        .bind(status)
        .bind(&summary)
        .execute(&state.db)
        .await;
    }

    Json(MaterializeBindingResponse {
        binding_id,
        status: status.to_string(),
        rows_read,
        inserted,
        updated,
        skipped,
        errors,
        dry_run: body.dry_run,
        error_details,
    })
    .into_response()
}
