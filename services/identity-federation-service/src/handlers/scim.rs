use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde::Deserialize;
use serde_json::{Map, Value, json};
use sqlx::PgPool;
use uuid::Uuid;

use crate::AppState;
use crate::cedar_authz::{
    AdminAuthzGuard, ScimDeprovisionUser, ScimGroupResource, ScimProvisionGroup, ScimProvisionUser,
    ScimUserResource,
};
use crate::hardening::scim::{
    ROUTE_GROUPS, ROUTE_USERS, SCHEMA_GROUP, SCHEMA_OPENFOUNDRY_USER_EXTENSION, SCHEMA_PATCH_OP,
    SCHEMA_USER, ScimEmail, ScimError, ScimGroup, ScimGroupMember, ScimListResponse, ScimMeta,
    ScimName, ScimPatchOperation, ScimPatchRequest, ScimUser, resource_types, schema_resources,
    service_provider_config,
};
use crate::models::user::User;

#[derive(Debug, Clone, sqlx::FromRow)]
struct ScimGroupRow {
    id: Uuid,
    name: String,
    scim_external_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
enum ScimFilter {
    UserName(String),
    DisplayName(String),
    ExternalId(String),
}

#[derive(Debug, Default, Deserialize)]
pub struct ScimListQuery {
    pub filter: Option<String>,
    #[serde(rename = "startIndex")]
    pub start_index: Option<usize>,
    pub count: Option<usize>,
}

pub async fn service_provider_config_handler() -> impl IntoResponse {
    Json(service_provider_config(&scim_base_url())).into_response()
}

pub async fn list_schemas() -> impl IntoResponse {
    let resources = schema_resources(&scim_base_url());
    Json(ScimListResponse::new(resources, 2, 1)).into_response()
}

pub async fn get_schema(Path(id): Path<String>) -> impl IntoResponse {
    match schema_resources(&scim_base_url())
        .into_iter()
        .find(|schema| schema.id == id)
    {
        Some(schema) => Json(schema).into_response(),
        None => scim_error(StatusCode::NOT_FOUND, "SCIM schema not found", None),
    }
}

pub async fn list_resource_types() -> impl IntoResponse {
    let resources = resource_types(&scim_base_url());
    Json(ScimListResponse::new(resources, 2, 1)).into_response()
}

pub async fn get_resource_type(Path(id): Path<String>) -> impl IntoResponse {
    match resource_types(&scim_base_url())
        .into_iter()
        .find(|resource| resource.id.eq_ignore_ascii_case(&id))
    {
        Some(resource) => Json(resource).into_response(),
        None => scim_error(StatusCode::NOT_FOUND, "SCIM resource type not found", None),
    }
}

pub async fn create_user(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    _guard: AdminAuthzGuard<ScimProvisionUser, ScimUserResource>,
    Json(body): Json<ScimUser>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_writer(&claims) {
        return response;
    }
    let email = match primary_email(&body).or_else(|| Some(body.user_name.as_str())) {
        Some(value) if !value.trim().is_empty() => value.trim().to_string(),
        _ => {
            return scim_error(
                StatusCode::BAD_REQUEST,
                "userName or primary email is required",
                Some("invalidValue"),
            );
        }
    };
    let name = display_name_from_scim(body.name.as_ref(), &body.user_name);
    let attributes = user_attributes_from_scim(&body);
    let scim_external_id = scim_external_id(&body);
    let organization_id = match resolve_user_organization_id(&state.db, &body, &attributes).await {
        Ok(organization_id) => organization_id,
        Err(response) => return response,
    };

    if let Some(external_id) = scim_external_id.as_deref() {
        match load_user_by_scim_external_id(&state.db, external_id).await {
            Ok(Some(mut user)) => {
                merge_scim_user_record(
                    &mut user,
                    email,
                    name,
                    body.active.unwrap_or(true),
                    organization_id,
                    attributes,
                );
                return match update_scim_user(&state.db, &user, Some(external_id)).await {
                    Ok(()) => Json(user_to_scim(user)).into_response(),
                    Err(error) if is_unique_violation(&error) => scim_error(
                        StatusCode::CONFLICT,
                        "userName already exists",
                        Some("uniqueness"),
                    ),
                    Err(error) => {
                        tracing::error!("failed to update idempotent SCIM user: {error}");
                        scim_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "failed to create user",
                            None,
                        )
                    }
                };
            }
            Ok(None) => {}
            Err(error) => {
                tracing::error!("failed to resolve SCIM user externalId: {error}");
                return scim_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "failed to create user",
                    None,
                );
            }
        }
    }
    let user_id = Uuid::now_v7();

    let result = sqlx::query(
        r#"INSERT INTO users
           (id, email, name, password_hash, is_active, organization_id, attributes, auth_source, scim_external_id)
           VALUES ($1, $2, $3, $4, $5, $6, $7, 'scim', $8)"#,
    )
    .bind(user_id)
    .bind(&email)
    .bind(&name)
    .bind("SCIM_EXTERNAL_ACCOUNT")
    .bind(body.active.unwrap_or(true))
    .bind(organization_id)
    .bind(attributes)
    .bind(scim_external_id.as_deref())
    .execute(&state.db)
    .await;

    match result {
        Ok(_) => match load_user(&state.db, user_id).await {
            Ok(Some(user)) => (StatusCode::CREATED, Json(user_to_scim(user))).into_response(),
            Ok(None) => scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "created user not found",
                None,
            ),
            Err(error) => {
                tracing::error!("failed to load SCIM-created user: {error}");
                scim_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "failed to create user",
                    None,
                )
            }
        },
        Err(error) if is_unique_violation(&error) => scim_error(
            StatusCode::CONFLICT,
            "userName already exists",
            Some("uniqueness"),
        ),
        Err(error) => {
            tracing::error!("failed to create SCIM user: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to create user",
                None,
            )
        }
    }
}

pub async fn get_user(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_reader(&claims) {
        return response;
    }
    match load_user(&state.db, id).await {
        Ok(Some(user)) => Json(user_to_scim(user)).into_response(),
        Ok(None) => scim_error(StatusCode::NOT_FOUND, "SCIM user not found", None),
        Err(error) => {
            tracing::error!("failed to load SCIM user: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to load user",
                None,
            )
        }
    }
}

