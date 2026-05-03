use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::{
    AppState,
    models::{
        branch::{CreateDatasetBranchRequest, DatasetBranch, MergeDatasetBranchRequest},
        dataset::Dataset,
    },
    storage::RuntimeStore,
};

fn runtime(state: &AppState) -> RuntimeStore {
    RuntimeStore::new(state.db.clone())
}

pub async fn list_branches(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    let dataset = match load_dataset(&state, dataset_id).await {
        Ok(Some(dataset)) => dataset,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("branch dataset lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    if let Err(error) = ensure_default_branch(&state, &dataset).await {
        tracing::error!("ensure default branch failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    match runtime(&state).list_legacy_branches(dataset_id).await {
        Ok(branches) => Json(branches).into_response(),
        Err(error) => {
            tracing::error!("list branches failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn create_branch(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<CreateDatasetBranchRequest>,
) -> impl IntoResponse {
    if body.name.trim().is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "branch name is required" })),
        )
            .into_response();
    }

    let dataset = match load_dataset(&state, dataset_id).await {
        Ok(Some(dataset)) => dataset,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("create branch dataset lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    if let Err(error) = ensure_default_branch(&state, &dataset).await {
        tracing::error!("ensure default branch failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    let source_version = body.source_version.unwrap_or(dataset.current_version);
    let version_exists = runtime(&state)
        .source_version_exists(dataset_id, source_version)
        .await
        .unwrap_or(false);

    if source_version != dataset.current_version && !version_exists {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "source version does not exist" })),
        )
            .into_response();
    }

    let result = runtime(&state)
        .create_legacy_branch(
            dataset_id,
            body.name.trim(),
            source_version,
            body.description.as_deref().unwrap_or_default(),
        )
        .await;

    match result {
        Ok(branch) => (StatusCode::CREATED, Json(branch)).into_response(),
        Err(error) => {
            tracing::error!("create branch failed: {error}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": error.to_string() })),
            )
                .into_response()
        }
    }
}

