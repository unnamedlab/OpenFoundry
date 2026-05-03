use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use core_models::security::{EffectiveMarking, MarkingId};
use uuid::Uuid;

use crate::AppState;
use crate::domain::validation::{
    validate_dataset_format, validate_dataset_name, validate_file_index_entry,
    validate_health_status, validate_lineage_link, validate_permission_edge,
};
use crate::models::{
    branch::DatasetBranch,
    dataset::Dataset,
    dataset_model::{
        DatasetFileIndexEntry, DatasetHealthSummary, DatasetLineageLink, DatasetMetadataPatch,
        DatasetPermissionEdge, DatasetRichModel, PutDatasetFilesRequest,
        PutDatasetLineageLinksRequest, PutDatasetMarkingsRequest, PutDatasetPermissionsRequest,
    },
    schema::DatasetSchema,
    version::DatasetVersion,
    view::DatasetView,
};
use crate::security::{emit_audit, require_dataset_admin, require_dataset_write};

pub async fn get_dataset_model(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match build_dataset_model(&state, dataset_id).await {
        Ok(Some(model)) => Json(model).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("dataset model lookup failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn patch_dataset_metadata(
    State(state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<DatasetMetadataPatch>,
) -> Response {
    if let Err(resp) = require_dataset_write(&claims, &dataset_id.to_string()) {
        return resp.into_response();
    }
    if let Some(name) = body.name.as_deref() {
        if let Err(error) = validate_dataset_name(name) {
            return bad_request(error);
        }
    }
    let normalized_format = body
        .format
        .as_ref()
        .map(|format| format.to_ascii_lowercase());
    if let Some(format) = normalized_format.as_deref() {
        if let Err(error) = validate_dataset_format(format) {
            return bad_request(error);
        }
    }
    if let Some(status) = body.health_status.as_deref() {
        if let Err(error) = validate_health_status(status) {
            return bad_request(error);
        }
    }
    if let Some(view_id) = body.current_view_id {
        let belongs_to_dataset = sqlx::query_scalar::<_, bool>(
            "SELECT EXISTS(SELECT 1 FROM dataset_views WHERE id = $1 AND dataset_id = $2)",
        )
        .bind(view_id)
        .bind(dataset_id)
        .fetch_one(&state.db)
        .await
        .unwrap_or(false);
        if !belongs_to_dataset {
            return bad_request("current_view_id must belong to the dataset");
        }
    }

    let update = sqlx::query_as::<_, Dataset>(
        r#"UPDATE datasets
           SET name = COALESCE($2, name),
               description = COALESCE($3, description),
               owner_id = COALESCE($4, owner_id),
               tags = COALESCE($5, tags),
               format = COALESCE($6, format),
               metadata = COALESCE($7, metadata),
               health_status = COALESCE($8, health_status),
               current_view_id = COALESCE($9, current_view_id),
               updated_at = NOW()
           WHERE id = $1
           RETURNING *"#,
    )
    .bind(dataset_id)
    .bind(&body.name)
    .bind(&body.description)
    .bind(body.owner_id)
    .bind(&body.tags)
    .bind(&normalized_format)
    .bind(&body.metadata)
    .bind(&body.health_status)
    .bind(body.current_view_id)
    .fetch_optional(&state.db)
    .await;

    let dataset = match update {
        Ok(Some(dataset)) => dataset,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("dataset metadata update failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    if let Some(fields) = body.schema {
        let upsert = sqlx::query(
            r#"INSERT INTO dataset_schemas (id, dataset_id, fields)
               VALUES ($1, $2, $3)
               ON CONFLICT (dataset_id)
               DO UPDATE SET fields = EXCLUDED.fields, created_at = NOW()"#,
        )
        .bind(Uuid::now_v7())
        .bind(dataset_id)
        .bind(fields)
        .execute(&state.db)
        .await;
        if let Err(error) = upsert {
            tracing::error!("dataset schema upsert failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }

    emit_audit(
        &claims.sub,
        "dataset.metadata.update",
        &dataset_id.to_string(),
        serde_json::json!({
            "dataset_id": dataset_id,
            "current_view_id": dataset.current_view_id,
        }),
    );

    Json(dataset).into_response()
}

pub async fn list_dataset_markings(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match effective_markings(&state, dataset_id).await {
        Ok(markings) => Json(markings).into_response(),
        Err(error) => {
            tracing::error!("dataset markings lookup failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn put_dataset_markings(
    State(state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<PutDatasetMarkingsRequest>,
) -> Response {
    if let Err(resp) = require_dataset_admin(&claims, &dataset_id.to_string()) {
        return resp.into_response();
    }
    if !dataset_exists(&state, dataset_id).await {
        return StatusCode::NOT_FOUND.into_response();
    }

    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("begin markings transaction failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    if let Err(error) =
        sqlx::query("DELETE FROM dataset_markings WHERE dataset_rid = $1 AND source = 'direct'")
            .bind(dataset_id.to_string())
            .execute(&mut *tx)
            .await
    {
        tracing::error!("delete dataset markings failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    for marking in body.markings {
        if let Err(error) = sqlx::query(
            r#"INSERT INTO dataset_markings (dataset_rid, marking_id, source, applied_by)
               VALUES ($1, $2, 'direct', $3)
               ON CONFLICT DO NOTHING"#,
        )
        .bind(dataset_id.to_string())
        .bind(marking)
        .bind(claims.sub)
        .execute(&mut *tx)
        .await
        {
            tracing::error!("insert dataset marking failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }
    if let Err(error) = tx.commit().await {
        tracing::error!("commit markings transaction failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    emit_audit(
        &claims.sub,
        "dataset.markings.replace",
        &dataset_id.to_string(),
        serde_json::json!({ "dataset_id": dataset_id }),
    );
    list_dataset_markings(State(state), Path(dataset_id))
        .await
        .into_response()
}

pub async fn list_dataset_permissions(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match permission_edges(&state, dataset_id).await {
        Ok(edges) => Json(edges).into_response(),
        Err(error) => {
            tracing::error!("dataset permissions lookup failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn put_dataset_permissions(
    State(state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<PutDatasetPermissionsRequest>,
) -> Response {
    if let Err(resp) = require_dataset_admin(&claims, &dataset_id.to_string()) {
        return resp.into_response();
    }
    if !dataset_exists(&state, dataset_id).await {
        return StatusCode::NOT_FOUND.into_response();
    }
    for edge in &body.permissions {
        let source = edge.source.as_deref().unwrap_or("direct");
        if let Err(error) = validate_permission_edge(
            &edge.principal_kind,
            &edge.principal_id,
            &edge.role,
            &edge.actions,
            source,
            edge.inherited_from.as_deref(),
        ) {
            return bad_request(error);
        }
    }

    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("begin permissions transaction failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    if let Err(error) = sqlx::query("DELETE FROM dataset_permission_edges WHERE dataset_id = $1")
        .bind(dataset_id)
        .execute(&mut *tx)
        .await
    {
        tracing::error!("delete dataset permissions failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    for edge in body.permissions {
        let source = edge.source.unwrap_or_else(|| "direct".to_string());
        if let Err(error) = sqlx::query(
            r#"INSERT INTO dataset_permission_edges
               (id, dataset_id, principal_kind, principal_id, role, actions, source, inherited_from)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8)"#,
        )
        .bind(Uuid::now_v7())
        .bind(dataset_id)
        .bind(edge.principal_kind)
        .bind(edge.principal_id)
        .bind(edge.role)
        .bind(edge.actions)
        .bind(source)
        .bind(edge.inherited_from)
        .execute(&mut *tx)
        .await
        {
            tracing::error!("insert dataset permission failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }
    if let Err(error) = tx.commit().await {
        tracing::error!("commit permissions transaction failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    emit_audit(
        &claims.sub,
        "dataset.permissions.replace",
        &dataset_id.to_string(),
        serde_json::json!({ "dataset_id": dataset_id }),
    );
    list_dataset_permissions(State(state), Path(dataset_id))
        .await
        .into_response()
}

pub async fn list_dataset_lineage_links(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match lineage_links(&state, dataset_id).await {
        Ok(links) => Json(links).into_response(),
        Err(error) => {
            tracing::error!("dataset lineage links lookup failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn put_dataset_lineage_links(
    State(state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<PutDatasetLineageLinksRequest>,
) -> Response {
    if let Err(resp) = require_dataset_write(&claims, &dataset_id.to_string()) {
        return resp.into_response();
    }
    if !dataset_exists(&state, dataset_id).await {
        return StatusCode::NOT_FOUND.into_response();
    }
    for link in &body.links {
        if let Err(error) = validate_lineage_link(&link.direction, &link.target_rid) {
            return bad_request(error);
        }
    }

    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("begin lineage transaction failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    if let Err(error) = sqlx::query("DELETE FROM dataset_lineage_links WHERE dataset_id = $1")
        .bind(dataset_id)
        .execute(&mut *tx)
        .await
    {
        tracing::error!("delete dataset lineage links failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    for link in body.links {
        if let Err(error) = sqlx::query(
            r#"INSERT INTO dataset_lineage_links
               (id, dataset_id, direction, target_rid, target_kind, relation_kind, pipeline_id, workflow_id, metadata)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)"#,
        )
        .bind(Uuid::now_v7())
        .bind(dataset_id)
        .bind(link.direction)
        .bind(link.target_rid)
        .bind(link.target_kind.unwrap_or_else(|| "dataset".to_string()))
        .bind(link.relation_kind.unwrap_or_else(|| "derives_from".to_string()))
        .bind(link.pipeline_id)
        .bind(link.workflow_id)
        .bind(link.metadata.unwrap_or_else(|| serde_json::json!({})))
        .execute(&mut *tx)
        .await
        {
            tracing::error!("insert dataset lineage link failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }
    if let Err(error) = tx.commit().await {
        tracing::error!("commit lineage transaction failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    emit_audit(
        &claims.sub,
        "dataset.lineage_links.replace",
        &dataset_id.to_string(),
        serde_json::json!({ "dataset_id": dataset_id }),
    );
    list_dataset_lineage_links(State(state), Path(dataset_id))
        .await
        .into_response()
}

pub async fn list_dataset_file_index(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match file_index_entries(&state, dataset_id).await {
        Ok(files) => Json(files).into_response(),
        Err(error) => {
            tracing::error!("dataset file index lookup failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn put_dataset_file_index(
    State(state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<PutDatasetFilesRequest>,
) -> Response {
    if let Err(resp) = require_dataset_write(&claims, &dataset_id.to_string()) {
        return resp.into_response();
    }
    if !dataset_exists(&state, dataset_id).await {
        return StatusCode::NOT_FOUND.into_response();
    }
    for file in &body.files {
        let entry_type = file.entry_type.as_deref().unwrap_or("file");
        if let Err(error) = validate_file_index_entry(
            &file.path,
            &file.storage_path,
            entry_type,
            file.size_bytes.unwrap_or(0),
        ) {
            return bad_request(error);
        }
    }
    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("begin file index transaction failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    if let Err(error) = sqlx::query("DELETE FROM dataset_file_index WHERE dataset_id = $1")
        .bind(dataset_id)
        .execute(&mut *tx)
        .await
    {
        tracing::error!("delete dataset file index failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    for file in body.files {
        if let Err(error) = sqlx::query(
            r#"INSERT INTO dataset_file_index
               (id, dataset_id, path, storage_path, entry_type, size_bytes, content_type, metadata, last_modified)
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)"#,
        )
        .bind(Uuid::now_v7())
        .bind(dataset_id)
        .bind(file.path)
        .bind(file.storage_path)
        .bind(file.entry_type.unwrap_or_else(|| "file".to_string()))
        .bind(file.size_bytes.unwrap_or(0))
        .bind(file.content_type)
        .bind(file.metadata.unwrap_or_else(|| serde_json::json!({})))
        .bind(file.last_modified)
        .execute(&mut *tx)
        .await
        {
            tracing::error!("insert dataset file index failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }
    if let Err(error) = tx.commit().await {
        tracing::error!("commit file index transaction failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }
    emit_audit(
        &claims.sub,
        "dataset.files.replace",
        &dataset_id.to_string(),
        serde_json::json!({ "dataset_id": dataset_id }),
    );
    list_dataset_file_index(State(state), Path(dataset_id))
        .await
        .into_response()
}

async fn build_dataset_model(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Option<DatasetRichModel>, sqlx::Error> {
    let Some(dataset) = sqlx::query_as::<_, Dataset>("SELECT * FROM datasets WHERE id = $1")
        .bind(dataset_id)
        .fetch_optional(&state.db)
        .await?
    else {
        return Ok(None);
    };
    let schema =
        sqlx::query_as::<_, DatasetSchema>("SELECT * FROM dataset_schemas WHERE dataset_id = $1")
            .bind(dataset_id)
            .fetch_optional(&state.db)
            .await?;
    let files = file_index_entries(state, dataset_id).await?;
    let branches = sqlx::query_as::<_, DatasetBranch>(
        "SELECT * FROM dataset_branches WHERE dataset_id = $1 ORDER BY is_default DESC, name ASC",
    )
    .bind(dataset_id)
    .fetch_all(&state.db)
    .await?;
    let versions = sqlx::query_as::<_, DatasetVersion>(
        "SELECT * FROM dataset_versions WHERE dataset_id = $1 ORDER BY version DESC",
    )
    .bind(dataset_id)
    .fetch_all(&state.db)
    .await?;
    let current_view = current_view(state, &dataset).await?;
    let health = health_summary(state, &dataset).await?;
    let markings = effective_markings(state, dataset_id)
        .await
        .unwrap_or_default();
    let permissions = permission_edges(state, dataset_id).await?;
    let lineage_links = lineage_links(state, dataset_id).await?;

    Ok(Some(DatasetRichModel {
        dataset,
        schema,
        files,
        branches,
        versions,
        current_view,
        health,
        markings,
        permissions,
        lineage_links,
    }))
}

async fn current_view(
    state: &AppState,
    dataset: &Dataset,
) -> Result<Option<DatasetView>, sqlx::Error> {
    if let Some(view_id) = dataset.current_view_id {
        return sqlx::query_as::<_, DatasetView>(
            "SELECT * FROM dataset_views WHERE id = $1 AND dataset_id = $2",
        )
        .bind(view_id)
        .bind(dataset.id)
        .fetch_optional(&state.db)
        .await;
    }
    sqlx::query_as::<_, DatasetView>(
        "SELECT * FROM dataset_views WHERE dataset_id = $1 ORDER BY updated_at DESC LIMIT 1",
    )
    .bind(dataset.id)
    .fetch_optional(&state.db)
    .await
}

async fn health_summary(
    state: &AppState,
    dataset: &Dataset,
) -> Result<DatasetHealthSummary, sqlx::Error> {
    let profile = sqlx::query_as::<_, (f64, chrono::DateTime<chrono::Utc>)>(
        "SELECT score, profiled_at FROM dataset_profiles WHERE dataset_id = $1",
    )
    .bind(dataset.id)
    .fetch_optional(&state.db)
    .await?;
    let active_alert_count = sqlx::query_scalar::<_, i64>(
        "SELECT COUNT(*) FROM dataset_quality_alerts WHERE dataset_id = $1 AND status <> 'resolved'",
    )
    .bind(dataset.id)
    .fetch_one(&state.db)
    .await
    .unwrap_or(0);
    Ok(DatasetHealthSummary {
        status: dataset.health_status.clone(),
        quality_score: profile.as_ref().map(|(score, _)| *score),
        profile_generated_at: profile.map(|(_, ts)| ts.to_rfc3339()),
        active_alert_count,
        lint_posture: None,
        lint_finding_count: 0,
    })
}

async fn effective_markings(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<EffectiveMarking>, sqlx::Error> {
    if let Some(resolver) = &state.marking_resolver {
        return Ok(resolver
            .compute(&dataset_id.to_string())
            .await
            .map(|items| items.as_ref().clone())
            .unwrap_or_default());
    }
    let ids = sqlx::query_as::<_, (Uuid,)>(
        r#"SELECT marking_id FROM dataset_markings
           WHERE dataset_rid = $1 AND source = 'direct'
           ORDER BY marking_id"#,
    )
    .bind(dataset_id.to_string())
    .fetch_all(&state.db)
    .await?;
    Ok(ids
        .into_iter()
        .map(|(id,)| EffectiveMarking::direct(MarkingId::from_uuid(id)))
        .collect())
}

async fn permission_edges(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<DatasetPermissionEdge>, sqlx::Error> {
    sqlx::query_as::<_, DatasetPermissionEdge>(
        "SELECT * FROM dataset_permission_edges WHERE dataset_id = $1 ORDER BY source, principal_kind, principal_id, role",
    )
    .bind(dataset_id)
    .fetch_all(&state.db)
    .await
}

async fn lineage_links(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<DatasetLineageLink>, sqlx::Error> {
    sqlx::query_as::<_, DatasetLineageLink>(
        "SELECT * FROM dataset_lineage_links WHERE dataset_id = $1 ORDER BY direction, target_rid, relation_kind",
    )
    .bind(dataset_id)
    .fetch_all(&state.db)
    .await
}

async fn file_index_entries(
    state: &AppState,
    dataset_id: Uuid,
) -> Result<Vec<DatasetFileIndexEntry>, sqlx::Error> {
    sqlx::query_as::<_, DatasetFileIndexEntry>(
        "SELECT * FROM dataset_file_index WHERE dataset_id = $1 ORDER BY path",
    )
    .bind(dataset_id)
    .fetch_all(&state.db)
    .await
}

async fn dataset_exists(state: &AppState, dataset_id: Uuid) -> bool {
    sqlx::query_scalar::<_, bool>("SELECT EXISTS(SELECT 1 FROM datasets WHERE id = $1)")
        .bind(dataset_id)
        .fetch_one(&state.db)
        .await
        .unwrap_or(false)
}

fn bad_request(message: impl Into<String>) -> Response {
    (
        StatusCode::BAD_REQUEST,
        Json(serde_json::json!({ "error": message.into() })),
    )
        .into_response()
}