pub async fn list_users(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Query(query): Query<ScimListQuery>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_reader(&claims) {
        return response;
    }
    let start_index = query.start_index.unwrap_or(1).max(1);
    let count = query.count.unwrap_or(100).clamp(1, 500);
    let offset = (start_index - 1) as i64;
    let limit = count as i64;
    let filter = match parse_eq_filter(query.filter.as_deref(), &["userName", "externalId"]) {
        Ok(filter) => filter,
        Err(response) => return response,
    };

    let total_result = match &filter {
        Some(ScimFilter::UserName(user_name)) => {
            sqlx::query_scalar::<_, i64>("SELECT COUNT(*)::BIGINT FROM users WHERE email = $1")
                .bind(user_name)
                .fetch_one(&state.db)
                .await
        }
        Some(ScimFilter::ExternalId(external_id)) => {
            sqlx::query_scalar::<_, i64>(
                "SELECT COUNT(*)::BIGINT FROM users WHERE scim_external_id = $1",
            )
            .bind(external_id)
            .fetch_one(&state.db)
            .await
        }
        Some(ScimFilter::DisplayName(_)) => unreachable!("displayName is not a User filter"),
        None => {
            sqlx::query_scalar::<_, i64>("SELECT COUNT(*)::BIGINT FROM users")
                .fetch_one(&state.db)
                .await
        }
    };
    let total_results = match total_result {
        Ok(total) => total.max(0) as usize,
        Err(error) => {
            tracing::error!("failed to count SCIM users: {error}");
            return scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to list users",
                None,
            );
        }
    };

    let users_result =
        match &filter {
            Some(ScimFilter::UserName(user_name)) => sqlx::query_as::<_, User>(
                "SELECT id, email, name, password_hash, is_active, organization_id, attributes, \
                    mfa_enforced, auth_source, created_at, updated_at \
             FROM users WHERE email = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
            )
            .bind(user_name)
            .bind(limit)
            .bind(offset)
            .fetch_all(&state.db)
            .await,
            Some(ScimFilter::ExternalId(external_id)) => sqlx::query_as::<_, User>(
                "SELECT id, email, name, password_hash, is_active, organization_id, attributes, \
                    mfa_enforced, auth_source, created_at, updated_at \
             FROM users WHERE scim_external_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3",
            )
            .bind(external_id)
            .bind(limit)
            .bind(offset)
            .fetch_all(&state.db)
            .await,
            Some(ScimFilter::DisplayName(_)) => unreachable!("displayName is not a User filter"),
            None => sqlx::query_as::<_, User>(
                "SELECT id, email, name, password_hash, is_active, organization_id, attributes, \
                    mfa_enforced, auth_source, created_at, updated_at \
             FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2",
            )
            .bind(limit)
            .bind(offset)
            .fetch_all(&state.db)
            .await,
        };

    match users_result {
        Ok(users) => {
            let resources = users.into_iter().map(user_to_scim).collect::<Vec<_>>();
            Json(ScimListResponse::new(resources, total_results, start_index)).into_response()
        }
        Err(error) => {
            tracing::error!("failed to list SCIM users: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to list users",
                None,
            )
        }
    }
}

pub async fn patch_user(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    _guard: AdminAuthzGuard<ScimProvisionUser, ScimUserResource>,
    Json(body): Json<ScimPatchRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_writer(&claims) {
        return response;
    }
    if !body.schemas.iter().any(|schema| schema == SCHEMA_PATCH_OP) {
        return scim_error(
            StatusCode::BAD_REQUEST,
            "PATCH request is missing PatchOp schema",
            Some("invalidSyntax"),
        );
    }
    let Some(mut user) = (match load_user(&state.db, id).await {
        Ok(user) => user,
        Err(error) => {
            tracing::error!("failed to load SCIM user for patch: {error}");
            return scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to patch user",
                None,
            );
        }
    }) else {
        return scim_error(StatusCode::NOT_FOUND, "SCIM user not found", None);
    };

    for operation in body.operations {
        if let Err(response) = apply_user_patch(&mut user, operation) {
            return response;
        }
    }
    user.organization_id =
        match resolve_organization_from_attributes(&state.db, &user.attributes).await {
            Ok(organization_id) => organization_id.or(user.organization_id),
            Err(response) => return response,
        };
    let external_id = external_id_from_attributes(&user.attributes);

    match update_scim_user(&state.db, &user, external_id.as_deref()).await {
        Ok(_) => Json(user_to_scim(user)).into_response(),
        Err(error) if is_unique_violation(&error) => scim_error(
            StatusCode::CONFLICT,
            "userName already exists",
            Some("uniqueness"),
        ),
        Err(error) => {
            tracing::error!("failed to patch SCIM user: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to patch user",
                None,
            )
        }
    }
}

pub async fn delete_user(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    _guard: AdminAuthzGuard<ScimDeprovisionUser, ScimUserResource>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_writer(&claims) {
        return response;
    }
    match sqlx::query("UPDATE users SET is_active = false, updated_at = NOW() WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => scim_error(StatusCode::NOT_FOUND, "SCIM user not found", None),
        Err(error) => {
            tracing::error!("failed to delete SCIM user: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to delete user",
                None,
            )
        }
    }
}

pub async fn create_group(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    _guard: AdminAuthzGuard<ScimProvisionGroup, ScimGroupResource>,
    Json(body): Json<ScimGroup>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_writer(&claims) {
        return response;
    }
    if body.display_name.trim().is_empty() {
        return scim_error(
            StatusCode::BAD_REQUEST,
            "displayName is required",
            Some("invalidValue"),
        );
    }
    let scim_external_id = body
        .external_id
        .as_deref()
        .map(str::trim)
        .filter(|id| !id.is_empty());
    if let Some(external_id) = scim_external_id {
        match load_group_by_scim_external_id(&state.db, external_id).await {
            Ok(Some(mut group)) => {
                let mut tx = match state.db.begin().await {
                    Ok(tx) => tx,
                    Err(error) => {
                        tracing::error!(
                            "failed to start idempotent SCIM group transaction: {error}"
                        );
                        return scim_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "failed to create group",
                            None,
                        );
                    }
                };
                group.name = body.display_name.trim().to_string();
                if let Some(members) = body.members.as_deref() {
                    if let Err(response) =
                        replace_group_members_tx(&mut tx, group.id, members).await
                    {
                        return response;
                    }
                }
                let update =
                    sqlx::query("UPDATE groups SET name = $2, scim_external_id = $3 WHERE id = $1")
                        .bind(group.id)
                        .bind(&group.name)
                        .bind(external_id)
                        .execute(&mut *tx)
                        .await;
                if let Err(error) = update {
                    return if is_unique_violation(&error) {
                        scim_error(
                            StatusCode::CONFLICT,
                            "displayName already exists",
                            Some("uniqueness"),
                        )
                    } else {
                        tracing::error!("failed to update idempotent SCIM group: {error}");
                        scim_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "failed to create group",
                            None,
                        )
                    };
                }
                if let Err(error) = tx.commit().await {
                    tracing::error!("failed to commit idempotent SCIM group transaction: {error}");
                    return scim_error(
                        StatusCode::INTERNAL_SERVER_ERROR,
                        "failed to create group",
                        None,
                    );
                }
                return match load_group(&state.db, group.id).await {
                    Ok(Some(group)) => match group_to_scim(&state.db, group).await {
                        Ok(group) => Json(group).into_response(),
                        Err(error) => {
                            tracing::error!("failed to build idempotent SCIM group: {error}");
                            scim_error(
                                StatusCode::INTERNAL_SERVER_ERROR,
                                "failed to create group",
                                None,
                            )
                        }
                    },
                    Ok(None) => scim_error(
                        StatusCode::INTERNAL_SERVER_ERROR,
                        "created group not found",
                        None,
                    ),
                    Err(error) => {
                        tracing::error!("failed to reload idempotent SCIM group: {error}");
                        scim_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "failed to create group",
                            None,
                        )
                    }
                };
            }
            Ok(None) => {}
            Err(error) => {
                tracing::error!("failed to resolve SCIM group externalId: {error}");
                return scim_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "failed to create group",
                    None,
                );
            }
        }
    }
    let group_id = Uuid::now_v7();
    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("failed to start SCIM group transaction: {error}");
            return scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to create group",
                None,
            );
        }
    };

    let insert = sqlx::query(
        "INSERT INTO groups (id, name, description, scim_external_id) VALUES ($1, $2, NULL, $3)",
    )
    .bind(group_id)
    .bind(body.display_name.trim())
    .bind(scim_external_id)
    .execute(&mut *tx)
    .await;
    if let Err(error) = insert {
        return if is_unique_violation(&error) {
            scim_error(
                StatusCode::CONFLICT,
                "displayName already exists",
                Some("uniqueness"),
            )
        } else {
            tracing::error!("failed to create SCIM group: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to create group",
                None,
            )
        };
    }
    if let Some(members) = body.members.as_deref() {
        if let Err(response) = insert_group_members_tx(&mut tx, group_id, members).await {
            return response;
        }
    }
    if let Err(error) = tx.commit().await {
        tracing::error!("failed to commit SCIM group transaction: {error}");
        return scim_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "failed to create group",
            None,
        );
    }

    match load_group(&state.db, group_id).await {
        Ok(Some(group)) => match group_to_scim(&state.db, group).await {
            Ok(group) => (StatusCode::CREATED, Json(group)).into_response(),
            Err(error) => {
                tracing::error!("failed to build SCIM group: {error}");
                scim_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "failed to create group",
                    None,
                )
            }
        },
        Ok(None) => scim_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "created group not found",
            None,
        ),
        Err(error) => {
            tracing::error!("failed to load created SCIM group: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to create group",
                None,
            )
        }
    }
}

