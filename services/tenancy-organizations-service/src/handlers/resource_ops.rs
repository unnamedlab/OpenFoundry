//! Workspace resource operations: move, rename, duplicate, soft delete,
//! and bulk batch dispatcher.
//!
//! These endpoints are scoped to the *ontology* workspace surface for
//! Phase 1 (projects, folders, resource bindings). Other resource kinds
//! continue to expose their own move/rename APIs in their owning
//! services; the workspace UI is expected to call those services
//! directly when a non-ontology row is acted upon — the `/batch`
//! endpoint will gain a router for that in a later phase.

use auth_middleware::{Claims, layer::AuthUser};
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use uuid::Uuid;

use crate::AppState;

use super::workspace::{ResourceKind, bad, db_err, forbidden};

#[derive(Debug, Deserialize)]
pub struct MoveRequest {
    /// Destination folder id. `None` moves the resource to the project root.
    pub target_folder_id: Option<Uuid>,
    /// Destination project id (only for resource bindings; folders cannot
    /// hop projects in Phase 1 because that requires a deep clone).
    pub target_project_id: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct RenameRequest {
    pub name: String,
}

#[derive(Debug, Deserialize)]
pub struct DuplicateRequest {
    pub new_name: Option<String>,
    pub target_folder_id: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct BatchAction {
    pub op: String, // "move" | "delete" | "restore" | "purge"
    pub resource_kind: String,
    pub resource_id: Uuid,
    #[serde(default)]
    pub target_folder_id: Option<Uuid>,
}

#[derive(Debug, Deserialize)]
pub struct BatchRequest {
    pub actions: Vec<BatchAction>,
}

#[derive(Debug, Serialize)]
pub struct BatchResultEntry {
    pub op: String,
    pub resource_kind: String,
    pub resource_id: Uuid,
    pub ok: bool,
    pub error: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct BatchResponse {
    pub results: Vec<BatchResultEntry>,
}

/// `POST /workspace/resources/{kind}/{id}/move`
pub async fn move_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
    Json(body): Json<MoveRequest>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    if let Err(response) = ensure_owner_or_admin(&state, &claims, kind, resource_id).await {
        return response;
    }

    match kind {
        ResourceKind::OntologyFolder => {
            // Reparent within the same project. parent_folder_id may be NULL.
            let outcome = sqlx::query(
                r#"UPDATE ontology_project_folders
                   SET parent_folder_id = $2, updated_at = NOW()
                   WHERE id = $1 AND is_deleted = FALSE"#,
            )
            .bind(resource_id)
            .bind(body.target_folder_id)
            .execute(&state.ontology_db)
            .await;
            execute_outcome(outcome, "failed to move folder")
        }
        ResourceKind::OntologyResourceBinding => {
            // Move a resource binding to a different project. We do not
            // model folder ownership for bindings yet, so target_folder_id
            // is currently ignored — kept in the API for forward-compat.
            let new_project = match body.target_project_id {
                Some(value) => value,
                None => return bad("'target_project_id' is required for resource bindings"),
            };
            let outcome = sqlx::query(
                r#"UPDATE ontology_project_resources
                   SET project_id = $2
                   WHERE resource_id = $1 AND is_deleted = FALSE"#,
            )
            .bind(resource_id)
            .bind(new_project)
            .execute(&state.ontology_db)
            .await;
            execute_outcome(outcome, "failed to move resource binding")
        }
        other => bad(format!(
            "move is not supported for resource_kind '{}'",
            other.as_str()
        )),
    }
}

/// `POST /workspace/resources/{kind}/{id}/rename`
pub async fn rename_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
    Json(body): Json<RenameRequest>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    let new_name = body.name.trim().to_string();
    if new_name.is_empty() {
        return bad("'name' must not be empty");
    }

    if let Err(response) = ensure_owner_or_admin(&state, &claims, kind, resource_id).await {
        return response;
    }

    match kind {
        ResourceKind::OntologyProject => {
            let outcome = sqlx::query(
                r#"UPDATE ontology_projects
                   SET display_name = $2, updated_at = NOW()
                   WHERE id = $1 AND is_deleted = FALSE"#,
            )
            .bind(resource_id)
            .bind(&new_name)
            .execute(&state.ontology_db)
            .await;
            execute_outcome(outcome, "failed to rename project")
        }
        ResourceKind::OntologyFolder => {
            let outcome = sqlx::query(
                r#"UPDATE ontology_project_folders
                   SET name = $2, updated_at = NOW()
                   WHERE id = $1 AND is_deleted = FALSE"#,
            )
            .bind(resource_id)
            .bind(&new_name)
            .execute(&state.ontology_db)
            .await;
            execute_outcome(outcome, "failed to rename folder")
        }
        other => bad(format!(
            "rename is not supported for resource_kind '{}'",
            other.as_str()
        )),
    }
}

/// `POST /workspace/resources/{kind}/{id}/duplicate`
///
/// Phase 1 only supports duplicating *folders* (shallow: the folder row
/// is cloned with a new id; children are not copied). Duplicating
/// projects or resource bindings requires a deeper clone routine that is
/// out of scope here and deferred to Phase 2.
pub async fn duplicate_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
    Json(body): Json<DuplicateRequest>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    if let Err(response) = ensure_owner_or_admin(&state, &claims, kind, resource_id).await {
        return response;
    }