pub async fn checkout_branch(
    State(state): State<AppState>,
    Path((dataset_id, branch_name)): Path<(Uuid, String)>,
) -> impl IntoResponse {
    let dataset = match load_dataset(&state, dataset_id).await {
        Ok(Some(dataset)) => dataset,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("checkout branch dataset lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    if let Err(error) = ensure_default_branch(&state, &dataset).await {
        tracing::error!("ensure default branch failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    let branch = match load_branch(&state, dataset_id, &branch_name).await {
        Ok(Some(branch)) => branch,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("checkout branch query failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    match apply_branch_to_dataset(&state, dataset_id, &branch.name, branch.version).await {
        Ok(dataset) => Json(dataset).into_response(),
        Err(error) => {
            tracing::error!("checkout branch update failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn merge_branch(
    State(state): State<AppState>,
    Path((dataset_id, branch_name)): Path<(Uuid, String)>,
    Json(body): Json<MergeDatasetBranchRequest>,
) -> impl IntoResponse {
    let target_branch = body
        .target_branch
        .unwrap_or_else(|| "main".to_string())
        .trim()
        .to_string();

    match merge_branch_into_target(&state, dataset_id, &branch_name, &target_branch, false).await {
        Ok(payload) => Json(payload).into_response(),
        Err(MergeBranchError::NotFound) => StatusCode::NOT_FOUND.into_response(),
        Err(MergeBranchError::Conflict {
            source_branch,
            target_branch,
            source_version,
            source_base_version,
            target_version,
        }) => (
            StatusCode::CONFLICT,
            Json(serde_json::json!({
                "error": "branch merge conflict",
                "source_branch": source_branch,
                "target_branch": target_branch,
                "source_version": source_version,
                "source_base_version": source_base_version,
                "target_version": target_version,
            })),
        )
            .into_response(),
        Err(MergeBranchError::Invalid(message)) => (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": message })),
        )
            .into_response(),
        Err(MergeBranchError::Database(error)) => {
            tracing::error!("merge branch failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn promote_branch(
    State(state): State<AppState>,
    Path((dataset_id, branch_name)): Path<(Uuid, String)>,
) -> impl IntoResponse {
    match merge_branch_into_target(&state, dataset_id, &branch_name, "main", true).await {
        Ok(payload) => Json(payload).into_response(),
        Err(MergeBranchError::NotFound) => StatusCode::NOT_FOUND.into_response(),
        Err(MergeBranchError::Conflict {
            source_branch,
            target_branch,
            source_version,
            source_base_version,
            target_version,
        }) => (
            StatusCode::CONFLICT,
            Json(serde_json::json!({
                "error": "branch promotion conflict",
                "source_branch": source_branch,
                "target_branch": target_branch,
                "source_version": source_version,
                "source_base_version": source_base_version,
                "target_version": target_version,
            })),
        )
            .into_response(),
        Err(MergeBranchError::Invalid(message)) => (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": message })),
        )
            .into_response(),
        Err(MergeBranchError::Database(error)) => {
            tracing::error!("promote branch failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn merge_branch_into_target(
    state: &AppState,
    dataset_id: Uuid,
    source_branch_name: &str,
    target_branch_name: &str,
    promoted: bool,
) -> Result<serde_json::Value, MergeBranchError> {
    if source_branch_name == target_branch_name {
        return Err(MergeBranchError::Invalid(
            "source and target branches must be different".to_string(),
        ));
    }

    let dataset = load_dataset(state, dataset_id)
        .await
        .map_err(MergeBranchError::Database)?
        .ok_or(MergeBranchError::NotFound)?;
    ensure_default_branch(state, &dataset)
        .await
        .map_err(MergeBranchError::Database)?;

    let source = load_branch(state, dataset_id, source_branch_name)
        .await
        .map_err(MergeBranchError::Database)?
        .ok_or(MergeBranchError::NotFound)?;
    let target = load_branch(state, dataset_id, target_branch_name)
        .await
        .map_err(MergeBranchError::Database)?
        .ok_or(MergeBranchError::NotFound)?;

    if has_merge_conflict(source.base_version, source.version, target.version) {
        return Err(MergeBranchError::Conflict {
            source_branch: source.name.clone(),
            target_branch: target.name.clone(),
            source_version: source.version,
            source_base_version: source.base_version,
            target_version: target.version,
        });
    }

    runtime(state)
        .update_branch_version_and_base(dataset_id, &target.name, source.version)
        .await
        .map_err(MergeBranchError::Database)?;

    runtime(state)
        .update_branch_base_version(dataset_id, &source.name, source.version)
        .await
        .map_err(MergeBranchError::Database)?;

    if dataset.active_branch == target.name {
        apply_branch_to_dataset(state, dataset_id, &target.name, source.version)
            .await
            .map_err(MergeBranchError::Database)?;
    }

    Ok(serde_json::json!({
        "status": if promoted { "promoted" } else { "merged" },
        "source_branch": source.name,
        "target_branch": target.name,
        "version": source.version,
        "target_was_active": dataset.active_branch == target_branch_name,
    }))
}

async fn ensure_default_branch(state: &AppState, dataset: &Dataset) -> Result<(), sqlx::Error> {
    runtime(state).ensure_default_branch(dataset).await
}

async fn apply_branch_to_dataset(
    state: &AppState,
    dataset_id: Uuid,
    branch_name: &str,
    version: i32,
) -> Result<Dataset, sqlx::Error> {
    sqlx::query_as::<_, Dataset>(
        r#"UPDATE datasets
           SET active_branch = $2,
               current_version = $3,
               updated_at = NOW()
           WHERE id = $1
           RETURNING *"#,
    )
    .bind(dataset_id)
    .bind(branch_name)
    .bind(version)
    .fetch_one(&state.db)
    .await
}

async fn load_dataset(state: &AppState, dataset_id: Uuid) -> Result<Option<Dataset>, sqlx::Error> {
    sqlx::query_as::<_, Dataset>("SELECT * FROM datasets WHERE id = $1")
        .bind(dataset_id)
        .fetch_optional(&state.db)
        .await
}

async fn load_branch(
    state: &AppState,
    dataset_id: Uuid,
    branch_name: &str,
) -> Result<Option<DatasetBranch>, sqlx::Error> {
    runtime(state)
        .load_legacy_branch(dataset_id, branch_name)
        .await
}

fn has_merge_conflict(source_base_version: i32, source_version: i32, target_version: i32) -> bool {
    target_version != source_base_version && target_version != source_version
}

enum MergeBranchError {
    NotFound,
    Conflict {
        source_branch: String,
        target_branch: String,
        source_version: i32,
        source_base_version: i32,
        target_version: i32,
    },
    Invalid(String),
    Database(sqlx::Error),
}

#[cfg(test)]
mod tests {
    use super::has_merge_conflict;

    #[test]
    fn detects_diverged_target_branch_versions() {
        assert!(!has_merge_conflict(3, 5, 3));
        assert!(!has_merge_conflict(3, 5, 5));
        assert!(has_merge_conflict(3, 5, 4));
    }
}