pub async fn get_group(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_reader(&claims) {
        return response;
    }
    match load_group(&state.db, id).await {
        Ok(Some(group)) => match group_to_scim(&state.db, group).await {
            Ok(group) => Json(group).into_response(),
            Err(error) => {
                tracing::error!("failed to build SCIM group: {error}");
                scim_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "failed to load group",
                    None,
                )
            }
        },
        Ok(None) => scim_error(StatusCode::NOT_FOUND, "SCIM group not found", None),
        Err(error) => {
            tracing::error!("failed to load SCIM group: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to load group",
                None,
            )
        }
    }
}

pub async fn list_groups(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Query(query): Query<ScimListQuery>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_reader(&claims) {
        return response;
    }
    let start_index = query.start_index.unwrap_or(1).max(1);
    let count = query.count.unwrap_or(100).clamp(1, 500);
    let offset = (start_index - 1) as i64;
    let limit = count as i64;
    let filter = match parse_eq_filter(query.filter.as_deref(), &["displayName", "externalId"]) {
        Ok(filter) => filter,
        Err(response) => return response,
    };

    let total_result = match &filter {
        Some(ScimFilter::DisplayName(display_name)) => {
            sqlx::query_scalar::<_, i64>("SELECT COUNT(*)::BIGINT FROM groups WHERE name = $1")
                .bind(display_name)
                .fetch_one(&state.db)
                .await
        }
        Some(ScimFilter::ExternalId(external_id)) => {
            sqlx::query_scalar::<_, i64>(
                "SELECT COUNT(*)::BIGINT FROM groups WHERE scim_external_id = $1",
            )
            .bind(external_id)
            .fetch_one(&state.db)
            .await
        }
        Some(ScimFilter::UserName(_)) => unreachable!("userName is not a Group filter"),
        None => {
            sqlx::query_scalar::<_, i64>("SELECT COUNT(*)::BIGINT FROM groups")
                .fetch_one(&state.db)
                .await
        }
    };
    let total_results = match total_result {
        Ok(total) => total.max(0) as usize,
        Err(error) => {
            tracing::error!("failed to count SCIM groups: {error}");
            return scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to list groups",
                None,
            );
        }
    };

    let groups_result =
        match &filter {
            Some(ScimFilter::DisplayName(display_name)) => {
                sqlx::query_as::<_, ScimGroupRow>(
                    "SELECT id, name, scim_external_id FROM groups WHERE name = $1 \
             ORDER BY name LIMIT $2 OFFSET $3",
                )
                .bind(display_name)
                .bind(limit)
                .bind(offset)
                .fetch_all(&state.db)
                .await
            }
            Some(ScimFilter::ExternalId(external_id)) => {
                sqlx::query_as::<_, ScimGroupRow>(
                    "SELECT id, name, scim_external_id FROM groups WHERE scim_external_id = $1 \
             ORDER BY name LIMIT $2 OFFSET $3",
                )
                .bind(external_id)
                .bind(limit)
                .bind(offset)
                .fetch_all(&state.db)
                .await
            }
            Some(ScimFilter::UserName(_)) => unreachable!("userName is not a Group filter"),
            None => sqlx::query_as::<_, ScimGroupRow>(
                "SELECT id, name, scim_external_id FROM groups ORDER BY name LIMIT $1 OFFSET $2",
            )
            .bind(limit)
            .bind(offset)
            .fetch_all(&state.db)
            .await,
        };

    match groups_result {
        Ok(groups) => {
            let mut resources = Vec::with_capacity(groups.len());
            for group in groups {
                match group_to_scim(&state.db, group).await {
                    Ok(group) => resources.push(group),
                    Err(error) => {
                        tracing::error!("failed to build SCIM group: {error}");
                        return scim_error(
                            StatusCode::INTERNAL_SERVER_ERROR,
                            "failed to list groups",
                            None,
                        );
                    }
                }
            }
            Json(ScimListResponse::new(resources, total_results, start_index)).into_response()
        }
        Err(error) => {
            tracing::error!("failed to list SCIM groups: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to list groups",
                None,
            )
        }
    }
}

