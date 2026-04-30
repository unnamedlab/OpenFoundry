//! "Recently viewed" workspace tab.
//!
//! Backed by `resource_access_log`. Writes are best-effort and intended to
//! be called from a frontend tracking middleware that fires on every
//! resource detail load. The list endpoint deduplicates per
//! (resource_kind, resource_id) and returns the most recent access for
//! each unique resource.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

use crate::AppState;

use super::workspace::{ResourceKind, bad, db_err};

#[derive(Debug, Deserialize)]
pub struct RecordAccessRequest {
    pub resource_kind: String,
    pub resource_id: Uuid,
}

#[derive(Debug, Clone, FromRow, Serialize)]
pub struct RecentEntry {
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub last_accessed_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct ListRecentsQuery {
    pub kind: Option<String>,
    pub limit: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct ListRecentsResponse {
    pub data: Vec<RecentEntry>,
}

/// `POST /workspace/recents` — record that the current user just opened
/// a resource. Always returns 202 even if the insert fails: tracking is
/// best-effort and must not block the calling page.
pub async fn record_access(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<RecordAccessRequest>,
) -> Response {
    let kind = match ResourceKind::parse(&body.resource_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    if let Err(error) = sqlx::query(
        r#"INSERT INTO resource_access_log (user_id, resource_kind, resource_id)
           VALUES ($1, $2, $3)"#,
    )
    .bind(claims.sub)
    .bind(kind.as_str())
    .bind(body.resource_id)
    .execute(&state.db)
    .await
    {
        tracing::warn!(target: "workspace.recents", "failed to record access: {error}");
    }

    StatusCode::ACCEPTED.into_response()
}

/// `GET /workspace/recents?kind=…&limit=…` — most recent unique resources
/// the current user has opened.
pub async fn list_recents(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListRecentsQuery>,
) -> Response {
    let limit = query.limit.unwrap_or(50).clamp(1, 500);

    // DISTINCT ON (kind, id) keeps only the latest row per resource.
    let result = if let Some(raw_kind) = query.kind {
        let kind = match ResourceKind::parse(&raw_kind) {
            Ok(kind) => kind,
            Err(error) => return bad(error),
        };
        sqlx::query_as::<_, RecentEntry>(
            r#"SELECT DISTINCT ON (resource_kind, resource_id)
                      resource_kind, resource_id,
                      accessed_at AS last_accessed_at
               FROM resource_access_log
               WHERE user_id = $1 AND resource_kind = $2
               ORDER BY resource_kind, resource_id, accessed_at DESC
               LIMIT $3"#,
        )
        .bind(claims.sub)
        .bind(kind.as_str())
        .bind(limit)
        .fetch_all(&state.db)
        .await
    } else {
        sqlx::query_as::<_, RecentEntry>(
            r#"SELECT DISTINCT ON (resource_kind, resource_id)
                      resource_kind, resource_id,
                      accessed_at AS last_accessed_at
               FROM resource_access_log
               WHERE user_id = $1
               ORDER BY resource_kind, resource_id, accessed_at DESC
               LIMIT $2"#,
        )
        .bind(claims.sub)
        .bind(limit)
        .fetch_all(&state.db)
        .await
    };

    match result {
        Ok(mut data) => {
            // Re-sort the deduplicated rows by recency for display.
            data.sort_by(|a, b| b.last_accessed_at.cmp(&a.last_accessed_at));
            Json(ListRecentsResponse { data }).into_response()
        }
        Err(error) => db_err("failed to list recents", error),
    }
}
