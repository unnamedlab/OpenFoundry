use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    CiRun, Commit, CreateCiRunRequest, CreateCommitRequest, CreateMergeRequestRequest,
    CreateRepositoryRequest, CreateReviewCommentRequest, MergeRequest, Repository, ReviewComment,
};

pub async fn get_overview(State(state): State<AppState>) -> impl IntoResponse {
    let repository_count = sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM repositories")
        .fetch_one(&state.db)
        .await
        .unwrap_or(0);
    let merge_request_count =
        sqlx::query_scalar::<_, i64>("SELECT COUNT(*) FROM merge_requests")
            .fetch_one(&state.db)
            .await
            .unwrap_or(0);
    Json(json!({
        "repositories": repository_count,
        "merge_requests": merge_request_count,
    }))
}

pub async fn list_repositories(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, Repository>(
        "SELECT * FROM repositories ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_repository(
    State(state): State<AppState>,
    Json(body): Json<CreateRepositoryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let default_branch = body.default_branch.unwrap_or_else(|| "main".to_string());
    let visibility = body.visibility.unwrap_or_else(|| "private".to_string());
    match sqlx::query_as::<_, Repository>(
        "INSERT INTO repositories (id, name, default_branch, visibility) \
         VALUES ($1, $2, $3, $4) RETURNING *",
    )
    .bind(id)
    .bind(&body.name)
    .bind(&default_branch)
    .bind(&visibility)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_repository(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, Repository>("SELECT * FROM repositories WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_commits(
    State(state): State<AppState>,
    Path(repository_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, Commit>(
        "SELECT * FROM commits WHERE repository_id = $1 ORDER BY created_at DESC LIMIT 200",
    )
    .bind(repository_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_commit(
    State(state): State<AppState>,
    Path(repository_id): Path<Uuid>,
    Json(body): Json<CreateCommitRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, Commit>(
        "INSERT INTO commits (id, repository_id, sha, author, message) \
         VALUES ($1, $2, $3, $4, $5) RETURNING *",
    )
    .bind(id)
    .bind(repository_id)
    .bind(&body.sha)
    .bind(&body.author)
    .bind(&body.message)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn list_files(
    State(_state): State<AppState>,
    Path(repository_id): Path<Uuid>,
) -> impl IntoResponse {
    Json(json!({"repository_id": repository_id, "files": []}))
}

pub async fn search_files(
    State(_state): State<AppState>,
    Path(repository_id): Path<Uuid>,
) -> impl IntoResponse {
    Json(json!({"repository_id": repository_id, "matches": []}))
}

pub async fn get_diff(
    State(_state): State<AppState>,
    Path(repository_id): Path<Uuid>,
) -> impl IntoResponse {
    Json(json!({"repository_id": repository_id, "diff": ""}))
}

pub async fn list_integrations(State(_state): State<AppState>) -> impl IntoResponse {
    Json(json!({"items": []}))
}

pub async fn create_integration(
    State(_state): State<AppState>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    (StatusCode::CREATED, Json(json!({"id": Uuid::now_v7(), "payload": body})))
}

pub async fn get_integration(
    State(_state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    Json(json!({"id": id, "status": "active"}))
}

pub async fn sync_integration(
    State(_state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    (StatusCode::ACCEPTED, Json(json!({"integration_id": id, "status": "queued"})))
}

pub async fn list_merge_requests(
    State(state): State<AppState>,
    Path(repository_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, MergeRequest>(
        "SELECT * FROM merge_requests WHERE repository_id = $1 ORDER BY created_at DESC LIMIT 200",
    )
    .bind(repository_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_merge_request(
    State(state): State<AppState>,
    Path(repository_id): Path<Uuid>,
    Json(body): Json<CreateMergeRequestRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, MergeRequest>(
        "INSERT INTO merge_requests (id, repository_id, source_branch, target_branch, title, status) \
         VALUES ($1, $2, $3, $4, $5, 'open') RETURNING *",
    )
    .bind(id)
    .bind(repository_id)
    .bind(&body.source_branch)
    .bind(&body.target_branch)
    .bind(&body.title)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn list_merge_requests_global(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, MergeRequest>(
        "SELECT * FROM merge_requests ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn get_merge_request(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, MergeRequest>("SELECT * FROM merge_requests WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn merge_merge_request(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, MergeRequest>(
        "UPDATE merge_requests SET status = 'merged', updated_at = now() WHERE id = $1 RETURNING *",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_review_comments(
    State(state): State<AppState>,
    Path(merge_request_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ReviewComment>(
        "SELECT * FROM review_comments WHERE merge_request_id = $1 ORDER BY created_at ASC",
    )
    .bind(merge_request_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_review_comment(
    State(state): State<AppState>,
    Path(merge_request_id): Path<Uuid>,
    Json(body): Json<CreateReviewCommentRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, ReviewComment>(
        "INSERT INTO review_comments (id, merge_request_id, author, body) \
         VALUES ($1, $2, $3, $4) RETURNING *",
    )
    .bind(id)
    .bind(merge_request_id)
    .bind(&body.author)
    .bind(&body.body)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn list_ci_runs(
    State(state): State<AppState>,
    Path(repository_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, CiRun>(
        "SELECT * FROM ci_runs WHERE repository_id = $1 ORDER BY created_at DESC LIMIT 200",
    )
    .bind(repository_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_ci_run(
    State(state): State<AppState>,
    Path(repository_id): Path<Uuid>,
    Json(body): Json<CreateCiRunRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, CiRun>(
        "INSERT INTO ci_runs (id, repository_id, commit_sha, status) \
         VALUES ($1, $2, $3, 'queued') RETURNING *",
    )
    .bind(id)
    .bind(repository_id)
    .bind(&body.commit_sha)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn delete_repository(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query("DELETE FROM repositories WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}