pub async fn patch_group(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    _guard: AdminAuthzGuard<ScimProvisionGroup, ScimGroupResource>,
    Json(body): Json<ScimPatchRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_writer(&claims) {
        return response;
    }
    let Some(mut group) = (match load_group(&state.db, id).await {
        Ok(group) => group,
        Err(error) => {
            tracing::error!("failed to load SCIM group for patch: {error}");
            return scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to patch group",
                None,
            );
        }
    }) else {
        return scim_error(StatusCode::NOT_FOUND, "SCIM group not found", None);
    };

    let mut tx = match state.db.begin().await {
        Ok(tx) => tx,
        Err(error) => {
            tracing::error!("failed to start SCIM group patch transaction: {error}");
            return scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to patch group",
                None,
            );
        }
    };

    for operation in body.operations {
        if let Err(response) = apply_group_patch(&mut tx, &mut group, operation).await {
            return response;
        }
    }

    let update = sqlx::query("UPDATE groups SET name = $2, scim_external_id = $3 WHERE id = $1")
        .bind(group.id)
        .bind(&group.name)
        .bind(group.scim_external_id.as_deref())
        .execute(&mut *tx)
        .await;
    if let Err(error) = update {
        return if is_unique_violation(&error) {
            scim_error(
                StatusCode::CONFLICT,
                "displayName already exists",
                Some("uniqueness"),
            )
        } else {
            tracing::error!("failed to patch SCIM group: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to patch group",
                None,
            )
        };
    }
    if let Err(error) = tx.commit().await {
        tracing::error!("failed to commit SCIM group patch transaction: {error}");
        return scim_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "failed to patch group",
            None,
        );
    }

    match load_group(&state.db, group.id).await {
        Ok(Some(group)) => match group_to_scim(&state.db, group).await {
            Ok(group) => Json(group).into_response(),
            Err(error) => {
                tracing::error!("failed to build patched SCIM group: {error}");
                scim_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "failed to patch group",
                    None,
                )
            }
        },
        Ok(None) => scim_error(StatusCode::NOT_FOUND, "SCIM group not found", None),
        Err(error) => {
            tracing::error!("failed to reload patched SCIM group: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to patch group",
                None,
            )
        }
    }
}

pub async fn delete_group(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    _guard: AdminAuthzGuard<ScimProvisionGroup, ScimGroupResource>,
) -> impl IntoResponse {
    if let Err(response) = require_scim_writer(&claims) {
        return response;
    }
    match sqlx::query("DELETE FROM groups WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => scim_error(StatusCode::NOT_FOUND, "SCIM group not found", None),
        Err(error) => {
            tracing::error!("failed to delete SCIM group: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to delete group",
                None,
            )
        }
    }
}

fn scim_error(status: StatusCode, detail: impl Into<String>, scim_type: Option<&str>) -> Response {
    (
        status,
        Json(ScimError::new(status.as_u16(), detail, scim_type)),
    )
        .into_response()
}

fn require_scim_reader(claims: &auth_middleware::Claims) -> Result<(), Response> {
    if is_scim_service_account(claims)
        || claims.has_permission("users", "read")
        || claims.has_permission("groups", "read")
    {
        Ok(())
    } else {
        Err(scim_error(
            StatusCode::FORBIDDEN,
            "missing SCIM read permission",
            None,
        ))
    }
}

fn require_scim_writer(claims: &auth_middleware::Claims) -> Result<(), Response> {
    if is_scim_service_account(claims) {
        Ok(())
    } else {
        Err(scim_error(
            StatusCode::FORBIDDEN,
            "SCIM provisioning requires service_account principal with scim_writer role",
            None,
        ))
    }
}

fn is_scim_service_account(claims: &auth_middleware::Claims) -> bool {
    claims.has_role("scim_writer")
        && claims
            .attributes
            .get("kind")
            .and_then(Value::as_str)
            .is_some_and(|kind| kind == "service_account")
}

fn scim_base_url() -> String {
    std::env::var("OF_SCIM_BASE_URL").unwrap_or_default()
}

fn user_location(id: Uuid) -> String {
    format!(
        "{}{ROUTE_USERS}/{id}",
        scim_base_url().trim_end_matches('/')
    )
}

fn group_location(id: Uuid) -> String {
    format!(
        "{}{ROUTE_GROUPS}/{id}",
        scim_base_url().trim_end_matches('/')
    )
}

fn user_to_scim(user: User) -> ScimUser {
    let name = scim_name_from_attributes(&user.attributes).unwrap_or_else(|| ScimName {
        given_name: None,
        family_name: None,
        formatted: Some(user.name.clone()),
    });
    let mut extensions = Map::new();
    if let Some(extension) = user.attributes.pointer("/scim/openfoundry").cloned() {
        extensions.insert(SCHEMA_OPENFOUNDRY_USER_EXTENSION.into(), extension);
    } else if let Some(organization_id) = user.organization_id {
        extensions.insert(
            SCHEMA_OPENFOUNDRY_USER_EXTENSION.into(),
            json!({ "organizationId": organization_id }),
        );
    }
    ScimUser {
        schemas: vec![SCHEMA_USER.into()],
        id: Some(user.id.to_string()),
        user_name: user.email.clone(),
        name: Some(name),
        emails: Some(vec![ScimEmail {
            value: user.email.clone(),
            primary: Some(true),
            type_: Some("work".into()),
        }]),
        active: Some(user.is_active),
        external_id: user
            .attributes
            .pointer("/scim/externalId")
            .and_then(Value::as_str)
            .map(ToString::to_string),
        meta: Some(ScimMeta {
            resource_type: "User".into(),
            location: user_location(user.id),
        }),
        extensions,
    }
}

async fn group_to_scim(pool: &PgPool, group: ScimGroupRow) -> Result<ScimGroup, sqlx::Error> {
    Ok(ScimGroup {
        schemas: vec![SCHEMA_GROUP.into()],
        id: Some(group.id.to_string()),
        display_name: group.name,
        members: Some(load_group_members(pool, group.id).await?),
        external_id: group.scim_external_id,
        meta: Some(ScimMeta {
            resource_type: "Group".into(),
            location: group_location(group.id),
        }),
        extensions: Map::new(),
    })
}

async fn load_group_members(
    pool: &PgPool,
    group_id: Uuid,
) -> Result<Vec<ScimGroupMember>, sqlx::Error> {
    let rows = sqlx::query_as::<_, (Uuid, String, String)>(
        "SELECT u.id, u.email, u.name \
         FROM users u INNER JOIN group_members gm ON gm.user_id = u.id \
         WHERE gm.group_id = $1 ORDER BY u.email",
    )
    .bind(group_id)
    .fetch_all(pool)
    .await?;
    Ok(rows
        .into_iter()
        .map(|(id, email, name)| ScimGroupMember {
            value: id.to_string(),
            ref_: Some(user_location(id)),
            type_: Some("User".into()),
            display: Some(if name.is_empty() { email } else { name }),
        })
        .collect())
}

