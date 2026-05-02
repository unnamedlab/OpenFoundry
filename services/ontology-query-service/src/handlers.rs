//! HTTP handlers for `ontology-query-service`.
//!
//! Substrate-only set: a single point read and a single page-by-type
//! read. They demonstrate the
//! [`ConsistencyHint`](crate::consistency::ConsistencyHint) plumbing
//! against an `Arc<dyn ObjectStore>`. The richer query/search surface
//! that lives in `libs/ontology-kernel/src/handlers/` migrates in a
//! follow-up batch (see the `[~]` annotation on S1.5 in the migration
//! plan).

use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde::{Deserialize, Serialize};
use storage_abstraction::repositories::{
    Object, ObjectId, Page, PagedResult, RepoError, TenantId, TypeId,
};

use crate::QueryState;
use crate::consistency::ConsistencyHint;

#[derive(Debug, Deserialize, Default)]
pub struct ListByTypeParams {
    /// Page size; clamped to `[1, 5000]` by the storage layer.
    #[serde(default = "default_page_size")]
    pub size: u32,
    /// Opaque continuation token returned by a prior call.
    #[serde(default)]
    pub token: Option<String>,
}

fn default_page_size() -> u32 {
    100
}

#[derive(Debug, Serialize)]
pub struct ListResponse<T> {
    pub items: Vec<T>,
    pub next_token: Option<String>,
}

impl<T> From<PagedResult<T>> for ListResponse<T> {
    fn from(p: PagedResult<T>) -> Self {
        Self {
            items: p.items,
            next_token: p.next_token,
        }
    }
}

/// `GET /api/v1/ontology/objects/{tenant}/{object_id}` — point read.
pub async fn get_object(
    State(state): State<QueryState>,
    Path((tenant, object_id)): Path<(String, String)>,
    ConsistencyHint(consistency): ConsistencyHint,
) -> Response {
    let result = state
        .objects
        .get(&TenantId(tenant), &ObjectId(object_id), consistency)
        .await;

    match result {
        Ok(Some(obj)) => (StatusCode::OK, Json(obj)).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(err) => repo_error_to_response(err),
    }
}

/// `GET /api/v1/ontology/objects/{tenant}/by-type/{type_id}` — page
/// over a type. Honours `X-Consistency` and the standard
/// `?size=&token=` query parameters.
pub async fn list_objects_by_type(
    State(state): State<QueryState>,
    Path((tenant, type_id)): Path<(String, String)>,
    Query(params): Query<ListByTypeParams>,
    ConsistencyHint(consistency): ConsistencyHint,
) -> Response {
    let page = Page {
        size: params.size,
        token: params.token,
    };
    let res = state
        .objects
        .list_by_type(&TenantId(tenant), &TypeId(type_id), page, consistency)
        .await;

    match res {
        Ok(p) => (StatusCode::OK, Json(ListResponse::<Object>::from(p))).into_response(),
        Err(err) => repo_error_to_response(err),
    }
}

fn repo_error_to_response(err: RepoError) -> Response {
    let status = match &err {
        RepoError::NotFound(_) => StatusCode::NOT_FOUND,
        RepoError::InvalidArgument(_) => StatusCode::BAD_REQUEST,
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    };
    (status, err.to_string()).into_response()
}
