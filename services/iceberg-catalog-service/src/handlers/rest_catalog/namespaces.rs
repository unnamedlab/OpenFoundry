//! REST Catalog § Namespaces.
//!
//! The Iceberg spec encodes hierarchical namespace paths with a `0x1F`
//! unit-separator on the wire (`ab`); the body request also
//! carries the path as a JSON array. Python clients use dot-separated
//! paths in URL segments. We accept all of:
//!
//!   * URL segment:  `/iceberg/v1/namespaces/a.b`
//!   * URL segment with `%1F` URL-encoded unit separator
//!   * JSON body:   `{"namespace": ["a", "b"], "properties": {}}`

use axum::extract::{Path, Query, State};
use axum::http::HeaderMap;
use axum::{Json, http::StatusCode};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::collections::HashMap;
use uuid::Uuid;

use crate::AppState;
use crate::audit;
use crate::domain::namespace::{self, Namespace, decode_path};
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;
use crate::handlers::rest_catalog::resolve_project_rid;
use crate::metrics;

#[derive(Debug, Serialize)]
pub struct NamespaceListResponse {
    pub namespaces: Vec<Vec<String>>,
}

#[derive(Debug, Deserialize)]
pub struct NamespaceQuery {
    #[serde(default)]
    pub parent: Option<String>,
    #[serde(default)]
    pub page_token: Option<String>,
    #[serde(default)]
    pub page_size: Option<i64>,
}

#[derive(Debug, Deserialize)]
pub struct CreateNamespaceRequest {
    pub namespace: Vec<String>,
    #[serde(default)]
    pub properties: HashMap<String, String>,
}

#[derive(Debug, Serialize)]
pub struct CreateNamespaceResponse {
    pub namespace: Vec<String>,
    pub properties: HashMap<String, String>,
}

#[derive(Debug, Serialize)]
pub struct LoadNamespaceResponse {
    pub namespace: Vec<String>,
    pub properties: HashMap<String, String>,
}

#[derive(Debug, Deserialize)]
pub struct UpdatePropertiesRequest {
    #[serde(default)]
    pub removals: Vec<String>,
    #[serde(default)]
    pub updates: HashMap<String, String>,
}

#[derive(Debug, Serialize)]
pub struct UpdatePropertiesResponse {
    pub updated: Vec<String>,
    pub removed: Vec<String>,
    pub missing: Vec<String>,
}

pub async fn list_namespaces(
    State(state): State<AppState>,
    headers: HeaderMap,
    Query(query): Query<NamespaceQuery>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<NamespaceListResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let parent_path = query.parent.as_ref().map(|p| decode_path(p));
    let parent_slice = parent_path.as_deref();
    let namespaces = namespace::list(&state.iceberg.db, &project_rid, parent_slice).await?;

    let response = NamespaceListResponse {
        namespaces: namespaces
            .iter()
            .map(|n| decode_path(&n.name))
            .collect(),
    };

    metrics::record_rest_request("GET", "/iceberg/v1/namespaces", 200);
    Ok(Json(response))
}

pub async fn create_namespace(
    State(state): State<AppState>,
    headers: HeaderMap,
    principal: AuthenticatedPrincipal,
    Json(body): Json<CreateNamespaceRequest>,
) -> Result<(StatusCode, Json<CreateNamespaceResponse>), ApiError> {
    if body.namespace.is_empty() {
        return Err(ApiError::BadRequest(
            "namespace must contain at least one segment".to_string(),
        ));
    }
    let project_rid = resolve_project_rid(&headers);
    let actor = parse_actor(&principal)?;

    let properties: Value = serde_json::to_value(&body.properties)
        .map_err(|err| ApiError::BadRequest(err.to_string()))?;
    let ns =
        namespace::create(&state.iceberg.db, &project_rid, &body.namespace, properties, actor, None)
            .await?;

    audit::namespace_created(actor, &project_rid, &ns.name);
    metrics::record_rest_request("POST", "/iceberg/v1/namespaces", 200);

    Ok((
        StatusCode::OK,
        Json(CreateNamespaceResponse {
            namespace: body.namespace,
            properties: body.properties,
        }),
    ))
}

pub async fn load_namespace(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<LoadNamespaceResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_namespace_segment(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    metrics::record_rest_request("GET", "/iceberg/v1/namespaces/{ns}", 200);
    Ok(Json(LoadNamespaceResponse {
        namespace: decode_path(&ns.name),
        properties: namespace_properties_to_map(&ns),
    }))
}

pub async fn drop_namespace(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    principal: AuthenticatedPrincipal,
) -> Result<StatusCode, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_namespace_segment(&namespace_path);
    namespace::drop(&state.iceberg.db, &project_rid, &path).await?;

    let actor = parse_actor(&principal)?;
    audit::namespace_deleted(actor, &project_rid, &path.join("."));
    metrics::record_rest_request("DELETE", "/iceberg/v1/namespaces/{ns}", 204);
    Ok(StatusCode::NO_CONTENT)
}

pub async fn get_properties(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    _principal: AuthenticatedPrincipal,
) -> Result<Json<LoadNamespaceResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_namespace_segment(&namespace_path);
    let ns = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    Ok(Json(LoadNamespaceResponse {
        namespace: decode_path(&ns.name),
        properties: namespace_properties_to_map(&ns),
    }))
}

pub async fn update_properties(
    State(state): State<AppState>,
    headers: HeaderMap,
    Path(namespace_path): Path<String>,
    _principal: AuthenticatedPrincipal,
    Json(body): Json<UpdatePropertiesRequest>,
) -> Result<Json<UpdatePropertiesResponse>, ApiError> {
    let project_rid = resolve_project_rid(&headers);
    let path = decode_namespace_segment(&namespace_path);

    let current = namespace::fetch(&state.iceberg.db, &project_rid, &path).await?;
    let mut props = current
        .properties
        .as_object()
        .cloned()
        .unwrap_or_else(serde_json::Map::new);

    let mut removed = Vec::new();
    let mut missing = Vec::new();
    for key in body.removals.iter() {
        if props.remove(key).is_some() {
            removed.push(key.clone());
        } else {
            missing.push(key.clone());
        }
    }
    let mut updated = Vec::new();
    for (key, value) in body.updates.iter() {
        props.insert(key.clone(), Value::String(value.clone()));
        updated.push(key.clone());
    }

    namespace::replace_properties(&state.iceberg.db, &project_rid, &path, Value::Object(props))
        .await?;

    Ok(Json(UpdatePropertiesResponse {
        updated,
        removed,
        missing,
    }))
}

fn decode_namespace_segment(raw: &str) -> Vec<String> {
    // Iceberg clients sometimes URL-encode the unit-separator (%1F)
    // when sending hierarchical paths. Normalize to dots.
    let normalized = raw.replace('\u{1F}', ".");
    decode_path(&normalized)
}

fn parse_actor(principal: &AuthenticatedPrincipal) -> Result<Uuid, ApiError> {
    Uuid::parse_str(&principal.subject)
        .or_else(|_| Ok::<Uuid, ApiError>(Uuid::nil()))
}

fn namespace_properties_to_map(ns: &Namespace) -> HashMap<String, String> {
    ns.properties
        .as_object()
        .map(|m| {
            m.iter()
                .map(|(k, v)| (k.clone(), v.as_str().unwrap_or_default().to_string()))
                .collect()
        })
        .unwrap_or_default()
}
