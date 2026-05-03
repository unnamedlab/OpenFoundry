use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use sqlx::Connection;
use uuid::Uuid;

use crate::{
    AppState,
    domain::transactions::{self, TransactionRecord},
    models::{
        branch::DatasetBranch,
        dataset::Dataset,
        lifecycle::{MutationRequest, SnapshotRequest},
    },
    storage::runtime::{
        NewDatasetVersion, ensure_default_branch_tx, insert_dataset_version_tx, load_branch_tx,
        lock_dataset, next_version_tx, update_branch_version_tx,
    },
};

pub async fn create_snapshot(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<SnapshotRequest>,
) -> impl IntoResponse {
    commit_lifecycle_operation(
        &state,
        dataset_id,
        "snapshot",
        None,
        body.message,
        0,
        0,
        None,
    )
    .await
}

pub async fn append_rows(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<MutationRequest>,
) -> impl IntoResponse {
    commit_lifecycle_operation(
        &state,
        dataset_id,
        "append",
        body.branch_name,
        body.message,
        body.row_delta.unwrap_or(0).max(0),
        body.size_delta_bytes.unwrap_or(0).max(0),
        Some(body.metadata),
    )
    .await
}

pub async fn update_rows(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<MutationRequest>,
) -> impl IntoResponse {
    commit_lifecycle_operation(
        &state,
        dataset_id,
        "update",
        body.branch_name,
        body.message,
        body.row_delta.unwrap_or(0),
        body.size_delta_bytes.unwrap_or(0),
        Some(body.metadata),
    )
    .await
}

pub async fn delete_rows(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<MutationRequest>,
) -> impl IntoResponse {
    commit_lifecycle_operation(
        &state,
        dataset_id,
        "delete",
        body.branch_name,
        body.message,
        -body.row_delta.unwrap_or(0).abs(),
        -body.size_delta_bytes.unwrap_or(0).abs(),
        Some(body.metadata),
    )
    .await
}

async fn commit_lifecycle_operation(
    state: &AppState,
    dataset_id: Uuid,
    operation: &str,
    requested_branch: Option<String>,
    message: String,
    row_delta: i64,
    size_delta: i64,
    extra_metadata: Option<serde_json::Value>,
) -> axum::response::Response {
    let mut connection = match state.db.acquire().await {
        Ok(connection) => connection,
        Err(error) => {
            tracing::error!("acquire lifecycle connection failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    let tx = match connection.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("begin lifecycle transaction failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    let mut tx = tx;

    let dataset = match lock_dataset(&mut tx, dataset_id).await {
        Ok(Some(dataset)) => dataset,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("lifecycle dataset lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let branch_name = requested_branch.unwrap_or_else(|| dataset.active_branch.clone());
    let branch = match ensure_branch(&mut tx, &dataset, &branch_name).await {
        Ok(branch) => branch,
        Err(StatusCode::NOT_FOUND) => return StatusCode::NOT_FOUND.into_response(),
        Err(status) => return status.into_response(),
    };

    let next_version = match next_version_tx(&mut tx, dataset_id, dataset.current_version).await {
        Ok(value) => value,
        Err(error) => {
            tracing::error!("lifecycle latest version lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };
    let next_row_count = (dataset.row_count + row_delta).max(0);
    let next_size_bytes = (dataset.size_bytes + size_delta).max(0);
    let storage_path = format!("{}/v{next_version}", dataset.storage_path);
    let summary = if message.trim().is_empty() {
        format!("{operation} dataset version {next_version}")
    } else {
        message.trim().to_string()
    };
    let metadata = json!({
        "version": next_version,
        "previous_version": dataset.current_version,
        "branch_name": branch.name,
        "row_delta": row_delta,
        "size_delta_bytes": size_delta,
        "storage_path": storage_path,
        "operation_metadata": extra_metadata.unwrap_or_else(|| json!({})),
    });

    let transaction = match transactions::record_committed_transaction(
        &mut tx,
        dataset_id,
        TransactionRecord {
            view_id: None,
            operation: operation.to_string(),
            branch_name: Some(branch.name.clone()),
            summary: summary.clone(),
            metadata: metadata.clone(),
        },
    )
    .await
    {
        Ok(record) => record,
        Err(error) => {
            tracing::error!("record lifecycle transaction failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let version = match insert_dataset_version_tx(
        &mut tx,
        NewDatasetVersion {
            id: Uuid::now_v7(),
            dataset_id,
            version: next_version,
            message: &summary,
            size_bytes: next_size_bytes,
            row_count: next_row_count,
            storage_path: &storage_path,
            transaction_id: transaction.id,
        },
    )
    .await
    {
        Ok(version) => version,
        Err(error) => {
            tracing::error!("insert lifecycle version failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    if let Err(error) =
        update_branch_version_tx(&mut tx, dataset_id, &branch.name, next_version).await
    {
        tracing::error!("update lifecycle branch failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    let updated_dataset = match sqlx::query_as::<_, Dataset>(
        r#"UPDATE datasets
           SET current_version = CASE WHEN active_branch = $2 THEN $3 ELSE current_version END,
               row_count = CASE WHEN active_branch = $2 THEN $4 ELSE row_count END,
               size_bytes = CASE WHEN active_branch = $2 THEN $5 ELSE size_bytes END,
               updated_at = NOW()
           WHERE id = $1
           RETURNING *"#,
    )
    .bind(dataset_id)
    .bind(&branch.name)
    .bind(next_version)
    .bind(next_row_count)
    .bind(next_size_bytes)
    .fetch_one(&mut *tx)
    .await
    {
        Ok(dataset) => dataset,
        Err(error) => {
            tracing::error!("update lifecycle dataset failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    if let Err(error) = tx.commit().await {
        tracing::error!("commit lifecycle operation failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    (
        StatusCode::CREATED,
        Json(json!({
            "transaction": transaction,
            "version": version,
            "dataset": updated_dataset,
        })),
    )
        .into_response()
}

async fn ensure_branch(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    dataset: &Dataset,
    branch_name: &str,
) -> Result<DatasetBranch, StatusCode> {
    if branch_name == "main" {
        ensure_default_branch_tx(tx, dataset)
            .await
            .map_err(|error| {
                tracing::error!("ensure default branch failed: {error}");
                StatusCode::INTERNAL_SERVER_ERROR
            })?;
    }

    load_branch_tx(tx, dataset.id, branch_name)
        .await
        .map_err(|error| {
            tracing::error!("load lifecycle branch failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR
        })?
        .ok_or(StatusCode::NOT_FOUND)
}
