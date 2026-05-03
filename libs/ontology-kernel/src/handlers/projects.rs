use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    domain::project_access::{
        OntologyResourceKind, ensure_project_edit_access, ensure_project_view_access,
        ensure_resource_manage_access, list_accessible_projects, load_resource_owner_id,
        load_resource_project_id,
    },
    models::project::{
        BindOntologyProjectResourceRequest, CreateOntologyProjectBranchRequest,
        CreateOntologyProjectMigrationRequest, CreateOntologyProjectProposalRequest,
        CreateOntologyProjectRequest, ListOntologyProjectBranchesResponse,
        ListOntologyProjectMembershipsResponse, ListOntologyProjectMigrationsResponse,
        ListOntologyProjectProposalsResponse, ListOntologyProjectResourcesResponse,
        ListOntologyProjectsQuery, ListOntologyProjectsResponse, OntologyProject,
        OntologyProjectBranch, OntologyProjectMembership, OntologyProjectMigration,
        OntologyProjectProposal, OntologyProjectResourceBinding, OntologyProjectWorkingState,
        ReplaceOntologyProjectWorkingStateRequest, UpdateOntologyProjectBranchRequest,
        UpdateOntologyProjectProposalRequest, UpdateOntologyProjectRequest,
        UpsertOntologyProjectMembershipRequest,
    },
};

