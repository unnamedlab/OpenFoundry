use axum::{
    Json,
    extract::{Path, Query, State},
    http::{HeaderValue, StatusCode},
    response::{IntoResponse, Response},
};
use serde::Deserialize;
use serde_json::json;
use uuid::Uuid;

use crate::AppState;
use crate::domain::lineage;
use crate::query_router::{self, QueryKind, QueryPlan};
use auth_middleware::layer::AuthUser;

#[derive(Debug, Clone, Deserialize, Default)]
pub struct LineageQueryRequest {
    #[serde(default)]
    pub historical: bool,
    #[serde(default)]
    pub window_hours: Option<u32>,
}

pub async fn get_dataset_lineage(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Query(query): Query<LineageQueryRequest>,
) -> impl IntoResponse {
    if let Err(error) = lineage::ensure_dataset_snapshot(&state, dataset_id).await {
        tracing::warn!(dataset_id = %dataset_id, "dataset snapshot refresh failed: {error}");
    }

    let plan = query_plan(QueryKind::DatasetGraph, &query);
    log_degraded_query(Some(dataset_id), plan);

    match lineage::get_lineage_graph(&state, dataset_id, plan).await {
        Ok(graph) => with_query_headers(
            Json(lineage::filter_graph_for_claims(graph, &claims)).into_response(),
            plan,
        ),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn get_dataset_column_lineage(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Query(query): Query<LineageQueryRequest>,
) -> impl IntoResponse {
    let plan = query_plan(QueryKind::DatasetColumns, &query);
    log_degraded_query(Some(dataset_id), plan);

    match lineage::get_dataset_column_lineage(&state, dataset_id, plan).await {
        Ok(edges) => with_query_headers(Json(edges).into_response(), plan),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn get_full_lineage(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<LineageQueryRequest>,
) -> impl IntoResponse {
    let plan = query_plan(QueryKind::FullGraph, &query);
    log_degraded_query(None, plan);

    match lineage::get_full_lineage_graph(&state, plan).await {
        Ok(graph) => with_query_headers(
            Json(lineage::filter_graph_for_claims(graph, &claims)).into_response(),
            plan,
        ),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn get_dataset_lineage_impact(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Query(query): Query<LineageQueryRequest>,
) -> impl IntoResponse {
    if let Err(error) = lineage::ensure_dataset_snapshot(&state, dataset_id).await {
        tracing::warn!(dataset_id = %dataset_id, "dataset snapshot refresh failed: {error}");
    }

    let plan = query_plan(QueryKind::DatasetImpact, &query);
    log_degraded_query(Some(dataset_id), plan);

    match lineage::get_lineage_impact_analysis(&state, dataset_id, plan).await {
        Ok(Some(impact)) => match lineage::filter_impact_for_claims(impact, &claims) {
            Ok(impact) => with_query_headers(Json(impact).into_response(), plan),
            Err(error) => (StatusCode::FORBIDDEN, Json(json!({ "error": error }))).into_response(),
        },
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => (StatusCode::INTERNAL_SERVER_ERROR, error.to_string()).into_response(),
    }
}

pub async fn trigger_dataset_lineage_builds(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Json(body): Json<lineage::LineageBuildRequest>,
) -> impl IntoResponse {
    if let Err(error) = lineage::ensure_dataset_snapshot(&state, dataset_id).await {
        tracing::warn!(dataset_id = %dataset_id, "dataset snapshot refresh failed: {error}");
    }

    match lineage::get_lineage_impact_analysis(
        &state,
        dataset_id,
        query_plan(QueryKind::DatasetImpact, &LineageQueryRequest::default()),
    )
    .await
    {
        Ok(Some(impact)) => {
            if let Err(error) = lineage::filter_impact_for_claims(impact, &claims) {
                return (StatusCode::FORBIDDEN, Json(json!({ "error": error }))).into_response();
            }
        }
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": error.to_string() })),
            )
                .into_response();
        }
    }

    match lineage::trigger_lineage_builds(&state, dataset_id, claims.sub, body).await {
        Ok(result) => Json(result).into_response(),
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

pub async fn sync_workflow_lineage(
    State(state): State<AppState>,
    Path(workflow_id): Path<Uuid>,
    Json(body): Json<lineage::WorkflowLineageSyncRequest>,
) -> impl IntoResponse {
    match lineage::sync_workflow_lineage(&state, workflow_id, body).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(error) => (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response(),
    }
}

pub async fn delete_workflow_lineage(
    State(state): State<AppState>,
    Path(workflow_id): Path<Uuid>,
) -> impl IntoResponse {
    match lineage::delete_workflow_lineage(&state, workflow_id).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(error) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": error.to_string() })),
        )
            .into_response(),
    }
}

fn query_plan(kind: QueryKind, query: &LineageQueryRequest) -> QueryPlan {
    let enabled = std::env::var("LINEAGE_TRINO_ENABLED").ok();
    let trino_available = query_router::trino_available_from_env(enabled.as_deref());
    query_router::plan(kind, query.window_hours, query.historical, trino_available)
}

fn with_query_headers(mut response: Response, plan: QueryPlan) -> Response {
    let headers = response.headers_mut();
    headers.insert(
        "x-openfoundry-lineage-scope",
        HeaderValue::from_static(plan.kind.as_scope_label()),
    );
    headers.insert(
        "x-openfoundry-lineage-requested-source",
        HeaderValue::from_static(plan.requested_source.as_metric_label()),
    );
    headers.insert(
        "x-openfoundry-lineage-served-source",
        HeaderValue::from_static(plan.selected_source.as_metric_label()),
    );
    headers.insert(
        "x-openfoundry-lineage-degraded",
        HeaderValue::from_static(if plan.degraded { "true" } else { "false" }),
    );
    if let Ok(window_hours) = HeaderValue::from_str(&plan.window_hours.to_string()) {
        headers.insert("x-openfoundry-lineage-window-hours", window_hours);
    }
    response
}

fn log_degraded_query(subject_id: Option<Uuid>, plan: QueryPlan) {
    if plan.degraded && plan.is_historical() {
        tracing::info!(
            subject_id = subject_id.map(|value| value.to_string()),
            scope = plan.kind.as_scope_label(),
            requested_source = plan.requested_source.as_metric_label(),
            served_source = plan.selected_source.as_metric_label(),
            window_hours = plan.window_hours,
            "historical lineage query degraded to Cassandra read-model"
        );
    }
}
