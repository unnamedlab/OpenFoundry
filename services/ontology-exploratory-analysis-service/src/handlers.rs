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
    ActionLogEntry, DefinitionId, DefinitionKind, DefinitionQuery, DefinitionRecord, ObjectId,
    Page, PutOutcome, ReadConsistency, RepoError,
};
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    CreateMapRequest, CreateViewRequest, ExploratoryMap, ExploratoryView, WritebackProposal,
    WritebackProposalRequest,
};

const VIEW_KIND: &str = "exploratory_view";
const MAP_KIND: &str = "exploratory_map";
const WRITEBACK_KIND: &str = "exploratory.writeback_proposed";
const PAGE_LIMIT: u32 = 200;

pub async fn list_views(State(state): State<AppState>) -> impl IntoResponse {
    match state
        .definitions
        .list(
            DefinitionQuery {
                kind: kind(VIEW_KIND),
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
            .map(view_from_record)
            .collect::<Result<Vec<_>, _>>()
        {
            Ok(items) => Json(items).into_response(),
            Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
        },
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn create_view(
    State(state): State<AppState>,
    Json(body): Json<CreateViewRequest>,
) -> impl IntoResponse {
    if body.slug.trim().is_empty() {
        return (StatusCode::BAD_REQUEST, "slug is required").into_response();
    }
    if let Err(e) = ensure_slug_available(&state, &body.slug).await {
        return e.into_response();
    }

    let id = Uuid::now_v7();
    let layout = body.layout.unwrap_or_else(|| json!({}));
    let now = now_ms();
    let view = ExploratoryView {
        id,
        slug: body.slug,
        name: body.name,
        object_type: body.object_type,
        filter_spec: body.filter_spec,
        layout,
        created_at: datetime_from_ms(now),
        updated_at: datetime_from_ms(now),
    };

    match state
        .definitions
        .put(view_to_record(&state, &view, now), None)
        .await
    {
        Ok(PutOutcome::VersionConflict { .. }) => {
            (StatusCode::CONFLICT, "view already exists").into_response()
        }
        Ok(_) => (StatusCode::CREATED, Json(view)).into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn get_view(State(state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    match state
        .definitions
        .get(
            &kind(VIEW_KIND),
            &DefinitionId(id.to_string()),
            ReadConsistency::Strong,
        )
        .await
    {
        Ok(Some(row)) => match view_from_record(row) {
            Ok(view) => Json(view).into_response(),
            Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
        },
        Ok(None) => (StatusCode::NOT_FOUND, "view not found").into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn list_maps(State(state): State<AppState>) -> impl IntoResponse {
    match state
        .definitions
        .list(
            DefinitionQuery {
                kind: kind(MAP_KIND),
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
            .map(map_from_record)
            .collect::<Result<Vec<_>, _>>()
        {
            Ok(items) => Json(items).into_response(),
            Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
        },
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn create_map(
    State(state): State<AppState>,
    Json(body): Json<CreateMapRequest>,
) -> impl IntoResponse {
    if let Some(view_id) = body.view_id {
        match state
            .definitions
            .get(
                &kind(VIEW_KIND),
                &DefinitionId(view_id.to_string()),
                ReadConsistency::Strong,
            )
            .await
        {
            Ok(Some(_)) => {}
            Ok(None) => return (StatusCode::NOT_FOUND, "view not found").into_response(),
            Err(e) => return repo_error(e).into_response(),
        }
    }

    let id = Uuid::now_v7();
    let now = now_ms();
    let map = ExploratoryMap {
        id,
        view_id: body.view_id,
        name: body.name,
        map_kind: body.map_kind,
        config: body.config,
        created_at: datetime_from_ms(now),
    };

    match state
        .definitions
        .put(map_to_record(&state, &map, now), None)
        .await
    {
        Ok(PutOutcome::VersionConflict { .. }) => {
            (StatusCode::CONFLICT, "map already exists").into_response()
        }
        Ok(_) => (StatusCode::CREATED, Json(map)).into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

pub async fn propose_writeback(
    State(state): State<AppState>,
    Json(body): Json<WritebackProposalRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let now = now_ms();
    let proposal = WritebackProposal {
        id,
        object_type: body.object_type,
        object_id: body.object_id,
        patch: body.patch,
        note: body.note,
        status: "pending".to_string(),
        created_at: datetime_from_ms(now),
    };

    let entry = ActionLogEntry {
        tenant: state.tenant.clone(),
        event_id: Some(format!("exploratory-writeback:{}", proposal.id)),
        action_id: proposal.id.to_string(),
        kind: WRITEBACK_KIND.to_string(),
        subject: state.subject.clone(),
        object: Some(ObjectId(proposal.object_id.clone())),
        payload: json!({
            "proposal_id": proposal.id,
            "object_type": proposal.object_type,
            "object_id": proposal.object_id,
            "patch": proposal.patch,
            "note": proposal.note,
            "status": proposal.status,
        }),
        recorded_at_ms: now,
    };

    match state.actions.append(entry).await {
        Ok(()) => (StatusCode::ACCEPTED, Json(proposal)).into_response(),
        Err(e) => repo_error(e).into_response(),
    }
}

async fn ensure_slug_available(state: &AppState, slug: &str) -> Result<(), (StatusCode, String)> {
    let mut filters = HashMap::new();
    filters.insert("slug".to_string(), slug.to_string());
    match state
        .definitions
        .list(
            DefinitionQuery {
                kind: kind(VIEW_KIND),
                tenant: Some(state.tenant.clone()),
                owner_id: None,
                parent_id: None,
                filters,
                search: None,
                page: Page {
                    size: 1,
                    token: None,
                },
            },
            ReadConsistency::Strong,
        )
        .await
    {
        Ok(rows) if rows.items.is_empty() => Ok(()),
        Ok(_) => Err((StatusCode::CONFLICT, "slug already exists".to_string())),
        Err(e) => Err(repo_error(e)),
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

fn view_to_record(state: &AppState, view: &ExploratoryView, now: i64) -> DefinitionRecord {
    DefinitionRecord {
        kind: kind(VIEW_KIND),
        id: DefinitionId(view.id.to_string()),
        tenant: Some(state.tenant.clone()),
        owner_id: None,
        parent_id: None,
        version: Some(1),
        payload: json!({
            "id": view.id,
            "slug": view.slug,
            "name": view.name,
            "object_type": view.object_type,
            "filter_spec": view.filter_spec,
            "layout": view.layout,
            "created_at": view.created_at,
            "updated_at": view.updated_at,
        }),
        created_at_ms: Some(now),
        updated_at_ms: Some(now),
    }
}

fn map_to_record(state: &AppState, map: &ExploratoryMap, now: i64) -> DefinitionRecord {
    DefinitionRecord {
        kind: kind(MAP_KIND),
        id: DefinitionId(map.id.to_string()),
        tenant: Some(state.tenant.clone()),
        owner_id: None,
        parent_id: map.view_id.map(|id| DefinitionId(id.to_string())),
        version: Some(1),
        payload: json!({
            "id": map.id,
            "view_id": map.view_id,
            "name": map.name,
            "map_kind": map.map_kind,
            "config": map.config,
            "created_at": map.created_at,
        }),
        created_at_ms: Some(now),
        updated_at_ms: Some(now),
    }
}

fn view_from_record(record: DefinitionRecord) -> Result<ExploratoryView, String> {
    let id =
        Uuid::parse_str(&record.id.0).map_err(|e| format!("stored view id is not a UUID: {e}"))?;
    Ok(ExploratoryView {
        id,
        slug: required_string(&record.payload, "slug")?,
        name: required_string(&record.payload, "name")?,
        object_type: required_string(&record.payload, "object_type")?,
        filter_spec: record
            .payload
            .get("filter_spec")
            .cloned()
            .unwrap_or_else(|| json!({})),
        layout: record
            .payload
            .get("layout")
            .cloned()
            .unwrap_or_else(|| json!({})),
        created_at: datetime_from_ms(record.created_at_ms.or(record.updated_at_ms).unwrap_or(0)),
        updated_at: datetime_from_ms(record.updated_at_ms.or(record.created_at_ms).unwrap_or(0)),
    })
}

fn map_from_record(record: DefinitionRecord) -> Result<ExploratoryMap, String> {
    let id =
        Uuid::parse_str(&record.id.0).map_err(|e| format!("stored map id is not a UUID: {e}"))?;
    let view_id = record
        .parent_id
        .as_ref()
        .map(|id| id.0.as_str())
        .or_else(|| {
            record
                .payload
                .get("view_id")
                .and_then(serde_json::Value::as_str)
        })
        .map(|raw| Uuid::parse_str(raw).map_err(|e| format!("stored view_id is not a UUID: {e}")))
        .transpose()?;

    Ok(ExploratoryMap {
        id,
        view_id,
        name: required_string(&record.payload, "name")?,
        map_kind: required_string(&record.payload, "map_kind")?,
        config: record
            .payload
            .get("config")
            .cloned()
            .unwrap_or_else(|| json!({})),
        created_at: datetime_from_ms(record.created_at_ms.or(record.updated_at_ms).unwrap_or(0)),
    })
}

fn required_string(payload: &serde_json::Value, field: &str) -> Result<String, String> {
    payload
        .get(field)
        .and_then(serde_json::Value::as_str)
        .map(ToOwned::to_owned)
        .ok_or_else(|| format!("stored record is missing {field}"))
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
        ActionLogStore, DefinitionId, DefinitionStore, Page, ReadConsistency, TenantId,
        noop::{InMemoryActionLogStore, InMemoryDefinitionStore},
    };

    fn state() -> (
        AppState,
        Arc<InMemoryDefinitionStore>,
        Arc<InMemoryActionLogStore>,
    ) {
        let definitions = Arc::new(InMemoryDefinitionStore::default());
        let actions = Arc::new(InMemoryActionLogStore::default());
        (
            AppState {
                definitions: definitions.clone(),
                actions: actions.clone(),
                tenant: TenantId("tenant-a".to_string()),
                subject: "analyst-1".to_string(),
            },
            definitions,
            actions,
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
    async fn view_and_map_handlers_use_definition_store() {
        let (state, definitions, _) = state();

        let created_view = create_view(
            State(state.clone()),
            Json(CreateViewRequest {
                slug: "fleet".to_string(),
                name: "Fleet".to_string(),
                object_type: "aircraft".to_string(),
                filter_spec: json!({ "where": "active" }),
                layout: None,
            }),
        )
        .await
        .into_response();
        assert_eq!(created_view.status(), StatusCode::CREATED);
        let view_body = json_body(created_view).await;
        let view_id = Uuid::parse_str(view_body["id"].as_str().expect("id")).expect("uuid");

        assert!(
            definitions
                .get(
                    &kind(VIEW_KIND),
                    &DefinitionId(view_id.to_string()),
                    ReadConsistency::Strong,
                )
                .await
                .expect("definition get")
                .is_some()
        );

        let duplicate = create_view(
            State(state.clone()),
            Json(CreateViewRequest {
                slug: "fleet".to_string(),
                name: "Fleet duplicate".to_string(),
                object_type: "aircraft".to_string(),
                filter_spec: json!({}),
                layout: None,
            }),
        )
        .await
        .into_response();
        assert_eq!(duplicate.status(), StatusCode::CONFLICT);

        let fetched = get_view(State(state.clone()), Path(view_id))
            .await
            .into_response();
        assert_eq!(fetched.status(), StatusCode::OK);

        let created_map = create_map(
            State(state.clone()),
            Json(CreateMapRequest {
                view_id: Some(view_id),
                name: "Map".to_string(),
                map_kind: "geo".to_string(),
                config: json!({ "projection": "mercator" }),
            }),
        )
        .await
        .into_response();
        assert_eq!(created_map.status(), StatusCode::CREATED);

        let maps = list_maps(State(state)).await.into_response();
        assert_eq!(maps.status(), StatusCode::OK);
        let maps = json_body(maps).await;
        assert_eq!(maps.as_array().map(Vec::len), Some(1));
        assert_eq!(maps[0]["view_id"], view_id.to_string());
    }

    #[tokio::test]
    async fn writeback_proposal_appends_to_action_log_store() {
        let (state, _, actions) = state();
        let object_id = Uuid::now_v7();
        let object_id = object_id.to_string();

        let response = propose_writeback(
            State(state.clone()),
            Json(WritebackProposalRequest {
                object_type: "aircraft".to_string(),
                object_id: object_id.clone(),
                patch: json!({ "tail": "N123OF" }),
                note: Some("analyst correction".to_string()),
            }),
        )
        .await
        .into_response();
        assert_eq!(response.status(), StatusCode::ACCEPTED);
        let body = json_body(response).await;
        assert_eq!(body["status"], "pending");

        let entries = actions
            .list_recent(
                &state.tenant,
                Page {
                    size: 10,
                    token: None,
                },
                ReadConsistency::Strong,
            )
            .await
            .expect("action log list");
        assert_eq!(entries.items.len(), 1);
        assert_eq!(entries.items[0].kind, WRITEBACK_KIND);
        assert_eq!(
            entries.items[0].object.as_ref().map(|id| id.0.as_str()),
            Some(object_id.as_str())
        );
        assert_eq!(entries.items[0].payload["status"], "pending");
    }
}