fn invalid(message: impl Into<String>) -> Response {
    (
        StatusCode::BAD_REQUEST,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn forbidden(message: impl Into<String>) -> Response {
    (
        StatusCode::FORBIDDEN,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn db_error(message: impl Into<String>) -> Response {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn not_found(message: impl Into<String>) -> Response {
    (
        StatusCode::NOT_FOUND,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn normalize_slug(value: &str, field_name: &str) -> Result<String, String> {
    let normalized = value.trim().to_ascii_lowercase();
    if normalized.is_empty() {
        return Err(format!("{field_name} is required"));
    }
    if !normalized
        .chars()
        .all(|ch| ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-')
    {
        return Err(format!(
            "{field_name} must contain only lowercase letters, digits, and hyphens"
        ));
    }
    if normalized.starts_with('-') || normalized.ends_with('-') {
        return Err(format!("{field_name} cannot start or end with a hyphen"));
    }
    Ok(normalized)
}

fn normalize_optional_slug(
    value: Option<&str>,
    field_name: &str,
) -> Result<Option<String>, String> {
    match value.map(str::trim).filter(|value| !value.is_empty()) {
        Some(value) => normalize_slug(value, field_name).map(Some),
        None => Ok(None),
    }
}

fn normalize_branch_name(value: &str) -> Result<String, String> {
    let normalized = value.trim().to_ascii_lowercase();
    if normalized.is_empty() {
        return Err("branch name is required".to_string());
    }
    if !normalized
        .chars()
        .all(|ch| ch.is_ascii_lowercase() || ch.is_ascii_digit() || ch == '-' || ch == '/')
    {
        return Err(
            "branch name must contain only lowercase letters, digits, hyphens, and slashes"
                .to_string(),
        );
    }
    Ok(normalized)
}

async fn load_project(state: &AppState, id: Uuid) -> Result<Option<OntologyProject>, String> {
    crate::domain::pg_repository::typed::<OntologyProject>(
        r#"SELECT id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at
           FROM ontology_projects
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|error| format!("failed to load ontology project: {error}"))
}

fn ensure_project_owner_or_admin(
    project: &OntologyProject,
    claims: &auth_middleware::Claims,
) -> Result<(), String> {
    if claims.has_role("admin") || project.owner_id == claims.sub {
        Ok(())
    } else {
        Err("forbidden: only the ontology project owner can manage memberships or delete the project".to_string())
    }
}

pub async fn list_projects(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListOntologyProjectsQuery>,
) -> impl IntoResponse {
    let accessible = match list_accessible_projects(&state.db, &claims).await {
        Ok(accessible) => accessible,
        Err(error) => return db_error(format!("failed to evaluate project access: {error}")),
    };

    let page = query.page.unwrap_or(1).max(1);
    let per_page = query.per_page.unwrap_or(20).clamp(1, 100);
    let search_pattern = format!("%{}%", query.search.unwrap_or_default());

    let projects = match crate::domain::pg_repository::typed::<OntologyProject>(
        r#"SELECT id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at
           FROM ontology_projects
           WHERE slug ILIKE $1 OR display_name ILIKE $1
           ORDER BY created_at DESC"#,
    )
    .bind(&search_pattern)
    .fetch_all(&state.db)
    .await
    {
        Ok(projects) => projects,
        Err(error) => return db_error(format!("failed to list ontology projects: {error}")),
    };

    let visible = if claims.has_role("admin") {
        projects
    } else {
        projects
            .into_iter()
            .filter(|project| accessible.contains_key(&project.id))
            .collect::<Vec<_>>()
    };

    let total = visible.len() as i64;
    let offset = ((page - 1) * per_page) as usize;
    let data = visible
        .into_iter()
        .skip(offset)
        .take(per_page as usize)
        .collect::<Vec<_>>();

    Json(ListOntologyProjectsResponse {
        data,
        total,
        page,
        per_page,
    })
    .into_response()
}

pub async fn create_project(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreateOntologyProjectRequest>,
) -> impl IntoResponse {
    let slug = match normalize_slug(&body.slug, "slug") {
        Ok(slug) => slug,
        Err(error) => return invalid(error),
    };
    let workspace_slug =
        match normalize_optional_slug(body.workspace_slug.as_deref(), "workspace_slug") {
            Ok(workspace_slug) => workspace_slug,
            Err(error) => return invalid(error),
        };
    let display_name = body.display_name.unwrap_or_else(|| slug.clone());
    let description = body.description.unwrap_or_default();

    match crate::domain::pg_repository::typed::<OntologyProject>(
        r#"INSERT INTO ontology_projects (id, slug, display_name, description, workspace_slug, owner_id)
           VALUES ($1, $2, $3, $4, $5, $6)
           RETURNING id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(slug)
    .bind(display_name)
    .bind(description)
    .bind(workspace_slug)
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(project) => (StatusCode::CREATED, Json(project)).into_response(),
        Err(error) => db_error(format!("failed to create ontology project: {error}")),
    }
}

pub async fn get_project(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(project) = (match load_project(&state, id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_view_access(&state.db, &claims, id).await {
        return forbidden(error);
    }

    Json(project).into_response()
}

pub async fn update_project(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateOntologyProjectRequest>,
) -> impl IntoResponse {
    let Some(existing) = (match load_project(&state, id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_owner_or_admin(&existing, &claims) {
        return forbidden(error);
    }

    let workspace_slug = match body.workspace_slug {
        Some(Some(value)) => {
            match normalize_optional_slug(Some(value.as_str()), "workspace_slug") {
                Ok(workspace_slug) => workspace_slug,
                Err(error) => return invalid(error),
            }
        }
        Some(None) => None,
        None => existing.workspace_slug.clone(),
    };

    match crate::domain::pg_repository::typed::<OntologyProject>(
        r#"UPDATE ontology_projects
           SET display_name = COALESCE($2, display_name),
               description = COALESCE($3, description),
               workspace_slug = $4,
               updated_at = NOW()
           WHERE id = $1
           RETURNING id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at"#,
    )
    .bind(id)
    .bind(body.display_name)
    .bind(body.description)
    .bind(workspace_slug)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(project)) => Json(project).into_response(),
        Ok(None) => not_found("ontology project not found"),
        Err(error) => db_error(format!("failed to update ontology project: {error}")),
    }
}

pub async fn delete_project(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(project) = (match load_project(&state, id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_owner_or_admin(&project, &claims) {
        return forbidden(error);
    }

    match crate::domain::pg_repository::raw("DELETE FROM ontology_projects WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => not_found("ontology project not found"),
        Err(error) => db_error(format!("failed to delete ontology project: {error}")),
    }
}

pub async fn list_project_memberships(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_view_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectMembership>(
        r#"SELECT project_id, user_id, role, created_at, updated_at
           FROM ontology_project_memberships
           WHERE project_id = $1
           ORDER BY created_at ASC"#,
    )
    .bind(project_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(data) => Json(ListOntologyProjectMembershipsResponse { data }).into_response(),
        Err(error) => db_error(format!(
            "failed to list ontology project memberships: {error}"
        )),
    }
}

pub async fn upsert_project_membership(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
    Json(body): Json<UpsertOntologyProjectMembershipRequest>,
) -> impl IntoResponse {
    let Some(project) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_owner_or_admin(&project, &claims) {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectMembership>(
        r#"INSERT INTO ontology_project_memberships (project_id, user_id, role)
           VALUES ($1, $2, $3)
           ON CONFLICT (project_id, user_id)
           DO UPDATE SET role = EXCLUDED.role, updated_at = NOW()
           RETURNING project_id, user_id, role, created_at, updated_at"#,
    )
    .bind(project_id)
    .bind(body.user_id)
    .bind(body.role)
    .fetch_one(&state.db)
    .await
    {
        Ok(membership) => Json(membership).into_response(),
        Err(error) => db_error(format!(
            "failed to upsert ontology project membership: {error}"
        )),
    }
}

pub async fn delete_project_membership(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((project_id, user_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    let Some(project) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_owner_or_admin(&project, &claims) {
        return forbidden(error);
    }

    match crate::domain::pg_repository::raw(
        r#"DELETE FROM ontology_project_memberships
           WHERE project_id = $1 AND user_id = $2"#,
    )
    .bind(project_id)
    .bind(user_id)
    .execute(&state.db)
    .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => not_found("ontology project membership not found"),
        Err(error) => db_error(format!(
            "failed to delete ontology project membership: {error}"
        )),
    }
}

pub async fn list_project_resources(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_view_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectResourceBinding>(
        r#"SELECT project_id, resource_kind, resource_id, bound_by, created_at
           FROM ontology_project_resources
           WHERE project_id = $1
           ORDER BY created_at DESC"#,
    )
    .bind(project_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(data) => Json(ListOntologyProjectResourcesResponse { data }).into_response(),
        Err(error) => db_error(format!(
            "failed to list ontology project resources: {error}"
        )),
    }
}

pub async fn bind_project_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
    Json(body): Json<BindOntologyProjectResourceRequest>,
) -> impl IntoResponse {
    let resource_kind = match OntologyResourceKind::try_from(body.resource_kind.as_str()) {
        Ok(resource_kind) => resource_kind,
        Err(error) => return invalid(error),
    };

    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    let Some(owner_id) =
        (match load_resource_owner_id(&state.db, resource_kind, body.resource_id).await {
            Ok(owner_id) => owner_id,
            Err(error) => return db_error(error),
        })
    else {
        return not_found("ontology resource not found");
    };

    let existing_project_id =
        match load_resource_project_id(&state.db, resource_kind, body.resource_id).await {
            Ok(project_id) => project_id,
            Err(error) => {
                return db_error(format!("failed to load ontology resource binding: {error}"));
            }
        };

    if let Err(error) =
        ensure_resource_manage_access(&state.db, &claims, owner_id, existing_project_id).await
    {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectResourceBinding>(
        r#"INSERT INTO ontology_project_resources (project_id, resource_kind, resource_id, bound_by)
           VALUES ($1, $2, $3, $4)
           ON CONFLICT (resource_kind, resource_id)
           DO UPDATE SET project_id = EXCLUDED.project_id, bound_by = EXCLUDED.bound_by, created_at = NOW()
           RETURNING project_id, resource_kind, resource_id, bound_by, created_at"#,
    )
    .bind(project_id)
    .bind(resource_kind.as_str())
    .bind(body.resource_id)
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(binding) => Json(binding).into_response(),
        Err(error) => db_error(format!("failed to bind ontology resource to project: {error}")),
    }
}

pub async fn unbind_project_resource(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((project_id, resource_kind, resource_id)): Path<(Uuid, String, Uuid)>,
) -> impl IntoResponse {
    let resource_kind = match OntologyResourceKind::try_from(resource_kind.as_str()) {
        Ok(resource_kind) => resource_kind,
        Err(error) => return invalid(error),
    };

    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    let binding = match crate::domain::pg_repository::typed::<OntologyProjectResourceBinding>(
        r#"SELECT project_id, resource_kind, resource_id, bound_by, created_at
           FROM ontology_project_resources
           WHERE project_id = $1 AND resource_kind = $2 AND resource_id = $3"#,
    )
    .bind(project_id)
    .bind(resource_kind.as_str())
    .bind(resource_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(binding)) => binding,
        Ok(None) => return not_found("ontology project resource binding not found"),
        Err(error) => {
            return db_error(format!(
                "failed to load ontology project resource binding: {error}"
            ));
        }
    };

    let Some(owner_id) = (match load_resource_owner_id(&state.db, resource_kind, resource_id).await
    {
        Ok(owner_id) => owner_id,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology resource not found");
    };

    if let Err(error) =
        ensure_resource_manage_access(&state.db, &claims, owner_id, Some(binding.project_id)).await
    {
        return forbidden(error);
    }

    match crate::domain::pg_repository::raw(
        r#"DELETE FROM ontology_project_resources
           WHERE project_id = $1 AND resource_kind = $2 AND resource_id = $3"#,
    )
    .bind(project_id)
    .bind(resource_kind.as_str())
    .bind(resource_id)
    .execute(&state.db)
    .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => not_found("ontology project resource binding not found"),
        Err(error) => db_error(format!(
            "failed to unbind ontology resource from project: {error}"
        )),
    }
}

pub async fn get_project_working_state(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_view_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectWorkingState>(
        r#"SELECT project_id, changes, updated_by, updated_at
           FROM ontology_project_working_states
           WHERE project_id = $1"#,
    )
    .bind(project_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(working_state)) => Json(working_state).into_response(),
        Ok(None) => Json(OntologyProjectWorkingState {
            project_id,
            changes: json!([]),
            updated_by: claims.sub,
            updated_at: chrono::Utc::now(),
        })
        .into_response(),
        Err(error) => db_error(format!(
            "failed to load ontology project working state: {error}"
        )),
    }
}

pub async fn replace_project_working_state(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
    Json(body): Json<ReplaceOntologyProjectWorkingStateRequest>,
) -> impl IntoResponse {
    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectWorkingState>(
        r#"INSERT INTO ontology_project_working_states (project_id, changes, updated_by)
           VALUES ($1, $2, $3)
           ON CONFLICT (project_id)
           DO UPDATE SET changes = EXCLUDED.changes, updated_by = EXCLUDED.updated_by, updated_at = NOW()
           RETURNING project_id, changes, updated_by, updated_at"#,
    )
    .bind(project_id)
    .bind(body.changes)
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(working_state) => Json(working_state).into_response(),
        Err(error) => db_error(format!(
            "failed to replace ontology project working state: {error}"
        )),
    }
}

pub async fn list_project_branches(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_view_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectBranch>(
        r#"SELECT id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
                  enable_indexing, created_by, created_at, updated_at, latest_rebased_at
           FROM ontology_project_branches
           WHERE project_id = $1
           ORDER BY updated_at DESC"#,
    )
    .bind(project_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(data) => Json(ListOntologyProjectBranchesResponse { data }).into_response(),
        Err(error) => db_error(format!("failed to list ontology project branches: {error}")),
    }
}

pub async fn create_project_branch(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
    Json(body): Json<CreateOntologyProjectBranchRequest>,
) -> impl IntoResponse {
    let Some(_) = (match load_project(&state, project_id).await {
        Ok(project) => project,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology project not found");
    };

    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    let name = match normalize_branch_name(&body.name) {
        Ok(name) => name,
        Err(error) => return invalid(error),
    };

    match crate::domain::pg_repository::typed::<OntologyProjectBranch>(
        r#"INSERT INTO ontology_project_branches
              (id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
               enable_indexing, created_by, latest_rebased_at)
           VALUES ($1, $2, $3, $4, 'draft', NULL, $5, $6, $7, $8, NOW())
           RETURNING id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
                     enable_indexing, created_by, created_at, updated_at, latest_rebased_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(project_id)
    .bind(name)
    .bind(body.description.unwrap_or_else(|| "Isolated ontology branch for testing and review.".to_string()))
    .bind(body.changes)
    .bind(json!({}))
    .bind(body.enable_indexing.unwrap_or(false))
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(branch) => Json(branch).into_response(),
        Err(error) => db_error(format!("failed to create ontology project branch: {error}")),
    }
}

pub async fn update_project_branch(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((project_id, branch_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<UpdateOntologyProjectBranchRequest>,
) -> impl IntoResponse {
    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectBranch>(
        r#"UPDATE ontology_project_branches
           SET description = COALESCE($3, description),
               status = COALESCE($4, status),
               proposal_id = COALESCE($5, proposal_id),
               changes = COALESCE($6, changes),
               conflict_resolutions = COALESCE($7, conflict_resolutions),
               enable_indexing = COALESCE($8, enable_indexing),
               latest_rebased_at = COALESCE($9, latest_rebased_at),
               updated_at = NOW()
           WHERE project_id = $1 AND id = $2
           RETURNING id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
                     enable_indexing, created_by, created_at, updated_at, latest_rebased_at"#,
    )
    .bind(project_id)
    .bind(branch_id)
    .bind(body.description)
    .bind(body.status)
    .bind(body.proposal_id)
    .bind(body.changes)
    .bind(body.conflict_resolutions)
    .bind(body.enable_indexing)
    .bind(body.latest_rebased_at)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(branch)) => Json(branch).into_response(),
        Ok(None) => not_found("ontology project branch not found"),
        Err(error) => db_error(format!("failed to update ontology project branch: {error}")),
    }
}

pub async fn list_project_proposals(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
) -> impl IntoResponse {
    if let Err(error) = ensure_project_view_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectProposal>(
        r#"SELECT id, project_id, branch_id, title, description, status, reviewer_ids, tasks, comments,
                  created_by, created_at, updated_at
           FROM ontology_project_proposals
           WHERE project_id = $1
           ORDER BY updated_at DESC"#,
    )
    .bind(project_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(data) => Json(ListOntologyProjectProposalsResponse { data }).into_response(),
        Err(error) => db_error(format!("failed to list ontology project proposals: {error}")),
    }
}

pub async fn create_project_proposal(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
    Json(body): Json<CreateOntologyProjectProposalRequest>,
) -> impl IntoResponse {
    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    let branch = match crate::domain::pg_repository::typed::<OntologyProjectBranch>(
        r#"SELECT id, project_id, name, description, status, proposal_id, changes, conflict_resolutions,
                  enable_indexing, created_by, created_at, updated_at, latest_rebased_at
           FROM ontology_project_branches
           WHERE project_id = $1 AND id = $2"#,
    )
    .bind(project_id)
    .bind(body.branch_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(branch)) => branch,
        Ok(None) => return not_found("ontology project branch not found"),
        Err(error) => return db_error(format!("failed to load ontology project branch: {error}")),
    };

    let proposal = match crate::domain::pg_repository::typed::<OntologyProjectProposal>(
        r#"INSERT INTO ontology_project_proposals
              (id, project_id, branch_id, title, description, status, reviewer_ids, tasks, comments, created_by)
           VALUES ($1, $2, $3, $4, $5, 'in_review', $6, $7, $8, $9)
           RETURNING id, project_id, branch_id, title, description, status, reviewer_ids, tasks, comments,
                     created_by, created_at, updated_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(project_id)
    .bind(body.branch_id)
    .bind(body.title)
    .bind(body.description.unwrap_or_else(|| "Ontology proposal generated from the current branch.".to_string()))
    .bind(body.reviewer_ids.unwrap_or_else(|| json!([])))
    .bind(body.tasks)
    .bind(body.comments.unwrap_or_else(|| json!([])))
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(proposal) => proposal,
        Err(error) => return db_error(format!("failed to create ontology project proposal: {error}")),
    };

    match crate::domain::pg_repository::raw(
        r#"UPDATE ontology_project_branches
           SET status = 'in_review', proposal_id = $3, updated_at = NOW()
           WHERE project_id = $1 AND id = $2"#,
    )
    .bind(project_id)
    .bind(branch.id)
    .bind(proposal.id)
    .execute(&state.db)
    .await
    {
        Ok(_) => Json(proposal).into_response(),
        Err(error) => db_error(format!("failed to link proposal to branch: {error}")),
    }
}

pub async fn update_project_proposal(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((project_id, proposal_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<UpdateOntologyProjectProposalRequest>,
) -> impl IntoResponse {
    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectProposal>(
        r#"UPDATE ontology_project_proposals
           SET title = COALESCE($3, title),
               description = COALESCE($4, description),
               status = COALESCE($5, status),
               reviewer_ids = COALESCE($6, reviewer_ids),
               tasks = COALESCE($7, tasks),
               comments = COALESCE($8, comments),
               updated_at = NOW()
           WHERE project_id = $1 AND id = $2
           RETURNING id, project_id, branch_id, title, description, status, reviewer_ids, tasks, comments,
                     created_by, created_at, updated_at"#,
    )
    .bind(project_id)
    .bind(proposal_id)
    .bind(body.title)
    .bind(body.description)
    .bind(body.status)
    .bind(body.reviewer_ids)
    .bind(body.tasks)
    .bind(body.comments)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(proposal)) => Json(proposal).into_response(),
        Ok(None) => not_found("ontology project proposal not found"),
        Err(error) => db_error(format!("failed to update ontology project proposal: {error}")),
    }
}

pub async fn list_project_migrations(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
) -> impl IntoResponse {
    if let Err(error) = ensure_project_view_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectMigration>(
        r#"SELECT id, project_id, source_project_id, target_project_id, resources, submitted_at, status, note, submitted_by
           FROM ontology_project_migrations
           WHERE project_id = $1
           ORDER BY submitted_at DESC"#,
    )
    .bind(project_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(data) => Json(ListOntologyProjectMigrationsResponse { data }).into_response(),
        Err(error) => db_error(format!("failed to list ontology project migrations: {error}")),
    }
}

pub async fn create_project_migration(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(project_id): Path<Uuid>,
    Json(body): Json<CreateOntologyProjectMigrationRequest>,
) -> impl IntoResponse {
    if let Err(error) = ensure_project_edit_access(&state.db, &claims, project_id).await {
        return forbidden(error);
    }

    match crate::domain::pg_repository::typed::<OntologyProjectMigration>(
        r#"INSERT INTO ontology_project_migrations
              (id, project_id, source_project_id, target_project_id, resources, status, note, submitted_by)
           VALUES ($1, $2, $3, $4, $5, 'planned', $6, $7)
           RETURNING id, project_id, source_project_id, target_project_id, resources, submitted_at, status, note, submitted_by"#,
    )
    .bind(Uuid::now_v7())
    .bind(project_id)
    .bind(body.source_project_id)
    .bind(body.target_project_id)
    .bind(body.resources)
    .bind(body.note.unwrap_or_default())
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(migration) => Json(migration).into_response(),
        Err(error) => db_error(format!("failed to create ontology project migration: {error}")),
    }
}