    match kind {
        ResourceKind::OntologyFolder => {
            let new_id = Uuid::now_v7();
            let outcome = sqlx::query(
                r#"INSERT INTO ontology_project_folders
                       (id, project_id, parent_folder_id, name, slug, description, created_by)
                   SELECT $1,
                          project_id,
                          COALESCE($2, parent_folder_id),
                          COALESCE($3, name || ' (copy)'),
                          slug || '-' || substr($1::text, 1, 8),
                          description,
                          $4
                   FROM ontology_project_folders
                   WHERE id = $5 AND is_deleted = FALSE"#,
            )
            .bind(new_id)
            .bind(body.target_folder_id)
            .bind(body.new_name)
            .bind(claims.sub)
            .bind(resource_id)
            .execute(&state.ontology_db)
            .await;

            match outcome {
                Ok(out) if out.rows_affected() > 0 => {
                    (StatusCode::CREATED, Json(json!({ "id": new_id }))).into_response()
                }
                Ok(_) => (
                    StatusCode::NOT_FOUND,
                    Json(json!({ "error": "source folder not found" })),
                )
                    .into_response(),
                Err(error) => db_err("failed to duplicate folder", error),
            }
        }
        other => bad(format!(
            "duplicate is not supported for resource_kind '{}' in Phase 1",
            other.as_str()
        )),
    }
}

/// `DELETE /workspace/resources/{kind}/{id}` — soft delete (sends to
/// trash). Hard delete is `…/purge` in [`super::trash`].
pub async fn soft_delete_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((raw_kind, resource_id)): Path<(String, Uuid)>,
) -> Response {
    let kind = match ResourceKind::parse(&raw_kind) {
        Ok(kind) => kind,
        Err(error) => return bad(error),
    };

    if let Err(response) = ensure_owner_or_admin(&state, &claims, kind, resource_id).await {
        return response;
    }

    let outcome = match kind {
        ResourceKind::OntologyProject => sqlx::query(
            r#"UPDATE ontology_projects
               SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
                   updated_at = NOW()
               WHERE id = $1 AND is_deleted = FALSE"#,
        )
        .bind(resource_id)
        .bind(claims.sub)
        .execute(&state.ontology_db)
        .await,
        ResourceKind::OntologyFolder => sqlx::query(
            r#"UPDATE ontology_project_folders
               SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
                   updated_at = NOW()
               WHERE id = $1 AND is_deleted = FALSE"#,
        )
        .bind(resource_id)
        .bind(claims.sub)
        .execute(&state.ontology_db)
        .await,
        ResourceKind::OntologyResourceBinding => sqlx::query(
            r#"UPDATE ontology_project_resources
               SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2
               WHERE resource_id = $1 AND is_deleted = FALSE"#,
        )
        .bind(resource_id)
        .bind(claims.sub)
        .execute(&state.ontology_db)
        .await,
        other => {
            return bad(format!(
                "soft delete is not supported for resource_kind '{}'",
                other.as_str()
            ));
        }
    };

    execute_outcome(outcome, "failed to delete resource")
}