async fn load_user(pool: &PgPool, id: Uuid) -> Result<Option<User>, sqlx::Error> {
    sqlx::query_as::<_, User>(
        "SELECT id, email, name, password_hash, is_active, organization_id, attributes, \
                mfa_enforced, auth_source, created_at, updated_at \
         FROM users WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(pool)
    .await
}

async fn load_user_by_scim_external_id(
    pool: &PgPool,
    external_id: &str,
) -> Result<Option<User>, sqlx::Error> {
    sqlx::query_as::<_, User>(
        "SELECT id, email, name, password_hash, is_active, organization_id, attributes, \
                mfa_enforced, auth_source, created_at, updated_at \
         FROM users WHERE scim_external_id = $1",
    )
    .bind(external_id)
    .fetch_optional(pool)
    .await
}

async fn update_scim_user(
    pool: &PgPool,
    user: &User,
    external_id: Option<&str>,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        "UPDATE users SET email = $2, name = $3, is_active = $4, organization_id = $5, \
                attributes = $6, scim_external_id = $7, updated_at = NOW() \
         WHERE id = $1",
    )
    .bind(user.id)
    .bind(&user.email)
    .bind(&user.name)
    .bind(user.is_active)
    .bind(user.organization_id)
    .bind(&user.attributes)
    .bind(external_id)
    .execute(pool)
    .await?;
    Ok(())
}

async fn load_group(pool: &PgPool, id: Uuid) -> Result<Option<ScimGroupRow>, sqlx::Error> {
    sqlx::query_as::<_, ScimGroupRow>("SELECT id, name, scim_external_id FROM groups WHERE id = $1")
        .bind(id)
        .fetch_optional(pool)
        .await
}

async fn load_group_by_scim_external_id(
    pool: &PgPool,
    external_id: &str,
) -> Result<Option<ScimGroupRow>, sqlx::Error> {
    sqlx::query_as::<_, ScimGroupRow>(
        "SELECT id, name, scim_external_id \
         FROM groups WHERE scim_external_id = $1",
    )
    .bind(external_id)
    .fetch_optional(pool)
    .await
}

fn primary_email(user: &ScimUser) -> Option<&str> {
    user.emails
        .as_deref()
        .and_then(|emails| {
            emails
                .iter()
                .find(|email| email.primary.unwrap_or(false))
                .or_else(|| emails.first())
        })
        .map(|email| email.value.as_str())
}

fn display_name_from_scim(name: Option<&ScimName>, fallback: &str) -> String {
    let Some(name) = name else {
        return fallback.to_string();
    };
    if let Some(formatted) = name.formatted.as_deref().filter(|value| !value.is_empty()) {
        return formatted.to_string();
    }
    let joined = [name.given_name.as_deref(), name.family_name.as_deref()]
        .into_iter()
        .flatten()
        .collect::<Vec<_>>()
        .join(" ");
    if joined.trim().is_empty() {
        fallback.to_string()
    } else {
        joined
    }
}

fn user_attributes_from_scim(user: &ScimUser) -> Value {
    let mut scim = Map::new();
    if let Some(external_id) = user.external_id.as_deref() {
        scim.insert("externalId".into(), Value::String(external_id.into()));
    }
    if let Some(name) = user.name.as_ref() {
        scim.insert(
            "name".into(),
            serde_json::to_value(name).unwrap_or(Value::Null),
        );
    }
    if let Some(extension) = user.extensions.get(SCHEMA_OPENFOUNDRY_USER_EXTENSION) {
        scim.insert("openfoundry".into(), extension.clone());
    }
    json!({ "scim": scim })
}

fn scim_external_id(user: &ScimUser) -> Option<String> {
    user.external_id
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToString::to_string)
}

fn merge_scim_user_record(
    user: &mut User,
    email: String,
    name: String,
    active: bool,
    organization_id: Option<Uuid>,
    attributes: Value,
) {
    user.email = email;
    user.name = name;
    user.is_active = active;
    user.organization_id = organization_id;
    user.attributes = attributes;
}

#[cfg(test)]
fn deactivate_scim_user_record(user: &mut User) {
    user.is_active = false;
}

fn external_id_from_attributes(attributes: &Value) -> Option<String> {
    attributes
        .pointer("/scim/externalId")
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToString::to_string)
}

async fn resolve_user_organization_id(
    pool: &PgPool,
    user: &ScimUser,
    attributes: &Value,
) -> Result<Option<Uuid>, Response> {
    if let Some(id) = user
        .extensions
        .get(SCHEMA_OPENFOUNDRY_USER_EXTENSION)
        .and_then(|extension| {
            extension
                .get("organizationId")
                .or_else(|| extension.get("organization_id"))
        })
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return Uuid::parse_str(id).map(Some).map_err(|_| {
            scim_error(
                StatusCode::BAD_REQUEST,
                "organizationId must be a UUID",
                Some("invalidValue"),
            )
        });
    }
    resolve_organization_from_attributes(pool, attributes).await
}

async fn resolve_organization_from_attributes(
    pool: &PgPool,
    attributes: &Value,
) -> Result<Option<Uuid>, Response> {
    if let Some(id) = attributes
        .pointer("/scim/openfoundry/organizationId")
        .or_else(|| attributes.pointer("/scim/openfoundry/organization_id"))
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    {
        return Uuid::parse_str(id).map(Some).map_err(|_| {
            scim_error(
                StatusCode::BAD_REQUEST,
                "organizationId must be a UUID",
                Some("invalidValue"),
            )
        });
    }
    let Some(slug) = attributes
        .pointer("/scim/openfoundry/organizationSlug")
        .or_else(|| attributes.pointer("/scim/openfoundry/organization"))
        .and_then(Value::as_str)
        .map(str::trim)
        .filter(|value| !value.is_empty())
    else {
        return Ok(None);
    };
    resolve_organization_slug(pool, slug).await
}

async fn resolve_organization_slug(pool: &PgPool, slug: &str) -> Result<Option<Uuid>, Response> {
    match sqlx::query_scalar::<_, Uuid>("SELECT id FROM tenancy_organizations WHERE slug = $1")
        .bind(slug)
        .fetch_optional(pool)
        .await
    {
        Ok(Some(id)) => Ok(Some(id)),
        Ok(None) => Err(scim_error(
            StatusCode::BAD_REQUEST,
            format!("organizationSlug {slug} does not exist"),
            Some("invalidValue"),
        )),
        Err(error) => {
            tracing::error!("failed to resolve SCIM organization slug: {error}");
            Err(scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to resolve organizationSlug",
                None,
            ))
        }
    }
}

fn set_scim_name(attributes: &mut Value, name: &ScimName) {
    if !attributes.is_object() {
        *attributes = json!({});
    }
    let object = attributes.as_object_mut().expect("attributes object");
    let scim = object
        .entry("scim")
        .or_insert_with(|| json!({}))
        .as_object_mut()
        .expect("scim attributes object");
    scim.insert(
        "name".into(),
        serde_json::to_value(name).unwrap_or(Value::Null),
    );
}

fn scim_name_from_attributes(attributes: &Value) -> Option<ScimName> {
    attributes
        .pointer("/scim/name")
        .cloned()
        .and_then(|value| serde_json::from_value(value).ok())
}

