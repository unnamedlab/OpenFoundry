//! Ontology link CRUD handlers.
//!
//! ## Migration to `storage-abstraction` traits (S1.2 → S1.4)
//!
//! Pure business logic for link composition lives in
//! [`crate::domain::composition`] and operates on
//! `&dyn storage_abstraction::repositories::LinkStore`. New services that
//! target Cassandra MUST route link mutations through
//! `composition::create_link` / `composition::delete_link`, constructing the
//! store from `AppState::stores.links`.
//!
//! Link type metadata is declarative and stays in PostgreSQL during S1, but is
//! accessed through `domain::link_type_repository` rather than inline SQL.
//! Link instances are always routed through [`AppState::stores.links`].

use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use serde_json::json;
use storage_abstraction::repositories::{
    Link as StoredLink, LinkTypeId, ObjectId, Page, ReadConsistency, TenantId, TypeId,
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        composition, link_type_repository, read_models::tenant_from_claims,
        type_system::validate_cardinality,
    },
    models::link_type::*,
};
use auth_middleware::layer::AuthUser;

// --- Link Type CRUD ---

pub async fn create_link_type(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreateLinkTypeRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let display_name = body
        .display_name
        .clone()
        .unwrap_or_else(|| body.name.clone());
    let description = body.description.clone().unwrap_or_default();
    let cardinality = body
        .cardinality
        .clone()
        .unwrap_or_else(|| "many_to_many".to_string());
    if let Err(error) = validate_cardinality(&cardinality) {
        return (StatusCode::BAD_REQUEST, error).into_response();
    }

    let result = link_type_repository::create(
        &state.db,
        id,
        claims.sub,
        &body,
        &display_name,
        &description,
        &cardinality,
    )
    .await;

    match result {
        Ok(lt) => (StatusCode::CREATED, Json(serde_json::json!(lt))).into_response(),
        Err(e) => {
            tracing::error!("create link type: {e}");
            (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response()
        }
    }
}

pub async fn list_link_types(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<ListLinkTypesQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;

    let (types, total) =
        match link_type_repository::list(&state.db, &params, per_page, offset).await {
            Ok(result) => result,
            Err(error) => {
                tracing::error!("list link types: {error}");
                (Vec::new(), 0)
            }
        };

    Json(serde_json::json!({ "data": types, "total": total, "page": page, "per_page": per_page }))
}

pub async fn delete_link_type(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match link_type_repository::delete(&state.db, id).await {
        Ok(true) => StatusCode::NO_CONTENT.into_response(),
        Ok(false) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn update_link_type(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateLinkTypeRequest>,
) -> impl IntoResponse {
    let existing = match link_type_repository::load(&state.db, id).await {
        Ok(Some(link_type)) => link_type,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(e) => return (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    };

    let cardinality = body
        .cardinality
        .clone()
        .unwrap_or_else(|| existing.cardinality.clone());
    if let Err(error) = validate_cardinality(&cardinality) {
        return (StatusCode::BAD_REQUEST, error).into_response();
    }

    match link_type_repository::update(&state.db, id, body, cardinality).await {
        Ok(Some(link_type)) => Json(serde_json::json!(link_type)).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

// --- Link Instance CRUD ---

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LinkInstance {
    pub id: Uuid,
    pub link_type_id: Uuid,
    pub source_object_id: Uuid,
    pub target_object_id: Uuid,
    pub properties: Option<serde_json::Value>,
    pub created_by: Uuid,
    pub created_at: chrono::DateTime<chrono::Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateLinkRequest {
    pub source_object_id: Uuid,
    pub target_object_id: Uuid,
    pub properties: Option<serde_json::Value>,
}

fn link_instance_from_store_link(link: StoredLink) -> Result<LinkInstance, String> {
    let link_type = LinkTypeId(link.link_type.0.clone());
    let from = ObjectId(link.from.0.clone());
    let to = ObjectId(link.to.0.clone());
    let link_type_id = Uuid::parse_str(&link_type.0).map_err(|error| {
        format!(
            "invalid link type id '{}' in LinkStore: {error}",
            link_type.0
        )
    })?;
    let source_object_id = Uuid::parse_str(&from.0).map_err(|error| {
        format!(
            "invalid source object id '{}' in LinkStore: {error}",
            from.0
        )
    })?;
    let target_object_id = Uuid::parse_str(&to.0)
        .map_err(|error| format!("invalid target object id '{}' in LinkStore: {error}", to.0))?;

    Ok(LinkInstance {
        id: composition::stable_link_id(&link_type, &from, &to),
        link_type_id,
        source_object_id,
        target_object_id,
        properties: link.payload,
        created_by: Uuid::nil(),
        created_at: chrono::DateTime::<chrono::Utc>::from_timestamp_millis(link.created_at_ms)
            .unwrap_or_else(Utc::now),
    })
}

async fn load_link_type(state: &AppState, link_type_id: Uuid) -> Result<Option<LinkType>, String> {
    link_type_repository::load(&state.db, link_type_id)
        .await
        .map_err(|error| format!("failed to load link type metadata: {error}"))
}

pub(crate) async fn collect_link_instances_for_type(
    state: &AppState,
    tenant: &TenantId,
    link_type: &LinkType,
) -> Result<Vec<LinkInstance>, String> {
    let mut instances = Vec::new();
    let mut object_page = Page {
        size: 256,
        token: None,
    };
    let source_type = TypeId(link_type.source_type_id.to_string());
    let link_type_id = LinkTypeId(link_type.id.to_string());

    loop {
        let objects = state
            .stores
            .objects
            .list_by_type(
                tenant,
                &source_type,
                object_page.clone(),
                ReadConsistency::Eventual,
            )
            .await
            .map_err(|error| format!("failed to enumerate source objects for links: {error}"))?;

        for object in objects.items {
            let mut link_page = Page {
                size: 256,
                token: None,
            };
            loop {
                let links = state
                    .stores
                    .links
                    .list_outgoing(
                        tenant,
                        &link_type_id,
                        &object.id,
                        link_page.clone(),
                        ReadConsistency::Eventual,
                    )
                    .await
                    .map_err(|error| format!("failed to enumerate link instances: {error}"))?;

                for link in links.items {
                    instances.push(link_instance_from_store_link(link)?);
                }

                let Some(next_token) = links.next_token else {
                    break;
                };
                link_page.token = Some(next_token);
            }
        }

        let Some(next_token) = objects.next_token else {
            break;
        };
        object_page.token = Some(next_token);
    }

    instances.sort_by(|left, right| {
        right
            .created_at
            .cmp(&left.created_at)
            .then_with(|| left.id.cmp(&right.id))
    });
    Ok(instances)
}

pub async fn create_link(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(link_type_id): Path<Uuid>,
    Json(body): Json<CreateLinkRequest>,
) -> impl IntoResponse {
    let created_at = Utc::now();
    let tenant = tenant_from_claims(&claims);
    let payload = body.properties.clone().unwrap_or_else(|| json!({}));
    let link_type = LinkTypeId(link_type_id.to_string());
    let from = ObjectId(body.source_object_id.to_string());
    let to = ObjectId(body.target_object_id.to_string());

    match composition::create_link(
        state.stores.links.as_ref(),
        tenant,
        link_type.clone(),
        from.clone(),
        to.clone(),
        payload,
        created_at.timestamp_millis(),
    )
    .await
    {
        Ok(_) => {
            let link = LinkInstance {
                id: composition::stable_link_id(&link_type, &from, &to),
                link_type_id,
                source_object_id: body.source_object_id,
                target_object_id: body.target_object_id,
                properties: body.properties,
                created_by: claims.sub,
                created_at,
            };
            (StatusCode::CREATED, Json(serde_json::json!(link))).into_response()
        }
        Err(error) => {
            tracing::error!("create link via LinkStore failed: {error}");
            (StatusCode::INTERNAL_SERVER_ERROR, error.to_string()).into_response()
        }
    }
}

pub async fn list_links(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(link_type_id): Path<Uuid>,
    Query(params): Query<ListLinkTypesQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let link_type = match load_link_type(&state, link_type_id).await {
        Ok(Some(link_type)) => link_type,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => return (StatusCode::INTERNAL_SERVER_ERROR, error).into_response(),
    };
    let tenant = tenant_from_claims(&claims);
    let links = match collect_link_instances_for_type(&state, &tenant, &link_type).await {
        Ok(links) => links,
        Err(error) => return (StatusCode::INTERNAL_SERVER_ERROR, error).into_response(),
    };

    let start = offset as usize;
    let end = start.saturating_add(per_page as usize).min(links.len());
    let data = if start >= links.len() {
        Vec::new()
    } else {
        links[start..end].to_vec()
    };

    Json(serde_json::json!({ "data": data })).into_response()
}

pub async fn delete_link(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((link_type_id, link_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    let link_type = match load_link_type(&state, link_type_id).await {
        Ok(Some(link_type)) => link_type,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => return (StatusCode::INTERNAL_SERVER_ERROR, error).into_response(),
    };
    let tenant = tenant_from_claims(&claims);
    let link = match collect_link_instances_for_type(&state, &tenant, &link_type).await {
        Ok(links) => links.into_iter().find(|candidate| candidate.id == link_id),
        Err(error) => return (StatusCode::INTERNAL_SERVER_ERROR, error).into_response(),
    };
    let Some(link) = link else {
        return StatusCode::NOT_FOUND.into_response();
    };

    match composition::delete_link(
        state.stores.links.as_ref(),
        tenant,
        LinkTypeId(link.link_type_id.to_string()),
        ObjectId(link.source_object_id.to_string()),
        ObjectId(link.target_object_id.to_string()),
    )
    .await
    {
        Ok(true) => StatusCode::NO_CONTENT.into_response(),
        Ok(false) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("delete link via LinkStore failed: {error}");
            (StatusCode::INTERNAL_SERVER_ERROR, error.to_string()).into_response()
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use auth_middleware::claims::Claims;
    use axum::extract::State;
    use serde_json::json;
    use storage_abstraction::repositories::noop::InMemoryLinkStore;

    #[test]
    fn store_links_map_to_stable_link_instances() {
        let link_type = Uuid::now_v7();
        let source = Uuid::now_v7();
        let target = Uuid::now_v7();
        let stored = StoredLink {
            tenant: TenantId("tenant-a".to_string()),
            link_type: LinkTypeId(link_type.to_string()),
            from: ObjectId(source.to_string()),
            to: ObjectId(target.to_string()),
            payload: Some(json!({ "weight": 3 })),
            created_at_ms: 1_700_000_000_000,
        };

        let mapped = link_instance_from_store_link(stored).expect("link maps cleanly");

        assert_eq!(
            mapped.id,
            composition::stable_link_id(
                &LinkTypeId(link_type.to_string()),
                &ObjectId(source.to_string()),
                &ObjectId(target.to_string()),
            )
        );
        assert_eq!(mapped.link_type_id, link_type);
        assert_eq!(mapped.source_object_id, source);
        assert_eq!(mapped.target_object_id, target);
        assert_eq!(mapped.properties, Some(json!({ "weight": 3 })));
    }

    fn test_claims() -> Claims {
        let now = Utc::now().timestamp();
        Claims {
            sub: Uuid::now_v7(),
            iat: now,
            exp: now + 3600,
            iss: None,
            aud: None,
            jti: Uuid::now_v7(),
            email: "links-test@openfoundry.dev".into(),
            name: "Links Test".into(),
            roles: vec!["admin".into()],
            permissions: vec!["*:*".into()],
            org_id: Some(Uuid::now_v7()),
            attributes: json!({}),
            auth_methods: vec!["password".into()],
            token_use: Some("access".into()),
            api_key_id: None,
            session_kind: None,
            session_scope: None,
        }
    }

    fn test_state(link_store: InMemoryLinkStore) -> AppState {
        AppState {
            db: crate::test_support::lazy_pg_pool(),
            stores: crate::stores::Stores {
                links: std::sync::Arc::new(link_store),
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

    #[tokio::test]
    async fn create_link_handler_uses_link_store() {
        let store = InMemoryLinkStore::default();
        let link_type_id = Uuid::now_v7();
        let source = Uuid::now_v7();
        let target = Uuid::now_v7();
        let state = test_state(store);
        let claims = test_claims();
        let tenant = tenant_from_claims(&claims);

        let response = create_link(
            AuthUser(claims),
            State(state.clone()),
            Path(link_type_id),
            Json(CreateLinkRequest {
                source_object_id: source,
                target_object_id: target,
                properties: Some(json!({ "weight": 1 })),
            }),
        )
        .await
        .into_response();
        assert_eq!(response.status(), StatusCode::CREATED);

        let page = state
            .stores
            .links
            .list_outgoing(
                &tenant,
                &LinkTypeId(link_type_id.to_string()),
                &ObjectId(source.to_string()),
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Strong,
            )
            .await
            .expect("list links");
        assert_eq!(page.items.len(), 1);
        assert_eq!(page.items[0].to, ObjectId(target.to_string()));
    }
}

// --- Multi-hop traversal ---------------------------------------------------

#[derive(Debug, Deserialize)]
pub struct TraverseQuery {
    /// Maximum number of hops to walk. Clamped to `[1, 5]`.
    pub depth: Option<i32>,
    /// Comma-separated list of `link_type_id` UUIDs to whitelist.
    pub link_types: Option<String>,
    /// Comma-separated list of allowed markings (e.g. `public,confidential`).
    pub markings: Option<String>,
    /// Hard cap on returned edges. Clamped to `[1, 5000]`.
    pub limit: Option<i32>,
}

fn parse_csv_uuid_list(value: Option<&str>) -> Result<Option<Vec<Uuid>>, String> {
    let Some(raw) = value.map(str::trim).filter(|v| !v.is_empty()) else {
        return Ok(None);
    };
    let mut out = Vec::new();
    for token in raw.split(',') {
        let token = token.trim();
        if token.is_empty() {
            continue;
        }
        let id =
            Uuid::parse_str(token).map_err(|error| format!("invalid uuid '{token}': {error}"))?;
        out.push(id);
    }
    Ok(Some(out))
}

fn parse_csv_string_list(value: Option<&str>) -> Option<Vec<String>> {
    let raw = value?.trim();
    if raw.is_empty() {
        return None;
    }
    let parts: Vec<String> = raw
        .split(',')
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .map(|s| s.to_string())
        .collect();
    if parts.is_empty() { None } else { Some(parts) }
}

/// `GET /ontology/objects/:id/traverse?depth=&link_types=&markings=&limit=`
///
/// Returns every edge reachable from `:id` within `depth` hops, filtered by
/// `link_types` (CSV of UUIDs) and `markings` (CSV of marking strings,
/// defaults to the caller's clearance).
pub async fn traverse_neighbors(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(starting_object_id): Path<Uuid>,
    Query(params): Query<TraverseQuery>,
) -> impl IntoResponse {
    let link_type_ids = match parse_csv_uuid_list(params.link_types.as_deref()) {
        Ok(value) => value,
        Err(error) => return (StatusCode::BAD_REQUEST, error).into_response(),
    };
    let marking_filter = parse_csv_string_list(params.markings.as_deref());

    let traversal_params = crate::domain::traversal::TraversalParams {
        starting_object_id,
        max_depth: params.depth.unwrap_or(2),
        link_type_ids,
        marking_filter,
        limit: params.limit.unwrap_or(500),
    };

    match crate::domain::traversal::traverse(&state, &claims, traversal_params).await {
        Ok(edges) => Json(serde_json::json!({
            "starting_object_id": starting_object_id,
            "total": edges.len(),
            "edges": edges,
        }))
        .into_response(),
        Err(error) => {
            tracing::error!("traverse_neighbors failed: {error}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": error.to_string() })),
            )
                .into_response()
        }
    }
}