/// `POST /workspace/resources/batch` — apply a list of actions
/// atomically *per row* (no global transaction). Returns one result
/// entry per input action so the UI can surface partial failures.
pub async fn batch_apply(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<BatchRequest>,
) -> Response {
    let mut results = Vec::with_capacity(body.actions.len());

    for action in body.actions {
        let kind = match ResourceKind::parse(&action.resource_kind) {
            Ok(kind) => kind,
            Err(error) => {
                results.push(BatchResultEntry {
                    op: action.op.clone(),
                    resource_kind: action.resource_kind.clone(),
                    resource_id: action.resource_id,
                    ok: false,
                    error: Some(error),
                });
                continue;
            }
        };

        let outcome = match action.op.as_str() {
            "delete" => {
                if ensure_owner_or_admin(&state, &claims, kind, action.resource_id)
                    .await
                    .is_err()
                {
                    Err("forbidden".to_string())
                } else {
                    soft_delete_one(&state, &claims, kind, action.resource_id)
                        .await
                        .map_err(|error| error.to_string())
                }
            }
            "move" => {
                if ensure_owner_or_admin(&state, &claims, kind, action.resource_id)
                    .await
                    .is_err()
                {
                    Err("forbidden".to_string())
                } else if !matches!(kind, ResourceKind::OntologyFolder) {
                    Err(format!(
                        "batch move only supported for folders in Phase 1 (got '{}')",
                        kind.as_str()
                    ))
                } else {
                    sqlx::query(
                        r#"UPDATE ontology_project_folders
                           SET parent_folder_id = $2, updated_at = NOW()
                           WHERE id = $1 AND is_deleted = FALSE"#,
                    )
                    .bind(action.resource_id)
                    .bind(action.target_folder_id)
                    .execute(&state.ontology_db)
                    .await
                    .map(|_| ())
                    .map_err(|error| error.to_string())
                }
            }
            other => Err(format!("unsupported batch op '{other}'")),
        };

        results.push(BatchResultEntry {
            op: action.op,
            resource_kind: action.resource_kind,
            resource_id: action.resource_id,
            ok: outcome.is_ok(),
            error: outcome.err(),
        });
    }

    Json(BatchResponse { results }).into_response()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

fn execute_outcome(
    outcome: Result<sqlx::postgres::PgQueryResult, sqlx::Error>,
    failure_msg: &'static str,
) -> Response {
    match outcome {
        Ok(out) if out.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "no row matched" })),
        )
            .into_response(),
        Err(error) => db_err(failure_msg, error),
    }
}

async fn soft_delete_one(
    state: &AppState,
    claims: &Claims,
    kind: ResourceKind,
    resource_id: Uuid,
) -> Result<(), sqlx::Error> {
    match kind {
        ResourceKind::OntologyProject => {
            sqlx::query(
                r#"UPDATE ontology_projects
                   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
                       updated_at = NOW()
                   WHERE id = $1"#,
            )
            .bind(resource_id)
            .bind(claims.sub)
            .execute(&state.ontology_db)
            .await
            .map(|_| ())
        }
        ResourceKind::OntologyFolder => {
            sqlx::query(
                r#"UPDATE ontology_project_folders
                   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2,
                       updated_at = NOW()
                   WHERE id = $1"#,
            )
            .bind(resource_id)
            .bind(claims.sub)
            .execute(&state.ontology_db)
            .await
            .map(|_| ())
        }
        ResourceKind::OntologyResourceBinding => {
            sqlx::query(
                r#"UPDATE ontology_project_resources
                   SET is_deleted = TRUE, deleted_at = NOW(), deleted_by = $2
                   WHERE resource_id = $1"#,
            )
            .bind(resource_id)
            .bind(claims.sub)
            .execute(&state.ontology_db)
            .await
            .map(|_| ())
        }
        // Unsupported kinds were already filtered before reaching here.
        _ => Ok(()),
    }
}

/// Authorisation helper: the project owner or an admin may operate on
/// the resource. Project members in Phase 2 will gain editor rights via
/// `ensure_project_edit_access` from the ontology kernel.
async fn ensure_owner_or_admin(
    state: &AppState,
    claims: &Claims,
    kind: ResourceKind,
    resource_id: Uuid,
) -> Result<(), Response> {
    if claims.has_role("admin") {
        return Ok(());
    }

    let owner_lookup = match kind {
        ResourceKind::OntologyProject => sqlx::query_scalar::<_, Uuid>(
            "SELECT owner_id FROM ontology_projects WHERE id = $1",
        )
        .bind(resource_id)
        .fetch_optional(&state.ontology_db)
        .await,
        ResourceKind::OntologyFolder => sqlx::query_scalar::<_, Uuid>(
            r#"SELECT p.owner_id
               FROM ontology_project_folders f
               JOIN ontology_projects p ON p.id = f.project_id
               WHERE f.id = $1"#,
        )
        .bind(resource_id)
        .fetch_optional(&state.ontology_db)
        .await,
        ResourceKind::OntologyResourceBinding => sqlx::query_scalar::<_, Uuid>(
            r#"SELECT p.owner_id
               FROM ontology_project_resources r
               JOIN ontology_projects p ON p.id = r.project_id
               WHERE r.resource_id = $1"#,
        )
        .bind(resource_id)
        .fetch_optional(&state.ontology_db)
        .await,
        other => {
            return Err(bad(format!(
                "operation not supported for resource_kind '{}'",
                other.as_str()
            )));
        }
    };

    match owner_lookup {
        Ok(Some(owner)) if owner == claims.sub => Ok(()),
        Ok(Some(_)) => Err(forbidden(
            "only the project owner or an admin may perform this action",
        )),
        Ok(None) => Err((
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "resource not found" })),
        )
            .into_response()),
        Err(error) => Err(db_err("failed to load resource owner", error)),
    }
}