fn parse_eq_filter(
    filter: Option<&str>,
    supported_attrs: &[&str],
) -> Result<Option<ScimFilter>, Response> {
    let Some(filter) = filter.map(str::trim).filter(|filter| !filter.is_empty()) else {
        return Ok(None);
    };
    let lower = filter.to_ascii_lowercase();
    let Some(supported_attr) = supported_attrs
        .iter()
        .find(|attr| lower.starts_with(&format!("{} eq ", attr.to_ascii_lowercase())))
    else {
        return Err(scim_error(
            StatusCode::BAD_REQUEST,
            format!("unsupported SCIM filter: {filter}"),
            Some("invalidFilter"),
        ));
    };
    let prefix = format!("{} eq ", supported_attr.to_ascii_lowercase());
    let value = filter[prefix.len()..].trim();
    if value.len() < 2 || !value.starts_with('"') || !value.ends_with('"') {
        return Err(scim_error(
            StatusCode::BAD_REQUEST,
            "SCIM filter value must be quoted",
            Some("invalidFilter"),
        ));
    }
    let value = value[1..value.len() - 1].to_string();
    match *supported_attr {
        "userName" => Ok(Some(ScimFilter::UserName(value))),
        "displayName" => Ok(Some(ScimFilter::DisplayName(value))),
        "externalId" => Ok(Some(ScimFilter::ExternalId(value))),
        _ => Err(scim_error(
            StatusCode::BAD_REQUEST,
            format!("unsupported SCIM filter: {filter}"),
            Some("invalidFilter"),
        )),
    }
}

fn apply_user_patch(user: &mut User, operation: ScimPatchOperation) -> Result<(), Response> {
    let op = operation.op.to_ascii_lowercase();
    match op.as_str() {
        "add" | "replace" => apply_user_replace(user, operation.path.as_deref(), operation.value),
        "remove" => Err(scim_error(
            StatusCode::BAD_REQUEST,
            "remove is unsupported for SCIM User; use DELETE to deactivate",
            Some("mutability"),
        )),
        _ => Err(scim_error(
            StatusCode::BAD_REQUEST,
            format!("unsupported PATCH op {}", operation.op),
            Some("invalidSyntax"),
        )),
    }
}

fn apply_user_replace(
    user: &mut User,
    path: Option<&str>,
    value: Option<Value>,
) -> Result<(), Response> {
    let Some(value) = value else {
        return Err(scim_error(
            StatusCode::BAD_REQUEST,
            "PATCH operation value is required",
            Some("invalidValue"),
        ));
    };
    if let Some(path) = path {
        return apply_user_field(user, path, value);
    }
    let Some(object) = value.as_object() else {
        return Err(scim_error(
            StatusCode::BAD_REQUEST,
            "pathless User PATCH requires an object value",
            Some("invalidValue"),
        ));
    };
    for (field, value) in object {
        apply_user_field(user, field, value.clone())?;
    }
    Ok(())
}

fn apply_user_field(user: &mut User, path: &str, value: Value) -> Result<(), Response> {
    match path {
        "userName" => {
            let Some(email) = value
                .as_str()
                .map(str::trim)
                .filter(|value| !value.is_empty())
            else {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    "userName must be a non-empty string",
                    Some("invalidValue"),
                ));
            };
            user.email = email.to_string();
            Ok(())
        }
        "active" => {
            let Some(active) = value.as_bool() else {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    "active must be a boolean",
                    Some("invalidValue"),
                ));
            };
            user.is_active = active;
            Ok(())
        }
        "name" => {
            let name: ScimName = serde_json::from_value(value).map_err(|_| {
                scim_error(
                    StatusCode::BAD_REQUEST,
                    "name must be a SCIM name object",
                    Some("invalidValue"),
                )
            })?;
            user.name = display_name_from_scim(Some(&name), &user.email);
            set_scim_name(&mut user.attributes, &name);
            Ok(())
        }
        "emails" => {
            let emails: Vec<ScimEmail> = serde_json::from_value(value).map_err(|_| {
                scim_error(
                    StatusCode::BAD_REQUEST,
                    "emails must be a SCIM email array",
                    Some("invalidValue"),
                )
            })?;
            let Some(email) = emails
                .iter()
                .find(|email| email.primary.unwrap_or(false))
                .or_else(|| emails.first())
                .map(|email| email.value.trim())
                .filter(|email| !email.is_empty())
            else {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    "emails must contain at least one value",
                    Some("invalidValue"),
                ));
            };
            user.email = email.to_string();
            Ok(())
        }
        "externalId" => {
            let Some(external_id) = value.as_str() else {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    "externalId must be a string",
                    Some("invalidValue"),
                ));
            };
            if !user.attributes.is_object() {
                user.attributes = json!({});
            }
            let object = user.attributes.as_object_mut().expect("attributes object");
            let scim = object
                .entry("scim")
                .or_insert_with(|| json!({}))
                .as_object_mut()
                .expect("scim attributes object");
            scim.insert("externalId".into(), Value::String(external_id.into()));
            Ok(())
        }
        path if path == SCHEMA_OPENFOUNDRY_USER_EXTENSION => {
            if !value.is_object() {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    "OpenFoundry SCIM extension must be an object",
                    Some("invalidValue"),
                ));
            }
            if !user.attributes.is_object() {
                user.attributes = json!({});
            }
            let object = user.attributes.as_object_mut().expect("attributes object");
            let scim = object
                .entry("scim")
                .or_insert_with(|| json!({}))
                .as_object_mut()
                .expect("scim attributes object");
            scim.insert("openfoundry".into(), value);
            Ok(())
        }
        path if path.starts_with(SCHEMA_OPENFOUNDRY_USER_EXTENSION) => {
            let Some(field) = path
                .strip_prefix(SCHEMA_OPENFOUNDRY_USER_EXTENSION)
                .and_then(|value| value.strip_prefix('.'))
            else {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    format!("unsupported User PATCH path {path}"),
                    Some("mutability"),
                ));
            };
            if !user.attributes.is_object() {
                user.attributes = json!({});
            }
            let object = user.attributes.as_object_mut().expect("attributes object");
            let scim = object
                .entry("scim")
                .or_insert_with(|| json!({}))
                .as_object_mut()
                .expect("scim attributes object");
            let extension = scim
                .entry("openfoundry")
                .or_insert_with(|| json!({}))
                .as_object_mut()
                .expect("openfoundry attributes object");
            extension.insert(field.to_string(), value);
            Ok(())
        }
        other => Err(scim_error(
            StatusCode::BAD_REQUEST,
            format!("unsupported User PATCH path {other}"),
            Some("mutability"),
        )),
    }
}

