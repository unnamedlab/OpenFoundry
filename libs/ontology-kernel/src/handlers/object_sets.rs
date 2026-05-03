use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use chrono::{TimeZone, Utc};
use serde::Deserialize;
use serde_json::{Value, json};
use storage_abstraction::repositories::{
    ObjectSetId, ObjectSetMaterialization, ObjectSetMaterializationMetadata,
    ObjectSetMaterializedRow, Page, ReadConsistency, TenantId, TypeId,
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{object_set_repository, object_sets, read_models::tenant_from_claims},
    models::object_set::{
        CreateObjectSetRequest, EvaluateObjectSetRequest, ListObjectSetsResponse,
        ObjectSetDefinition, ObjectSetEvaluationResponse, UpdateObjectSetRequest,
    },
};

fn bad_request(message: impl Into<String>) -> Response {
    (
        StatusCode::BAD_REQUEST,
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

fn internal_error(message: impl Into<String>) -> Response {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

#[derive(Debug, Deserialize, Default)]
pub struct ListObjectSetsQuery {
    #[serde(default = "default_page_size")]
    pub size: u32,
    #[serde(default)]
    pub token: Option<String>,
}

fn default_page_size() -> u32 {
    100
}

fn build_definition_from_create(
    owner_id: Uuid,
    request: CreateObjectSetRequest,
) -> ObjectSetDefinition {
    ObjectSetDefinition {
        id: Uuid::now_v7(),
        name: request.name,
        description: request.description,
        base_object_type_id: request.base_object_type_id,
        filters: request.filters,
        traversals: request.traversals,
        join: request.join,
        projections: request.projections,
        what_if_label: request.what_if_label,
        policy: request.policy,
        materialized_snapshot: None,
        materialized_at: None,
        materialized_row_count: 0,
        owner_id,
        created_at: Utc::now(),
        updated_at: Utc::now(),
    }
}

async fn load_object_set(
    state: &AppState,
    tenant: &TenantId,
    id: Uuid,
) -> Result<Option<ObjectSetDefinition>, String> {
    let Some(definition) = object_set_repository::get(state.stores.definitions.as_ref(), id)
        .await
        .map_err(|error| format!("failed to load object set: {error}"))?
    else {
        return Ok(None);
    };
    Ok(Some(
        enrich_materialization_metadata(state, tenant, definition).await,
    ))
}

async fn object_type_exists(state: &AppState, object_type_id: Uuid) -> Result<bool, String> {
    object_set_repository::object_type_exists(state.stores.definitions.as_ref(), object_type_id)
        .await
        .map_err(|error| format!("failed to validate object type: {error}"))
}

fn object_set_id(id: Uuid) -> ObjectSetId {
    ObjectSetId(id.to_string())
}

fn row_id_from_value(row: &Value, ordinal: usize) -> String {
    row.get("base")
        .and_then(|base| base.get("id"))
        .and_then(Value::as_str)
        .or_else(|| row.get("id").and_then(Value::as_str))
        .map(ToOwned::to_owned)
        .unwrap_or_else(|| format!("row-{ordinal}"))
}

fn materialization_from_evaluation(
    tenant: TenantId,
    definition: &ObjectSetDefinition,
    evaluation: &ObjectSetEvaluationResponse,
) -> ObjectSetMaterialization {
    let rows = evaluation
        .rows
        .iter()
        .enumerate()
        .map(|(index, row)| ObjectSetMaterializedRow {
            row_id: row_id_from_value(row, index),
            ordinal: index as u32,
            payload: row.clone(),
        })
        .collect();

    ObjectSetMaterialization {
        tenant,
        set_id: object_set_id(definition.id),
        base_type_id: TypeId(definition.base_object_type_id.to_string()),
        generated_at_ms: evaluation.generated_at.timestamp_millis(),
        total_base_matches: evaluation.total_base_matches as u64,
        total_rows: evaluation.total_rows as u64,
        traversal_neighbor_count: evaluation.traversal_neighbor_count as u64,
        rows,
    }
}

fn apply_materialization_metadata(
    definition: &mut ObjectSetDefinition,
    metadata: &ObjectSetMaterializationMetadata,
) {
    definition.materialized_at = Utc.timestamp_millis_opt(metadata.generated_at_ms).single();
    definition.materialized_row_count = i32::try_from(metadata.total_rows).unwrap_or(i32::MAX);
}

async fn enrich_materialization_metadata(
    state: &AppState,
    tenant: &TenantId,
    mut definition: ObjectSetDefinition,
) -> ObjectSetDefinition {
    match state
        .stores
        .object_set_materializations
        .get_metadata(
            tenant,
            &object_set_id(definition.id),
            ReadConsistency::Eventual,
        )
        .await
    {
        Ok(Some(metadata))
            if metadata.generated_at_ms >= definition.updated_at.timestamp_millis() =>
        {
            apply_materialization_metadata(&mut definition, &metadata);
        }
        Ok(_) => {}
        Err(error) => {
            tracing::warn!(
                object_set_id = %definition.id,
                %error,
                "failed to load object set materialization metadata"
            );
        }
    }
    definition
}

pub async fn list_object_sets(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListObjectSetsQuery>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    let page = match object_set_repository::list(
        state.stores.definitions.as_ref(),
        object_set_repository::ObjectSetListQuery {
            owner_id: claims.sub,
            include_restricted_views: true,
            page: Page {
                size: query.size.clamp(1, 500),
                token: query.token,
            },
        },
    )
    .await
    {
        Ok(page) => page,
        Err(error) => {
            tracing::error!("list object sets failed: {error}");
            return internal_error("failed to load object sets");
        }
    };

    let mut data = Vec::with_capacity(page.items.len());
    for definition in page.items {
        data.push(enrich_materialization_metadata(&state, &tenant, definition).await);
    }

    Json(ListObjectSetsResponse {
        data,
        next_token: page.next_token,
    })
    .into_response()
}

pub async fn create_object_set(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(request): Json<CreateObjectSetRequest>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    let definition = build_definition_from_create(claims.sub, request);
    if let Err(error) = object_sets::validate_object_set_definition(&definition) {
        return bad_request(error);
    }
    match object_type_exists(&state, definition.base_object_type_id).await {
        Ok(true) => {}
        Ok(false) => return bad_request("base_object_type_id does not exist"),
        Err(error) => return internal_error(error),
    }
    if let Some(join) = definition.join.as_ref() {
        match object_type_exists(&state, join.secondary_object_type_id).await {
            Ok(true) => {}
            Ok(false) => return bad_request("join.secondary_object_type_id does not exist"),
            Err(error) => return internal_error(error),
        }
    }

    let definition_id = definition.id;
    if let Err(error) =
        object_set_repository::create(state.stores.definitions.as_ref(), definition).await
    {
        tracing::error!("create object set failed: {error}");
        return internal_error("failed to create object set");
    }

    match load_object_set(&state, &tenant, definition_id).await {
        Ok(Some(object_set)) => (StatusCode::CREATED, Json(object_set)).into_response(),
        Ok(None) => internal_error("created object set could not be reloaded"),
        Err(error) => internal_error(error),
    }
}

pub async fn get_object_set(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    match load_object_set(&state, &tenant, id).await {
        Ok(Some(object_set)) => Json(object_set).into_response(),
        Ok(None) => not_found("object set not found"),
        Err(error) => internal_error(error),
    }
}

pub async fn update_object_set(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(request): Json<UpdateObjectSetRequest>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    let Some(existing) = (match load_object_set(&state, &tenant, id).await {
        Ok(object_set) => object_set,
        Err(error) => return internal_error(error),
    }) else {
        return not_found("object set not found");
    };

    if existing.owner_id != claims.sub && !claims.has_role("admin") {
        return (
            StatusCode::FORBIDDEN,
            Json(json!({ "error": "forbidden: only the owner can update this object set" })),
        )
            .into_response();
    }

    let next = ObjectSetDefinition {
        id: existing.id,
        name: request.name.unwrap_or(existing.name),
        description: request.description.unwrap_or(existing.description),
        base_object_type_id: request
            .base_object_type_id
            .unwrap_or(existing.base_object_type_id),
        filters: request.filters.unwrap_or(existing.filters),
        traversals: request.traversals.unwrap_or(existing.traversals),
        join: request.join.or(existing.join),
        projections: request.projections.unwrap_or(existing.projections),
        what_if_label: request.what_if_label.or(existing.what_if_label),
        policy: request.policy.unwrap_or(existing.policy),
        materialized_snapshot: None,
        materialized_at: None,
        materialized_row_count: 0,
        owner_id: existing.owner_id,
        created_at: existing.created_at,
        updated_at: Utc::now(),
    };

    if let Err(error) = object_sets::validate_object_set_definition(&next) {
        return bad_request(error);
    }

    if let Err(error) = object_set_repository::update(state.stores.definitions.as_ref(), next).await
    {
        tracing::error!("update object set failed: {error}");
        return internal_error("failed to update object set");
    }

    if let Err(error) = state
        .stores
        .object_set_materializations
        .delete(&tenant, &object_set_id(id))
        .await
    {
        tracing::warn!(object_set_id = %id, %error, "failed to invalidate object set materialization");
    }

    match load_object_set(&state, &tenant, id).await {
        Ok(Some(object_set)) => Json(object_set).into_response(),
        Ok(None) => internal_error("updated object set could not be reloaded"),
        Err(error) => internal_error(error),
    }
}

pub async fn delete_object_set(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    let Some(existing) = (match load_object_set(&state, &tenant, id).await {
        Ok(object_set) => object_set,
        Err(error) => return internal_error(error),
    }) else {
        return not_found("object set not found");
    };

    if existing.owner_id != claims.sub && !claims.has_role("admin") {
        return (
            StatusCode::FORBIDDEN,
            Json(json!({ "error": "forbidden: only the owner can delete this object set" })),
        )
            .into_response();
    }

    match object_set_repository::delete(state.stores.definitions.as_ref(), id).await {
        Ok(true) => {
            if let Err(error) = state
                .stores
                .object_set_materializations
                .delete(&tenant, &object_set_id(id))
                .await
            {
                tracing::warn!(object_set_id = %id, %error, "failed to delete object set materialization");
            }
            StatusCode::NO_CONTENT.into_response()
        }
        Ok(false) => not_found("object set not found"),
        Err(error) => {
            tracing::error!("delete object set failed: {error}");
            internal_error("failed to delete object set")
        }
    }
}

pub async fn evaluate_object_set(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(request): Json<EvaluateObjectSetRequest>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    let Some(definition) = (match load_object_set(&state, &tenant, id).await {
        Ok(object_set) => object_set,
        Err(error) => return internal_error(error),
    }) else {
        return not_found("object set not found");
    };

    let limit = request.limit.unwrap_or(250).clamp(1, 2_000);
    match object_sets::evaluate_object_set(&state, &claims, &definition, limit, false).await {
        Ok(evaluation) => Json(evaluation).into_response(),
        Err(error) if error.contains("forbidden") => {
            (StatusCode::FORBIDDEN, Json(json!({ "error": error }))).into_response()
        }
        Err(error) => bad_request(error),
    }
}

pub async fn materialize_object_set(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(request): Json<EvaluateObjectSetRequest>,
) -> impl IntoResponse {
    let tenant = tenant_from_claims(&claims);
    let Some(definition) = (match load_object_set(&state, &tenant, id).await {
        Ok(object_set) => object_set,
        Err(error) => return internal_error(error),
    }) else {
        return not_found("object set not found");
    };

    let limit = request.limit.unwrap_or(2_000).clamp(1, 5_000);
    let mut evaluation =
        match object_sets::evaluate_object_set(&state, &claims, &definition, limit, true).await {
            Ok(evaluation) => evaluation,
            Err(error) if error.contains("forbidden") => {
                return (StatusCode::FORBIDDEN, Json(json!({ "error": error }))).into_response();
            }
            Err(error) => return bad_request(error),
        };

    let materialization = materialization_from_evaluation(tenant, &definition, &evaluation);
    let metadata = match state
        .stores
        .object_set_materializations
        .replace(materialization)
        .await
    {
        Ok(metadata) => metadata,
        Err(error) => {
            tracing::error!(object_set_id = %id, %error, "materialize object set failed");
            return internal_error("failed to materialize object set");
        }
    };
    apply_materialization_metadata(&mut evaluation.object_set, &metadata);

    Json(evaluation).into_response()
}

#[cfg(test)]
mod tests {
    use super::*;
    use auth_middleware::claims::Claims;
    use http_body_util::BodyExt;
    use serde_json::Value;
    use std::sync::Arc;
    use storage_abstraction::repositories::{
        DefinitionId, DefinitionKind, DefinitionRecord, DefinitionStore, IndexDoc, PutOutcome,
        TypeId, noop::InMemoryDefinitionStore,
    };

    fn claims() -> Claims {
        let now = Utc::now().timestamp();
        Claims {
            sub: Uuid::now_v7(),
            iat: now,
            exp: now + 3600,
            iss: None,
            aud: None,
            jti: Uuid::now_v7(),
            email: "object-sets-test@openfoundry.dev".into(),
            name: "Object Sets Test".into(),
            roles: vec!["admin".into()],
            permissions: vec!["*:*".into()],
            org_id: Some(Uuid::now_v7()),
            attributes: json!({ "classification_clearance": "public" }),
            auth_methods: vec!["password".into()],
            token_use: Some("access".into()),
            api_key_id: None,
            session_kind: None,
            session_scope: None,
        }
    }

    fn state_with_definitions(definitions: Arc<dyn DefinitionStore>) -> AppState {
        AppState {
            db: crate::test_support::lazy_pg_pool(),
            stores: crate::stores::Stores {
                definitions,
                ..crate::stores::Stores::in_memory()
            },
            http_client: reqwest::Client::new(),
            jwt_config: auth_middleware::jwt::JwtConfig::new("test-secret"),
            audit_service_url: String::new(),
            dataset_service_url: String::new(),
            ontology_service_url: String::new(),
            pipeline_service_url: String::new(),
            ai_service_url: String::new(),
            notification_service_url: String::new(),
            search_embedding_provider: "deterministic".into(),
            node_runtime_command: "node".into(),
            connector_management_service_url: String::new(),
        }
    }

    async fn seed_object_type(store: &InMemoryDefinitionStore, type_id: Uuid) {
        let now = Utc::now();
        let outcome = store
            .put(
                DefinitionRecord {
                    kind: DefinitionKind(object_set_repository::OBJECT_TYPE_KIND.to_string()),
                    id: DefinitionId(type_id.to_string()),
                    tenant: None,
                    owner_id: None,
                    parent_id: None,
                    version: Some(1),
                    payload: json!({
                        "id": type_id,
                        "name": "aircraft",
                        "display_name": "Aircraft",
                        "created_at": now,
                        "updated_at": now,
                    }),
                    created_at_ms: Some(now.timestamp_millis()),
                    updated_at_ms: Some(now.timestamp_millis()),
                },
                None,
            )
            .await
            .expect("seed object type");
        assert!(matches!(outcome, PutOutcome::Inserted));
    }

    async fn response_json(response: Response) -> Value {
        let body = response
            .into_body()
            .collect()
            .await
            .expect("body")
            .to_bytes();
        serde_json::from_slice(&body).expect("json response")
    }

    fn create_request(type_id: Uuid, name: &str, status: &str) -> CreateObjectSetRequest {
        CreateObjectSetRequest {
            name: name.to_string(),
            description: "saved set".to_string(),
            base_object_type_id: type_id,
            filters: vec![crate::models::object_set::ObjectSetFilter {
                field: "properties.status".to_string(),
                operator: "equals".to_string(),
                value: json!(status),
            }],
            traversals: Vec::new(),
            join: None,
            projections: Vec::new(),
            what_if_label: None,
            policy: Default::default(),
        }
    }

    async fn create_set(
        state: AppState,
        claims: Claims,
        request: CreateObjectSetRequest,
    ) -> (StatusCode, Value) {
        let response = create_object_set(AuthUser(claims), State(state), Json(request))
            .await
            .into_response();
        let status = response.status();
        (status, response_json(response).await)
    }

    #[tokio::test]
    async fn create_list_paginate_evaluate_and_delete_object_set() {
        let definitions = Arc::new(InMemoryDefinitionStore::default());
        let type_id = Uuid::now_v7();
        seed_object_type(definitions.as_ref(), type_id).await;

        let state = state_with_definitions(definitions.clone());
        let claims = claims();
        let tenant = tenant_from_claims(&claims);

        let (first_status, first_json) = create_set(
            state.clone(),
            claims.clone(),
            create_request(type_id, "active", "active"),
        )
        .await;
        assert_eq!(first_status, StatusCode::CREATED);
        let first_id = Uuid::parse_str(first_json["id"].as_str().expect("id")).expect("uuid");

        let (second_status, _) = create_set(
            state.clone(),
            claims.clone(),
            create_request(type_id, "idle", "idle"),
        )
        .await;
        assert_eq!(second_status, StatusCode::CREATED);

        let first_page = list_object_sets(
            AuthUser(claims.clone()),
            State(state.clone()),
            Query(ListObjectSetsQuery {
                size: 1,
                token: None,
            }),
        )
        .await
        .into_response();
        assert_eq!(first_page.status(), StatusCode::OK);
        let first_page = response_json(first_page).await;
        assert_eq!(first_page["data"].as_array().map(Vec::len), Some(1));
        let next_token = first_page["next_token"]
            .as_str()
            .expect("pagination token")
            .to_string();

        let second_page = list_object_sets(
            AuthUser(claims.clone()),
            State(state.clone()),
            Query(ListObjectSetsQuery {
                size: 1,
                token: Some(next_token),
            }),
        )
        .await
        .into_response();
        assert_eq!(second_page.status(), StatusCode::OK);
        let second_page = response_json(second_page).await;
        assert_eq!(second_page["data"].as_array().map(Vec::len), Some(1));

        state
            .stores
            .search
            .index(IndexDoc {
                tenant: tenant.clone(),
                id: storage_abstraction::repositories::ObjectId(Uuid::now_v7().to_string()),
                type_id: TypeId(type_id.to_string()),
                payload: json!({
                    "id": Uuid::now_v7(),
                    "object_type_id": type_id,
                    "properties": { "status": "active" },
                    "created_by": claims.sub,
                    "organization_id": claims.org_id,
                    "marking": "public",
                    "created_at": Utc::now(),
                    "updated_at": Utc::now(),
                }),
                version: 1,
                embedding: None,
            })
            .await
            .expect("index active object");
        state
            .stores
            .search
            .index(IndexDoc {
                tenant,
                id: storage_abstraction::repositories::ObjectId(Uuid::now_v7().to_string()),
                type_id: TypeId(type_id.to_string()),
                payload: json!({
                    "id": Uuid::now_v7(),
                    "object_type_id": type_id,
                    "properties": { "status": "idle" },
                    "created_by": claims.sub,
                    "organization_id": claims.org_id,
                    "marking": "public",
                    "created_at": Utc::now(),
                    "updated_at": Utc::now(),
                }),
                version: 1,
                embedding: None,
            })
            .await
            .expect("index idle object");

        let evaluation = evaluate_object_set(
            AuthUser(claims.clone()),
            State(state.clone()),
            Path(first_id),
            Json(EvaluateObjectSetRequest { limit: Some(10) }),
        )
        .await
        .into_response();
        assert_eq!(evaluation.status(), StatusCode::OK);
        let evaluation = response_json(evaluation).await;
        assert_eq!(evaluation["total_base_matches"], 1);
        assert_eq!(evaluation["rows"].as_array().map(Vec::len), Some(1));

        let deleted = delete_object_set(
            AuthUser(claims.clone()),
            State(state.clone()),
            Path(first_id),
        )
        .await
        .into_response();
        assert_eq!(deleted.status(), StatusCode::NO_CONTENT);

        let get_deleted = get_object_set(AuthUser(claims), State(state), Path(first_id))
            .await
            .into_response();
        assert_eq!(get_deleted.status(), StatusCode::NOT_FOUND);
    }
}
