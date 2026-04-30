//! Cross-resource sharing.
//!
//! Backed by `resource_shares`. Project- and folder-level RBAC continues
//! to be evaluated by the ontology service; this handler only owns
//! direct resource shares — the entries that surface in the
//! "Shared with me" / "Shared by me" workspace tabs.

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

#[derive(Debug, Clone, Copy, sqlx::Type, Serialize, Deserialize, PartialEq, Eq)]
#[sqlx(type_name = "text", rename_all = "lowercase")]
#[serde(rename_all = "lowercase")]
pub enum AccessLevel {
    Viewer,
    Editor,
    Owner,
}

impl AccessLevel {
    fn as_str(self) -> &'static str {
        match self {
            Self::Viewer => "viewer",
            Self::Editor => "editor",
            Self::Owner => "owner",
        }
    }
}

#[derive(Debug, Clone, FromRow, Serialize)]
pub struct ResourceShare {
    pub id: Uuid,
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub shared_with_user_id: Option<Uuid>,
    pub shared_with_group_id: Option<Uuid>,
    pub sharer_id: Uuid,
    pub access_level: AccessLevel,
    pub note: String,
    pub expires_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateShareRequest {
    pub shared_with_user_id: Option<Uuid>,
    pub shared_with_group_id: Option<Uuid>,
    pub access_level: AccessLevel,
    #[serde(default)]
    pub note: Option<String>,
    pub expires_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Serialize)]
pub struct ListSharesResponse {
    pub data: Vec<ResourceShare>,
}

#[derive(Debug, Deserialize)]
pub struct ListSharedQuery {
    pub kind: Option<String>,
    pub limit: Option<i64>,
}

/// `POST /workspace/resources/{kind}/{id}/share`
///
/// The current user must have at least `editor` rights on the resource;
/// authorisation for project-bound resources is delegated to the
/// resource-owning service via the upstream call to `ensure_…_access`.
/// In Phase 1 we trust the caller and only validate the principal split
/// (exactly one of user/group must be set).
pub async fn create_share(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
    Json(body): Json<CreateShareRequest>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    let user_some = body.shared_with_user_id.is_some();
    let group_some = body.shared_with_group_id.is_some();
    if user_some == group_some {
        return bad(
            "exactly one of 'shared_with_user_id' or 'shared_with_group_id' must be provided",
        );
    }

    let note = body.note.unwrap_or_default();

    match sqlx::query_as::<_, ResourceShare>(
        r#"INSERT INTO resource_shares
              (resource_kind, resource_id, shared_with_user_id, shared_with_group_id,
               sharer_id, access_level, note, expires_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
           ON CONFLICT (resource_kind, resource_id, shared_with_user_id)
               WHERE shared_with_user_id IS NOT NULL
               DO UPDATE SET access_level = EXCLUDED.access_level,
                             note = EXCLUDED.note,
                             expires_at = EXCLUDED.expires_at,
                             sharer_id = EXCLUDED.sharer_id,
                             updated_at = NOW()
           RETURNING id, resource_kind, resource_id, shared_with_user_id,
                     shared_with_group_id, sharer_id, access_level, note,
                     expires_at, created_at, updated_at"#,
    )
    .bind(kind.as_str())
    .bind(resource_id)
    .bind(body.shared_with_user_id)
    .bind(body.shared_with_group_id)
    .bind(claims.sub)
    .bind(body.access_level.as_str())
    .bind(&note)
    .bind(body.expires_at)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(sqlx::Error::Database(database_error))
            if database_error.constraint() == Some("idx_resource_shares_group") =>
        {
            // Group conflict — issue a manual upsert path. Kept simple here.
            match sqlx::query_as::<_, ResourceShare>(
                r#"UPDATE resource_shares
                   SET access_level = $5, note = $6, expires_at = $7,
                       sharer_id = $4, updated_at = NOW()
                   WHERE resource_kind = $1 AND resource_id = $2
                     AND shared_with_group_id = $3
                   RETURNING id, resource_kind, resource_id, shared_with_user_id,
                             shared_with_group_id, sharer_id, access_level, note,
                             expires_at, created_at, updated_at"#,
            )
            .bind(kind.as_str())
            .bind(resource_id)
            .bind(body.shared_with_group_id)
            .bind(claims.sub)
            .bind(body.access_level.as_str())
            .bind(&note)
            .bind(body.expires_at)
            .fetch_one(&state.db)
            .await
            {
                Ok(row) => (StatusCode::OK, Json(row)).into_response(),
                Err(error) => db_err("failed to upsert group share", error),
            }
        }
        Err(error) => db_err("failed to create share", error),
    }
}