async fn apply_group_patch(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    group: &mut ScimGroupRow,
    operation: ScimPatchOperation,
) -> Result<(), Response> {
    let op = operation.op.to_ascii_lowercase();
    match op.as_str() {
        "add" => {
            if operation.path.as_deref() != Some("members") {
                return Err(
                    ScimError::unsupported("only members add is supported for Group")
                        .into_response_with_status(StatusCode::BAD_REQUEST),
                );
            }
            let members = members_from_value(operation.value)?;
            insert_group_members_tx(tx, group.id, &members).await
        }
        "replace" => {
            if let Some(path) = operation.path.as_deref() {
                match path {
                    "displayName" => {
                        let Some(display_name) = operation.value.as_ref().and_then(Value::as_str)
                        else {
                            return Err(scim_error(
                                StatusCode::BAD_REQUEST,
                                "displayName must be a string",
                                Some("invalidValue"),
                            ));
                        };
                        group.name = display_name.trim().to_string();
                        Ok(())
                    }
                    "externalId" => {
                        let Some(external_id) = operation.value.as_ref().and_then(Value::as_str)
                        else {
                            return Err(scim_error(
                                StatusCode::BAD_REQUEST,
                                "externalId must be a string",
                                Some("invalidValue"),
                            ));
                        };
                        group.scim_external_id =
                            Some(external_id.trim().to_string()).filter(|value| !value.is_empty());
                        Ok(())
                    }
                    "members" => {
                        sqlx::query("DELETE FROM group_members WHERE group_id = $1")
                            .bind(group.id)
                            .execute(&mut **tx)
                            .await
                            .map_err(|error| {
                                tracing::error!("failed to clear SCIM group members: {error}");
                                scim_error(
                                    StatusCode::INTERNAL_SERVER_ERROR,
                                    "failed to patch group",
                                    None,
                                )
                            })?;
                        let members = members_from_value(operation.value)?;
                        insert_group_members_tx(tx, group.id, &members).await
                    }
                    other => Err(scim_error(
                        StatusCode::BAD_REQUEST,
                        format!("unsupported Group PATCH path {other}"),
                        Some("mutability"),
                    )),
                }
            } else {
                let Some(object) = operation.value.as_ref().and_then(Value::as_object) else {
                    return Err(scim_error(
                        StatusCode::BAD_REQUEST,
                        "pathless Group PATCH requires an object value",
                        Some("invalidValue"),
                    ));
                };
                if let Some(display_name) = object.get("displayName").and_then(Value::as_str) {
                    group.name = display_name.trim().to_string();
                }
                if let Some(external_id) = object.get("externalId").and_then(Value::as_str) {
                    group.scim_external_id =
                        Some(external_id.trim().to_string()).filter(|value| !value.is_empty());
                }
                if let Some(value) = object.get("members") {
                    sqlx::query("DELETE FROM group_members WHERE group_id = $1")
                        .bind(group.id)
                        .execute(&mut **tx)
                        .await
                        .map_err(|error| {
                            tracing::error!("failed to clear SCIM group members: {error}");
                            scim_error(
                                StatusCode::INTERNAL_SERVER_ERROR,
                                "failed to patch group",
                                None,
                            )
                        })?;
                    let members = members_from_value(Some(value.clone()))?;
                    insert_group_members_tx(tx, group.id, &members).await?;
                }
                Ok(())
            }
        }
        "remove" => remove_group_members_tx(tx, group.id, operation.path.as_deref()).await,
        _ => Err(scim_error(
            StatusCode::BAD_REQUEST,
            format!("unsupported PATCH op {}", operation.op),
            Some("invalidSyntax"),
        )),
    }
}

trait ScimErrorResponseExt {
    fn into_response_with_status(self, status: StatusCode) -> Response;
}

impl ScimErrorResponseExt for ScimError {
    fn into_response_with_status(self, status: StatusCode) -> Response {
        (status, Json(self)).into_response()
    }
}

fn members_from_value(value: Option<Value>) -> Result<Vec<ScimGroupMember>, Response> {
    let Some(value) = value else {
        return Err(scim_error(
            StatusCode::BAD_REQUEST,
            "members value is required",
            Some("invalidValue"),
        ));
    };
    if value.is_array() {
        serde_json::from_value(value)
    } else {
        serde_json::from_value::<ScimGroupMember>(value).map(|member| vec![member])
    }
    .map_err(|_| {
        scim_error(
            StatusCode::BAD_REQUEST,
            "members must be SCIM member objects",
            Some("invalidValue"),
        )
    })
}

async fn insert_group_members_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    group_id: Uuid,
    members: &[ScimGroupMember],
) -> Result<(), Response> {
    for member in members {
        let user_id = Uuid::parse_str(&member.value).map_err(|_| {
            scim_error(
                StatusCode::BAD_REQUEST,
                "member value must be a user UUID",
                Some("invalidValue"),
            )
        })?;
        sqlx::query(
            "INSERT INTO group_members (group_id, user_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
        )
        .bind(group_id)
        .bind(user_id)
        .execute(&mut **tx)
        .await
        .map_err(|error| {
            tracing::error!("failed to insert SCIM group member: {error}");
            scim_error(
                StatusCode::BAD_REQUEST,
                "group member does not reference an existing user",
                Some("invalidValue"),
            )
        })?;
    }
    Ok(())
}

async fn replace_group_members_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    group_id: Uuid,
    members: &[ScimGroupMember],
) -> Result<(), Response> {
    sqlx::query("DELETE FROM group_members WHERE group_id = $1")
        .bind(group_id)
        .execute(&mut **tx)
        .await
        .map_err(|error| {
            tracing::error!("failed to replace SCIM group members: {error}");
            scim_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to replace group members",
                None,
            )
        })?;
    insert_group_members_tx(tx, group_id, members).await
}

async fn remove_group_members_tx(
    tx: &mut sqlx::Transaction<'_, sqlx::Postgres>,
    group_id: Uuid,
    path: Option<&str>,
) -> Result<(), Response> {
    match path {
        Some("members") | None => {
            sqlx::query("DELETE FROM group_members WHERE group_id = $1")
                .bind(group_id)
                .execute(&mut **tx)
                .await
        }
        Some(path) => {
            let Some(user_id) = parse_member_filter_path(path) else {
                return Err(scim_error(
                    StatusCode::BAD_REQUEST,
                    "unsupported members remove path",
                    Some("invalidPath"),
                ));
            };
            sqlx::query("DELETE FROM group_members WHERE group_id = $1 AND user_id = $2")
                .bind(group_id)
                .bind(user_id)
                .execute(&mut **tx)
                .await
        }
    }
    .map_err(|error| {
        tracing::error!("failed to remove SCIM group members: {error}");
        scim_error(
            StatusCode::INTERNAL_SERVER_ERROR,
            "failed to patch group",
            None,
        )
    })?;
    Ok(())
}

fn parse_member_filter_path(path: &str) -> Option<Uuid> {
    let path = path.trim();
    let prefix = "members[value eq \"";
    let suffix = "\"]";
    path.strip_prefix(prefix)
        .and_then(|rest| rest.strip_suffix(suffix))
        .and_then(|value| Uuid::parse_str(value).ok())
}

