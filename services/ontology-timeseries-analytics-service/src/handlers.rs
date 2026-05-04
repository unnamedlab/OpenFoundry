use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use serde_json::json;
use std::collections::HashMap;
use storage_abstraction::repositories::{
    DefinitionId, DefinitionKind, DefinitionQuery, DefinitionRecord, Page, PutOutcome,
    ReadConsistency, RepoError,
};
use uuid::Uuid;

use crate::AppState;
use crate::models::{CreatePrimaryRequest, CreateSecondaryRequest, PrimaryItem, SecondaryItem};

const DASHBOARD_KIND: &str = "ontology_timeseries_dashboard";
const QUERY_KIND: &str = "ontology_timeseries_query";
const PAGE_LIMIT: u32 = 200;

pub async fn list_items(State(state): State<AppState>) -> impl IntoResponse {
    match state
        .definitions
        .list(
            DefinitionQuery {
                kind: kind(DASHBOARD_KIND),
                tenant: Some(state.tenant.clone()),
                owner_id: None,
                parent_id: None,
                filters: HashMap::new(),
                search: None,
                page: Page {
                    size: PAGE_LIMIT,
                    token: None,
                },
            },
            ReadConsistency::Eventual,
        )
        .await
    {
        Ok(rows) => match rows
            .items
            .into_iter()
            .map(primary_from_record)
            .collect::<Result<Vec<_>, _>>()
        {
            Ok(items) => Json(items).into_response(),
            Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
        },
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn create_item(
    State(state): State<AppState>,
    Json(body): Json<CreatePrimaryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let now = now_ms();
    let item = PrimaryItem {
        id,
        payload: body.payload,
        created_at: datetime_from_ms(now),
    };
    match state
        .definitions
        .put(primary_to_record(&state, &item, now), None)
        .await
    {
        Ok(PutOutcome::VersionConflict { .. }) => {
            (StatusCode::CONFLICT, "dashboard already exists").into_response()
        }
        Ok(_) => (StatusCode::CREATED, Json(item)).into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn get_item(State(state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    match state
        .definitions
        .get(
            &kind(DASHBOARD_KIND),
            &DefinitionId(id.to_string()),
            ReadConsistency::Strong,
        )
        .await
    {
        Ok(Some(row)) => match primary_from_record(row) {
            Ok(item) => Json(item).into_response(),
            Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
        },
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn list_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
) -> impl IntoResponse {
    match state
        .definitions
        .list(
            DefinitionQuery {
                kind: kind(QUERY_KIND),
                tenant: Some(state.tenant.clone()),
                owner_id: None,
                parent_id: Some(DefinitionId(parent_id.to_string())),
                filters: HashMap::new(),
                search: None,
                page: Page {
                    size: PAGE_LIMIT,
                    token: None,
                },
            },
            ReadConsistency::Eventual,
        )
        .await
    {
        Ok(rows) => match rows
            .items
            .into_iter()
            .map(secondary_from_record)
            .collect::<Result<Vec<_>, _>>()
        {
            Ok(items) => Json(items).into_response(),
            Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
        },
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn create_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
    Json(body): Json<CreateSecondaryRequest>,
) -> impl IntoResponse {
    match state
        .definitions
        .get(
            &kind(DASHBOARD_KIND),
            &DefinitionId(parent_id.to_string()),
            ReadConsistency::Strong,
        )
        .await
    {
        Ok(Some(_)) => {}
        Ok(None) => return (StatusCode::NOT_FOUND, "dashboard not found").into_response(),
        Err(e) => return repo_error(e).into_response(),
    }

    let id = Uuid::now_v7();
    let now = now_ms();
    let item = SecondaryItem {
        id,
        parent_id,
        payload: body.payload,
        created_at: datetime_from_ms(now),
    };
    match state
        .definitions
        .put(secondary_to_record(&state, &item, now), None)
        .await
    {
        Ok(PutOutcome::VersionConflict { .. }) => {
            (StatusCode::CONFLICT, "saved query already exists").into_response()
        }
        Ok(_) => (StatusCode::CREATED, Json(item)).into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

fn kind(value: &str) -> DefinitionKind {
    DefinitionKind(value.to_string())
}

fn now_ms() -> i64 {
    Utc::now().timestamp_millis()
}

fn datetime_from_ms(value: i64) -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp_millis(value).unwrap_or_else(Utc::now)
}

fn primary_to_record(state: &AppState, item: &PrimaryItem, now: i64) -> DefinitionRecord {
    DefinitionRecord {
        kind: kind(DASHBOARD_KIND),
        id: DefinitionId(item.id.to_string()),
        tenant: Some(state.tenant.clone()),
        owner_id: None,
        parent_id: None,
        version: Some(1),
        payload: json!({
            "id": item.id,
            "payload": item.payload,
            "created_at": item.created_at,
        }),
        created_at_ms: Some(now),
        updated_at_ms: Some(now),
    }
}

fn secondary_to_record(state: &AppState, item: &SecondaryItem, now: i64) -> DefinitionRecord {
    DefinitionRecord {
        kind: kind(QUERY_KIND),
        id: DefinitionId(item.id.to_string()),
        tenant: Some(state.tenant.clone()),
        owner_id: None,
        parent_id: Some(DefinitionId(item.parent_id.to_string())),
        version: Some(1),
        payload: json!({
            "id": item.id,
            "parent_id": item.parent_id,
            "payload": item.payload,
            "created_at": item.created_at,
        }),
        created_at_ms: Some(now),
        updated_at_ms: Some(now),
    }
}

fn primary_from_record(record: DefinitionRecord) -> Result<PrimaryItem, String> {
    let id = Uuid::parse_str(&record.id.0)
        .map_err(|e| format!("stored dashboard id is not a UUID: {e}"))?;
    Ok(PrimaryItem {
        id,
        payload: record
            .payload
            .get("payload")
            .cloned()
            .unwrap_or(serde_json::Value::Null),
        created_at: datetime_from_ms(record.created_at_ms.or(record.updated_at_ms).unwrap_or(0)),
    })
}

fn secondary_from_record(record: DefinitionRecord) -> Result<SecondaryItem, String> {
    let id = Uuid::parse_str(&record.id.0)
        .map_err(|e| format!("stored saved-query id is not a UUID: {e}"))?;
    let parent_id = record
        .parent_id
        .as_ref()
        .map(|id| id.0.as_str())
        .or_else(|| {
            record
                .payload
                .get("parent_id")
                .and_then(serde_json::Value::as_str)
        })
        .ok_or_else(|| "stored saved-query is missing parent_id".to_string())
        .and_then(|raw| {
            Uuid::parse_str(raw).map_err(|e| format!("stored parent_id is not a UUID: {e}"))
        })?;

    Ok(SecondaryItem {
        id,
        parent_id,
        payload: record
            .payload
            .get("payload")
            .cloned()
            .unwrap_or(serde_json::Value::Null),
        created_at: datetime_from_ms(record.created_at_ms.or(record.updated_at_ms).unwrap_or(0)),
    })
}

fn repo_error(error: RepoError) -> (StatusCode, String) {
    let status = match error {
        RepoError::InvalidArgument(_) | RepoError::TenantScope(_) => StatusCode::BAD_REQUEST,
        RepoError::NotFound(_) => StatusCode::NOT_FOUND,
        RepoError::Backend(_) => StatusCode::INTERNAL_SERVER_ERROR,
    };
    (status, error.to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::response::IntoResponse;
    use http_body_util::BodyExt;
    use std::sync::Arc;
    use storage_abstraction::repositories::{
        DefinitionStore, TenantId, noop::InMemoryDefinitionStore,
    };

    fn state() -> (AppState, Arc<InMemoryDefinitionStore>) {
        let definitions = Arc::new(InMemoryDefinitionStore::default());
        (
            AppState {
                definitions: definitions.clone(),
                tenant: TenantId("tenant-a".to_string()),
            },
            definitions,
        )
    }

    async fn json_body(response: axum::response::Response) -> serde_json::Value {
        let body = response
            .into_body()
            .collect()
            .await
            .expect("body")
            .to_bytes();
        serde_json::from_slice(&body).expect("json body")
    }

    #[tokio::test]
    async fn dashboard_handlers_round_trip_through_definition_store() {
        let (state, definitions) = state();

        let created = create_item(
            State(state.clone()),
            Json(CreatePrimaryRequest {
                payload: json!({ "title": "Fleet telemetry" }),
            }),
        )
        .await
        .into_response();
        assert_eq!(created.status(), StatusCode::CREATED);
        let body = json_body(created).await;
        let id = Uuid::parse_str(body["id"].as_str().expect("id")).expect("uuid");

        assert!(
            definitions
                .get(
                    &kind(DASHBOARD_KIND),
                    &DefinitionId(id.to_string()),
                    ReadConsistency::Strong,
                )
                .await
                .expect("definition get")
                .is_some()
        );

        let fetched = get_item(State(state.clone()), Path(id))
            .await
            .into_response();
        assert_eq!(fetched.status(), StatusCode::OK);

        let listed = list_items(State(state)).await.into_response();
        assert_eq!(listed.status(), StatusCode::OK);
        let listed = json_body(listed).await;
        assert_eq!(listed.as_array().map(Vec::len), Some(1));
    }

    #[tokio::test]
    async fn saved_query_handlers_validate_parent_and_use_definition_store() {
        let (state, _) = state();
        let missing_parent = Uuid::now_v7();
        let response = create_secondary(
            State(state.clone()),
            Path(missing_parent),
            Json(CreateSecondaryRequest {
                payload: json!({ "sql": "select 1" }),
            }),
        )
        .await
        .into_response();
        assert_eq!(response.status(), StatusCode::NOT_FOUND);

        let created_parent = create_item(
            State(state.clone()),
            Json(CreatePrimaryRequest {
                payload: json!({ "title": "Fleet telemetry" }),
            }),
        )
        .await
        .into_response();
        let parent_body = json_body(created_parent).await;
        let parent_id = Uuid::parse_str(parent_body["id"].as_str().expect("id")).expect("uuid");

        let created_query = create_secondary(
            State(state.clone()),
            Path(parent_id),
            Json(CreateSecondaryRequest {
                payload: json!({ "metric": "fuel" }),
            }),
        )
        .await
        .into_response();
        assert_eq!(created_query.status(), StatusCode::CREATED);

        let listed = list_secondary(State(state), Path(parent_id))
            .await
            .into_response();
        assert_eq!(listed.status(), StatusCode::OK);
        let listed = json_body(listed).await;
        assert_eq!(listed.as_array().map(Vec::len), Some(1));
        assert_eq!(listed[0]["parent_id"], parent_id.to_string());
    }
}
