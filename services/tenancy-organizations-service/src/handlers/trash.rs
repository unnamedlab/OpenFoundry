//! Trash UX (soft-delete + restore + purge) for ontology workspace
//! resources (projects, folders, resource bindings).
//!
//! Operates on the `ontology_*` tables in the ontology database. Other
//! resource kinds (datasets, pipelines, notebooks, …) keep their own
//! soft-delete mechanics in their own services; this handler only owns
//! the ontology surface used by the workspace UI.

use auth_middleware::{Claims, layer::AuthUser};
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

use super::workspace::{ResourceKind, bad, db_err, forbidden};

#[derive(Debug, Clone, FromRow, Serialize)]
pub struct TrashEntry {
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub project_id: Option<Uuid>,
    pub display_name: String,
    pub deleted_at: DateTime<Utc>,
    pub deleted_by: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct ListTrashQuery {
    pub kind: Option<String>,
    pub limit: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct ListTrashResponse {
    pub data: Vec<TrashEntry>,
}

/// `GET /workspace/trash?kind=…&limit=…`
pub async fn list_trash(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListTrashQuery>,
) -> Response {
    let limit = query.limit.unwrap_or(200).clamp(1, 1000);

    let kind_filter = match query.kind.as_deref() {
        Some(raw) => match ResourceKind::parse(raw) {
            Ok(kind) => Some(kind),
            Err(error) => return bad(error),
        },
        None => None,
    };

    // UNION across the three soft-deletable ontology tables. We always
    // restrict by deleted_by = current user OR admin role; the workspace
    // UI surfaces the user's own trash, with admins able to see every row.
    let is_admin = claims.has_role("admin");
    let user_id = claims.sub;

    let mut entries: Vec<TrashEntry> = Vec::new();

    if kind_filter.is_none() || matches!(kind_filter, Some(ResourceKind::OntologyProject)) {
        let rows = sqlx::query_as::<_, TrashEntry>(
            r#"SELECT 'ontology_project'::text AS resource_kind,
                      id AS resource_id,
                      NULL::uuid AS project_id,
                      display_name,
                      deleted_at AS "deleted_at!",
                      deleted_by
               FROM ontology_projects
               WHERE is_deleted = TRUE AND ($1 OR deleted_by = $2)
               ORDER BY deleted_at DESC
               LIMIT $3"#,
        )
        .bind(is_admin)
        .bind(user_id)
        .bind(limit)
        .fetch_all(&state.ontology_db)
        .await;

        match rows {
            Ok(rows) => entries.extend(rows),
            Err(error) => return db_err("failed to list trashed projects", error),
        }
    }

    if kind_filter.is_none() || matches!(kind_filter, Some(ResourceKind::OntologyFolder)) {
        let rows = sqlx::query_as::<_, TrashEntry>(
            r#"SELECT 'ontology_folder'::text AS resource_kind,
                      id AS resource_id,
                      project_id,
                      name AS display_name,
                      deleted_at AS "deleted_at!",
                      deleted_by
               FROM ontology_project_folders
               WHERE is_deleted = TRUE AND ($1 OR deleted_by = $2)
               ORDER BY deleted_at DESC
               LIMIT $3"#,
        )
        .bind(is_admin)
        .bind(user_id)
        .bind(limit)
        .fetch_all(&state.ontology_db)
        .await;

        match rows {
            Ok(rows) => entries.extend(rows),
            Err(error) => return db_err("failed to list trashed folders", error),
        }
    }

    if kind_filter.is_none()
        || matches!(kind_filter, Some(ResourceKind::OntologyResourceBinding))
    {
        let rows = sqlx::query_as::<_, TrashEntry>(
            r#"SELECT 'ontology_resource_binding'::text AS resource_kind,
                      resource_id,
                      project_id,
                      resource_kind AS display_name,
                      deleted_at AS "deleted_at!",
                      deleted_by
               FROM ontology_project_resources
               WHERE is_deleted = TRUE AND ($1 OR deleted_by = $2)
               ORDER BY deleted_at DESC
               LIMIT $3"#,
        )
        .bind(is_admin)
        .bind(user_id)
        .bind(limit)
        .fetch_all(&state.ontology_db)
        .await;

        match rows {
            Ok(rows) => entries.extend(rows),
            Err(error) => return db_err("failed to list trashed resource bindings", error),
        }
    }

    // Merge-sort by deletion time descending across kinds.
    entries.sort_by(|a, b| b.deleted_at.cmp(&a.deleted_at));
    entries.truncate(limit as usize);

    Json(ListTrashResponse { data: entries }).into_response()
}

/// `POST /workspace/resources/{kind}/{id}/restore`
pub async fn restore_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    if let Err(error) = ensure_can_modify_trashed(&state, &claims, kind, resource_id).await {
        return error;
    }

    let result = match kind {
        ResourceKind::OntologyProject => {
            sqlx::query(
                r#"UPDATE ontology_projects
                   SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
                       updated_at = NOW()
                   WHERE id = $1 AND is_deleted = TRUE"#,
            )
            .bind(resource_id)
            .execute(&state.ontology_db)
            .await
        }
        ResourceKind::OntologyFolder => {
            sqlx::query(
                r#"UPDATE ontology_project_folders
                   SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL,
                       updated_at = NOW()
                   WHERE id = $1 AND is_deleted = TRUE"#,
            )
            .bind(resource_id)
            .execute(&state.ontology_db)
            .await
        }
        ResourceKind::OntologyResourceBinding => {
            sqlx::query(
                r#"UPDATE ontology_project_resources
                   SET is_deleted = FALSE, deleted_at = NULL, deleted_by = NULL
                   WHERE resource_id = $1 AND is_deleted = TRUE"#,
            )
            .bind(resource_id)
            .execute(&state.ontology_db)
            .await
        }
        other => {
            return bad(format!(
                "restore is not implemented for resource_kind '{}'",
                other.as_str()
            ));
        }
    };

    match result {
        Ok(out) if out.rows_affected() > 0 => Json(json!({ "restored": true })).into_response(),
        Ok(_) => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "no trashed row matched" })),
        )
            .into_response(),
        Err(error) => db_err("failed to restore resource", error),
    }
}

