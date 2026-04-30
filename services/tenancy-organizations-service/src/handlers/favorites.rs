//! Per-user favorites for any workspace resource.
//!
//! Backed by the cross-resource `user_favorites` table (see
//! `migrations/20260501000300_user_favorites.sql`). Favorites are not
//! foreign-keyed to the resource tables because each resource kind lives
//! in its own service database; orphan rows are tolerated and pruned
//! lazily by the resource-owning service.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::json;
use sqlx::FromRow;
use uuid::Uuid;

use crate::AppState;

use super::workspace::{ResourceKind, bad, db_err};

#[derive(Debug, Clone, FromRow, Serialize)]
pub struct UserFavorite {
    pub user_id: Uuid,
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateFavoriteRequest {
    pub resource_kind: String,
    pub resource_id: Uuid,
}

#[derive(Debug, Deserialize)]
pub struct ListFavoritesQuery {
    /// Optional filter; when omitted, returns favorites of every kind.
    pub kind: Option<String>,
    pub limit: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct ListFavoritesResponse {
    pub data: Vec<UserFavorite>,
}

/// `POST /workspace/favorites` — add a favorite for the current user.
///
/// Idempotent: re-favoriting the same resource returns the existing row.
pub async fn create_favorite(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreateFavoriteRequest>,
) -> Response {
    let kind = match ResourceKind::parse(&body.resource_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    match sqlx::query_as::<_, UserFavorite>(
        r#"INSERT INTO user_favorites (user_id, resource_kind, resource_id)
           VALUES ($1, $2, $3)
           ON CONFLICT (user_id, resource_kind, resource_id) DO UPDATE
               SET created_at = user_favorites.created_at
           RETURNING user_id, resource_kind, resource_id, created_at"#,
    )
    .bind(claims.sub)
    .bind(kind.as_str())
    .bind(body.resource_id)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(error) => db_err("failed to create favorite", error),
    }
}

/// `GET /workspace/favorites?kind=…&limit=…`
pub async fn list_favorites(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListFavoritesQuery>,
) -> Response {
    let limit = query.limit.unwrap_or(200).clamp(1, 1000);

    let result = if let Some(raw_kind) = query.kind {
        let kind = match ResourceKind::parse(&raw_kind) {
            Ok(kind) => kind,
            Err(error) => return bad(error),
        };
        sqlx::query_as::<_, UserFavorite>(
            r#"SELECT user_id, resource_kind, resource_id, created_at
               FROM user_favorites
               WHERE user_id = $1 AND resource_kind = $2
               ORDER BY created_at DESC
               LIMIT $3"#,
        )
        .bind(claims.sub)
        .bind(kind.as_str())
        .bind(limit)
        .fetch_all(&state.db)
        .await
    } else {
        sqlx::query_as::<_, UserFavorite>(
            r#"SELECT user_id, resource_kind, resource_id, created_at
               FROM user_favorites
               WHERE user_id = $1
               ORDER BY created_at DESC
               LIMIT $2"#,
        )
        .bind(claims.sub)
        .bind(limit)
        .fetch_all(&state.db)
        .await
    };

    match result {
        Ok(data) => Json(ListFavoritesResponse { data }).into_response(),
        Err(error) => db_err("failed to list favorites", error),
    }
}

/// `DELETE /workspace/favorites/{kind}/{id}` — remove a favorite.
pub async fn delete_favorite(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    match sqlx::query(
        r#"DELETE FROM user_favorites
           WHERE user_id = $1 AND resource_kind = $2 AND resource_id = $3"#,
    )
    .bind(claims.sub)
    .bind(kind.as_str())
    .bind(resource_id)
    .execute(&state.db)
    .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "favorite not found" })),
        )
            .into_response(),
        Err(error) => db_err("failed to delete favorite", error),
    }
}
