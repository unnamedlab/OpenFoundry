use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use serde_json::{Value, json};
use uuid::Uuid;

use crate::{
    AppState,
    domain::temporal_adapter::EnqueueTaskRequest,
    models::{CreatePrimaryRequest, CreateSecondaryRequest},
};

pub async fn list_items(State(_state): State<AppState>) -> impl IntoResponse {
    Json(json!({
        "data": [],
        "source": "temporal",
        "note": "automation task state is authoritative in Temporal; legacy queue tables are retired from live runtime"
    }))
    .into_response()
}

pub async fn create_item(
    State(state): State<AppState>,
    Json(body): Json<CreatePrimaryRequest>,
) -> impl IntoResponse {
    let request = match enqueue_request_from_payload(body.payload) {
        Ok(request) => request,
        Err(error) => {
            return (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response();
        }
    };

    match state.adapter.enqueue(&request).await {
        Ok(handle) => (
            StatusCode::ACCEPTED,
            Json(json!({
                "id": request.task_id,
                "payload": request.payload,
                "created_at": Utc::now(),
                "temporal": {
                    "workflow_id": handle.workflow_id.0,
                    "run_id": handle.run_id.0,
                    "authoritative": true
                }
            })),
        )
            .into_response(),
        Err(error) => (
            StatusCode::BAD_REQUEST,
            Json(json!({ "error": error.to_string() })),
        )
            .into_response(),
    }
}

pub async fn get_item(State(_state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    Json(json!({
        "id": id,
        "source": "temporal",
        "temporal": {
            "workflow_id": format!("automation-ops:{id}"),
            "authoritative": true
        }
    }))
    .into_response()
}

pub async fn list_secondary(
    State(_state): State<AppState>,
    Path(parent_id): Path<Uuid>,
) -> impl IntoResponse {
    Json(json!({
        "data": [],
        "parent_id": parent_id,
        "source": "temporal",
        "note": "automation run state is authoritative in Temporal; legacy queue tables are retired from live runtime"
    }))
    .into_response()
}

pub async fn create_secondary(
    State(_state): State<AppState>,
    Path(parent_id): Path<Uuid>,
    Json(body): Json<CreateSecondaryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    (
        StatusCode::ACCEPTED,
        Json(json!({
            "id": id,
            "parent_id": parent_id,
            "payload": body.payload,
            "created_at": Utc::now(),
            "source": "temporal-worker",
            "temporal": {
                "workflow_id": format!("automation-ops:{parent_id}"),
                "authoritative": true
            }
        })),
    )
        .into_response()
}

fn enqueue_request_from_payload(payload: Value) -> Result<EnqueueTaskRequest, String> {
    let task_id = payload
        .get("task_id")
        .and_then(Value::as_str)
        .map(Uuid::parse_str)
        .transpose()
        .map_err(|error| format!("invalid task_id: {error}"))?
        .unwrap_or_else(Uuid::now_v7);
    let tenant_id = payload
        .get("tenant_id")
        .and_then(Value::as_str)
        .unwrap_or("default")
        .to_string();
    let task_type = payload
        .get("task_type")
        .and_then(Value::as_str)
        .filter(|value| !value.trim().is_empty())
        .ok_or_else(|| "payload.task_type is required".to_string())?
        .to_string();
    let audit_correlation_id = payload
        .get("audit_correlation_id")
        .and_then(Value::as_str)
        .map(Uuid::parse_str)
        .transpose()
        .map_err(|error| format!("invalid audit_correlation_id: {error}"))?;

    Ok(EnqueueTaskRequest {
        task_id,
        tenant_id,
        task_type,
        payload,
        audit_correlation_id,
    })
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use async_trait::async_trait;
    use axum::{body::to_bytes, response::IntoResponse};
    use temporal_client::{
        AutomationOpsClient, Namespace, RunId, ScheduleSpec, StartWorkflowOptions, WorkflowClient,
        WorkflowClientError, WorkflowHandle, WorkflowId, WorkflowListPage, task_queues,
        workflow_types,
    };

    use super::*;

    #[derive(Default)]
    struct RecordingWorkflowClient {
        starts: Mutex<Vec<StartWorkflowOptions>>,
    }

    #[async_trait]
    impl WorkflowClient for RecordingWorkflowClient {
        async fn start_workflow(
            &self,
            options: StartWorkflowOptions,
        ) -> temporal_client::Result<WorkflowHandle> {
            let workflow_id = options.workflow_id.clone();
            self.starts.lock().expect("starts mutex").push(options);
            Ok(WorkflowHandle {
                workflow_id,
                run_id: RunId("temporal-run-1".into()),
            })
        }

        async fn signal_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _signal_name: &str,
            _input: serde_json::Value,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal("unexpected signal".into()))
        }

        async fn query_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _query_type: &str,
            _input: serde_json::Value,
        ) -> temporal_client::Result<serde_json::Value> {
            Err(WorkflowClientError::Internal("unexpected query".into()))
        }

        async fn list_workflows(
            &self,
            _namespace: &Namespace,
            _query: &str,
            _page_size: i32,
            _next_page_token: Option<&str>,
        ) -> temporal_client::Result<WorkflowListPage> {
            Err(WorkflowClientError::Internal("unexpected list".into()))
        }

        async fn cancel_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal("unexpected cancel".into()))
        }

        async fn terminate_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal("unexpected terminate".into()))
        }

        async fn create_schedule(
            &self,
            _namespace: &Namespace,
            _spec: ScheduleSpec,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule create".into(),
            ))
        }

        async fn pause_schedule(
            &self,
            _namespace: &Namespace,
            _schedule_id: &str,
            _note: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule pause".into(),
            ))
        }

        async fn delete_schedule(
            &self,
            _namespace: &Namespace,
            _schedule_id: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule delete".into(),
            ))
        }
    }

    fn test_state() -> (AppState, Arc<RecordingWorkflowClient>) {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let client = AutomationOpsClient::new(recorder.clone(), Namespace::new("ops-tenant"));
        (
            AppState {
                adapter: crate::domain::temporal_adapter::AutomationOpsAdapter::new(client),
            },
            recorder,
        )
    }

    async fn response_json(response: axum::response::Response) -> serde_json::Value {
        let body = to_bytes(response.into_body(), usize::MAX)
            .await
            .expect("response body");
        serde_json::from_slice(&body).expect("json body")
    }

    #[test]
    fn enqueue_request_requires_task_type() {
        let error = enqueue_request_from_payload(json!({"tenant_id": "acme"}))
            .expect_err("missing task_type");
        assert_eq!(error, "payload.task_type is required");
    }

    #[test]
    fn enqueue_request_preserves_task_identity() {
        let task_id = Uuid::now_v7();
        let audit = Uuid::now_v7();
        let request = enqueue_request_from_payload(json!({
            "task_id": task_id,
            "tenant_id": "acme",
            "task_type": "retention.sweep",
            "audit_correlation_id": audit
        }))
        .expect("request");

        assert_eq!(request.task_id, task_id);
        assert_eq!(request.tenant_id, "acme");
        assert_eq!(request.task_type, "retention.sweep");
        assert_eq!(request.audit_correlation_id, Some(audit));
    }

    #[tokio::test]
    async fn create_item_enqueues_temporal_task() {
        let (state, recorder) = test_state();
        let task_id = Uuid::now_v7();
        let audit = Uuid::now_v7();
        let response = create_item(
            State(state),
            Json(CreatePrimaryRequest {
                payload: json!({
                    "task_id": task_id,
                    "tenant_id": "acme",
                    "task_type": "retention.sweep",
                    "audit_correlation_id": audit,
                    "scope": "datasets"
                }),
            }),
        )
        .await
        .into_response();

        assert_eq!(response.status(), StatusCode::ACCEPTED);
        let body = response_json(response).await;
        assert_eq!(body["id"], task_id.to_string());
        assert_eq!(
            body["temporal"]["workflow_id"],
            format!("automation-ops:{task_id}")
        );
        assert_eq!(body["temporal"]["run_id"], "temporal-run-1");
        assert_eq!(body["temporal"]["authoritative"], true);

        let starts = recorder.starts.lock().expect("starts mutex");
        assert_eq!(starts.len(), 1);
        let start = &starts[0];
        assert_eq!(start.namespace.0, "ops-tenant");
        assert_eq!(start.workflow_id.0, format!("automation-ops:{task_id}"));
        assert_eq!(start.workflow_type.0, workflow_types::AUTOMATION_OPS_TASK);
        assert_eq!(start.task_queue.0, task_queues::AUTOMATION_OPS);
        assert_eq!(start.input["task_id"], task_id.to_string());
        assert_eq!(start.input["tenant_id"], "acme");
        assert_eq!(start.input["task_type"], "retention.sweep");
        assert_eq!(
            start
                .search_attributes
                .get(StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION),
            Some(&serde_json::Value::String(audit.to_string()))
        );
    }

    #[tokio::test]
    async fn list_get_and_runs_are_temporal_projections() {
        let (state, _recorder) = test_state();
        let task_id = Uuid::now_v7();

        let list = list_items(State(state.clone())).await.into_response();
        assert_eq!(list.status(), StatusCode::OK);
        let list_body = response_json(list).await;
        assert_eq!(list_body["source"], "temporal");
        assert_eq!(list_body["data"], json!([]));

        let get = get_item(State(state.clone()), Path(task_id))
            .await
            .into_response();
        assert_eq!(get.status(), StatusCode::OK);
        let get_body = response_json(get).await;
        assert_eq!(
            get_body["temporal"]["workflow_id"],
            format!("automation-ops:{task_id}")
        );

        let runs = list_secondary(State(state.clone()), Path(task_id))
            .await
            .into_response();
        assert_eq!(runs.status(), StatusCode::OK);
        let runs_body = response_json(runs).await;
        assert_eq!(runs_body["parent_id"], task_id.to_string());
        assert_eq!(runs_body["source"], "temporal");

        let run = create_secondary(
            State(state),
            Path(task_id),
            Json(CreateSecondaryRequest {
                payload: json!({"status": "completed"}),
            }),
        )
        .await
        .into_response();
        assert_eq!(run.status(), StatusCode::ACCEPTED);
        let run_body = response_json(run).await;
        assert_eq!(run_body["parent_id"], task_id.to_string());
        assert_eq!(run_body["source"], "temporal-worker");
        assert_eq!(
            run_body["temporal"]["workflow_id"],
            format!("automation-ops:{task_id}")
        );
    }
}