/// `DELETE /workspace/resources/{kind}/{id}/purge` — hard delete.
pub async fn purge_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    if let Err(error) = ensure_can_modify_trashed(&state, &claims, kind, resource_id).await {
        return error;
    }

    let result = match kind {
        ResourceKind::OntologyProject => {
            sqlx::query("DELETE FROM ontology_projects WHERE id = $1 AND is_deleted = TRUE")
                .bind(resource_id)
                .execute(&state.ontology_db)
                .await
        }
        ResourceKind::OntologyFolder => {
            sqlx::query(
                "DELETE FROM ontology_project_folders WHERE id = $1 AND is_deleted = TRUE",
            )
            .bind(resource_id)
            .execute(&state.ontology_db)
            .await
        }
        ResourceKind::OntologyResourceBinding => {
            sqlx::query(
                "DELETE FROM ontology_project_resources \
                 WHERE resource_id = $1 AND is_deleted = TRUE",
            )
            .bind(resource_id)
            .execute(&state.ontology_db)
            .await
        }
        other => {
            return bad(format!(
                "purge is not implemented for resource_kind '{}'",
                other.as_str()
            ));
        }
    };

    match result {
        Ok(out) if out.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "no trashed row matched" })),
        )
            .into_response(),
        Err(error) => db_err("failed to purge resource", error),
    }
}

/// Common access guard: only the user who soft-deleted the row, the
/// project owner (when relevant), or an admin may restore/purge it.
async fn ensure_can_modify_trashed(
    state: &AppState,
    claims: &Claims,
    kind: ResourceKind,
    resource_id: Uuid,
) -> Result<(), Response> {
    if claims.has_role("admin") {
        return Ok(());
    }

    let lookup = match kind {
        ResourceKind::OntologyProject => sqlx::query_as::<_, (Uuid, Option<Uuid>)>(
            r#"SELECT owner_id, deleted_by FROM ontology_projects WHERE id = $1"#,
        )
        .bind(resource_id)
        .fetch_optional(&state.ontology_db)
        .await,
        ResourceKind::OntologyFolder => sqlx::query_as::<_, (Uuid, Option<Uuid>)>(
            r#"SELECT p.owner_id, f.deleted_by
               FROM ontology_project_folders f
               JOIN ontology_projects p ON p.id = f.project_id
               WHERE f.id = $1"#,
        )
        .bind(resource_id)
        .fetch_optional(&state.ontology_db)
        .await,
        ResourceKind::OntologyResourceBinding => sqlx::query_as::<_, (Uuid, Option<Uuid>)>(
            r#"SELECT p.owner_id, r.deleted_by
               FROM ontology_project_resources r
               JOIN ontology_projects p ON p.id = r.project_id
               WHERE r.resource_id = $1"#,
        )
        .bind(resource_id)
        .fetch_optional(&state.ontology_db)
        .await,
        other => {
            return Err(bad(format!(
                "trash actions are not supported for '{}'",
                other.as_str()
            )));
        }
    };

    match lookup {
        Ok(Some((owner_id, deleted_by))) => {
            if owner_id == claims.sub || deleted_by == Some(claims.sub) {
                Ok(())
            } else {
                Err(forbidden("only the owner or the user who deleted the resource may restore or purge it"))
            }
        }
        Ok(None) => Err((
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "resource not found" })),
        )
            .into_response()),
        Err(error) => Err(db_err("failed to load resource for trash action", error)),
    }
}