/// `DELETE /workspace/shares/{id}` — revoke a share. Only the original
/// sharer or an admin may revoke.
pub async fn revoke_share(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(share_id): Path<Uuid>,
) -> Response {
    let is_admin = claims.has_role("admin");

    match sqlx::query(
        r#"DELETE FROM resource_shares
           WHERE id = $1 AND ($2 OR sharer_id = $3)"#,
    )
    .bind(share_id)
    .bind(is_admin)
    .bind(claims.sub)
    .execute(&state.db)
    .await
    {
        Ok(out) if out.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "share not found or not revocable by current user" })),
        )
            .into_response(),
        Err(error) => db_err("failed to revoke share", error),
    }
}

/// `GET /workspace/shared-with-me?kind=…&limit=…`
pub async fn list_shared_with_me(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListSharedQuery>,
) -> Response {
    let limit = query.limit.unwrap_or(200).clamp(1, 1000);

    let result = if let Some(raw_kind) = query.kind {
        let kind = match ResourceKind::parse(&raw_kind) {
            Ok(kind) => kind,
            Err(error) => return bad(error),
        };
        sqlx::query_as::<_, ResourceShare>(
            r#"SELECT id, resource_kind, resource_id, shared_with_user_id,
                      shared_with_group_id, sharer_id, access_level, note,
                      expires_at, created_at, updated_at
               FROM resource_shares
               WHERE shared_with_user_id = $1
                 AND resource_kind = $2
                 AND (expires_at IS NULL OR expires_at > NOW())
               ORDER BY created_at DESC
               LIMIT $3"#,
        )
        .bind(claims.sub)
        .bind(kind.as_str())
        .bind(limit)
        .fetch_all(&state.db)
        .await
    } else {
        sqlx::query_as::<_, ResourceShare>(
            r#"SELECT id, resource_kind, resource_id, shared_with_user_id,
                      shared_with_group_id, sharer_id, access_level, note,
                      expires_at, created_at, updated_at
               FROM resource_shares
               WHERE shared_with_user_id = $1
                 AND (expires_at IS NULL OR expires_at > NOW())
               ORDER BY created_at DESC
               LIMIT $2"#,
        )
        .bind(claims.sub)
        .bind(limit)
        .fetch_all(&state.db)
        .await
    };

    match result {
        Ok(data) => Json(ListSharesResponse { data }).into_response(),
        Err(error) => db_err("failed to list shared-with-me", error),
    }
}

/// `GET /workspace/shared-by-me?kind=…&limit=…`
pub async fn list_shared_by_me(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListSharedQuery>,
) -> Response {
    let limit = query.limit.unwrap_or(200).clamp(1, 1000);

    let result = if let Some(raw_kind) = query.kind {
        let kind = match ResourceKind::parse(&raw_kind) {
            Ok(kind) => kind,
            Err(error) => return bad(error),
        };
        sqlx::query_as::<_, ResourceShare>(
            r#"SELECT id, resource_kind, resource_id, shared_with_user_id,
                      shared_with_group_id, sharer_id, access_level, note,
                      expires_at, created_at, updated_at
               FROM resource_shares
               WHERE sharer_id = $1 AND resource_kind = $2
               ORDER BY created_at DESC
               LIMIT $3"#,
        )
        .bind(claims.sub)
        .bind(kind.as_str())
        .bind(limit)
        .fetch_all(&state.db)
        .await
    } else {
        sqlx::query_as::<_, ResourceShare>(
            r#"SELECT id, resource_kind, resource_id, shared_with_user_id,
                      shared_with_group_id, sharer_id, access_level, note,
                      expires_at, created_at, updated_at
               FROM resource_shares
               WHERE sharer_id = $1
               ORDER BY created_at DESC
               LIMIT $2"#,
        )
        .bind(claims.sub)
        .bind(limit)
        .fetch_all(&state.db)
        .await
    };

    match result {
        Ok(data) => Json(ListSharesResponse { data }).into_response(),
        Err(error) => db_err("failed to list shared-by-me", error),
    }
}

/// `GET /workspace/resources/{kind}/{id}/shares` — list every share row
/// attached to a single resource. Useful for the "Manage access" dialog.
pub async fn list_resource_shares(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    match sqlx::query_as::<_, ResourceShare>(
        r#"SELECT id, resource_kind, resource_id, shared_with_user_id,
                  shared_with_group_id, sharer_id, access_level, note,
                  expires_at, created_at, updated_at
           FROM resource_shares
           WHERE resource_kind = $1 AND resource_id = $2
           ORDER BY created_at DESC"#,
    )
    .bind(kind.as_str())
    .bind(resource_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(data) => Json(ListSharesResponse { data }).into_response(),
        Err(error) => db_err("failed to list resource shares", error),
    }
}