fn is_unique_violation(error: &sqlx::Error) -> bool {
    matches!(
        error,
        sqlx::Error::Database(db_error) if db_error.code().as_deref() == Some("23505")
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::{DateTime, Utc};

    #[test]
    fn parses_supported_eq_filter() {
        assert_eq!(
            parse_eq_filter(Some("userName eq \"alice@example.com\""), &["userName"]).unwrap(),
            Some(ScimFilter::UserName("alice@example.com".into()))
        );
        assert_eq!(
            parse_eq_filter(Some("externalId eq \"00u1\""), &["userName", "externalId"]).unwrap(),
            Some(ScimFilter::ExternalId("00u1".into()))
        );
    }

    #[test]
    fn rejects_unsupported_filter_as_scim_error() {
        let response = parse_eq_filter(Some("emails.value co \"example.com\""), &["userName"])
            .expect_err("unsupported filter");
        assert_eq!(response.status(), StatusCode::BAD_REQUEST);
    }

    #[test]
    fn parses_member_remove_path() {
        let id = Uuid::now_v7();
        assert_eq!(
            parse_member_filter_path(&format!("members[value eq \"{id}\"]")),
            Some(id)
        );
    }

    #[test]
    fn user_transform_preserves_scim_contract() {
        let id = Uuid::now_v7();
        let user = User {
            id,
            email: "alice@example.com".into(),
            name: "Alice Example".into(),
            password_hash: "x".into(),
            is_active: true,
            organization_id: None,
            attributes: json!({
                "scim": {
                    "externalId": "00u1",
                    "name": { "givenName": "Alice", "familyName": "Example" }
                }
            }),
            mfa_enforced: false,
            auth_source: "scim".into(),
            created_at: DateTime::<Utc>::UNIX_EPOCH,
            updated_at: DateTime::<Utc>::UNIX_EPOCH,
        };
        let scim = user_to_scim(user);
        assert_eq!(scim.schemas, vec![SCHEMA_USER.to_string()]);
        assert_eq!(scim.id, Some(id.to_string()));
        assert_eq!(scim.external_id, Some("00u1".into()));
        assert_eq!(scim.name.unwrap().given_name, Some("Alice".into()));
    }

    #[test]
    fn scim_user_attributes_store_external_id_and_tenancy_extension() {
        let mut extensions = Map::new();
        extensions.insert(
            SCHEMA_OPENFOUNDRY_USER_EXTENSION.into(),
            json!({ "organizationSlug": "acme" }),
        );
        let user = ScimUser {
            schemas: vec![SCHEMA_USER.into()],
            id: None,
            user_name: "alice@example.com".into(),
            name: None,
            emails: None,
            active: Some(true),
            external_id: Some("00u1".into()),
            meta: None,
            extensions,
        };
        let attributes = user_attributes_from_scim(&user);
        assert_eq!(
            external_id_from_attributes(&attributes).as_deref(),
            Some("00u1")
        );
        assert_eq!(
            attributes.pointer("/scim/openfoundry/organizationSlug"),
            Some(&json!("acme"))
        );
    }

    #[test]
    fn group_transform_preserves_external_id_and_membership() {
        let group = ScimGroup {
            schemas: vec![SCHEMA_GROUP.into()],
            id: Some(Uuid::now_v7().to_string()),
            display_name: "Analysts".into(),
            members: Some(vec![ScimGroupMember {
                value: Uuid::now_v7().to_string(),
                ref_: None,
                type_: Some("User".into()),
                display: Some("Alice".into()),
            }]),
            external_id: Some("grp-1".into()),
            meta: None,
            extensions: Map::new(),
        };
        assert_eq!(group.external_id.as_deref(), Some("grp-1"));
        assert_eq!(group.members.as_ref().unwrap().len(), 1);
    }

    #[test]
    fn create_with_external_id_is_idempotent_over_existing_user_record() {
        let id = Uuid::now_v7();
        let mut existing = User {
            id,
            email: "old@example.com".into(),
            name: "Old Name".into(),
            password_hash: "x".into(),
            is_active: false,
            organization_id: None,
            attributes: json!({ "scim": { "externalId": "00u1" } }),
            mfa_enforced: false,
            auth_source: "scim".into(),
            created_at: DateTime::<Utc>::UNIX_EPOCH,
            updated_at: DateTime::<Utc>::UNIX_EPOCH,
        };
        merge_scim_user_record(
            &mut existing,
            "alice@example.com".into(),
            "Alice Example".into(),
            true,
            Some(Uuid::nil()),
            json!({ "scim": { "externalId": "00u1" } }),
        );
        assert_eq!(existing.id, id);
        assert_eq!(existing.email, "alice@example.com");
        assert!(existing.is_active);
        assert_eq!(existing.organization_id, Some(Uuid::nil()));
    }

    #[test]
    fn patch_user_updates_core_fields_and_external_id() {
        let mut user = User {
            id: Uuid::now_v7(),
            email: "old@example.com".into(),
            name: "Old Name".into(),
            password_hash: "x".into(),
            is_active: true,
            organization_id: None,
            attributes: json!({}),
            mfa_enforced: false,
            auth_source: "scim".into(),
            created_at: DateTime::<Utc>::UNIX_EPOCH,
            updated_at: DateTime::<Utc>::UNIX_EPOCH,
        };
        apply_user_patch(
            &mut user,
            ScimPatchOperation {
                op: "replace".into(),
                path: None,
                value: Some(json!({
                    "userName": "new@example.com",
                    "active": false,
                    "externalId": "00u2",
                    "name": { "formatted": "New Name" }
                })),
            },
        )
        .unwrap();
        assert_eq!(user.email, "new@example.com");
        assert_eq!(user.name, "New Name");
        assert!(!user.is_active);
        assert_eq!(
            external_id_from_attributes(&user.attributes).as_deref(),
            Some("00u2")
        );
    }

    #[test]
    fn deactivate_marks_user_inactive() {
        let mut user = User {
            id: Uuid::now_v7(),
            email: "alice@example.com".into(),
            name: "Alice".into(),
            password_hash: "x".into(),
            is_active: true,
            organization_id: None,
            attributes: json!({}),
            mfa_enforced: false,
            auth_source: "scim".into(),
            created_at: DateTime::<Utc>::UNIX_EPOCH,
            updated_at: DateTime::<Utc>::UNIX_EPOCH,
        };
        deactivate_scim_user_record(&mut user);
        assert!(!user.is_active);
    }

    #[test]
    fn group_membership_accepts_single_or_array_value() {
        let user_id = Uuid::now_v7();
        let one = members_from_value(Some(json!({ "value": user_id.to_string() }))).unwrap();
        assert_eq!(one.len(), 1);
        let many = members_from_value(Some(json!([
            { "value": user_id.to_string() },
            { "value": Uuid::now_v7().to_string(), "type": "User" }
        ])))
        .unwrap();
        assert_eq!(many.len(), 2);
    }
}
