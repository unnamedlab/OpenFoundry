use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    AgentDefinition, AgentRun, AgentRunStep, CreateAgentRequest, HumanApprovalRequest,
    RecordStepRequest, StartRunRequest, UpdateAgentRequest,
};

pub async fn list_agents(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, AgentDefinition>(
        "SELECT * FROM agent_definitions ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_agent(
    State(state): State<AppState>,
    Json(body): Json<CreateAgentRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let tools = body.tools.unwrap_or_else(|| json!([]));
    match sqlx::query_as::<_, AgentDefinition>(
        "INSERT INTO agent_definitions (id, slug, name, description, system_prompt, provider_id, tools, status) \
         VALUES ($1, $2, $3, $4, $5, $6, $7, 'active') RETURNING *",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.name)
    .bind(&body.description)
    .bind(&body.system_prompt)
    .bind(body.provider_id)
    .bind(&tools)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_agent(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, AgentDefinition>("SELECT * FROM agent_definitions WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "agent not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn update_agent(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateAgentRequest>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, AgentDefinition>(
        "UPDATE agent_definitions SET \
            name = COALESCE($2, name), \
            description = COALESCE($3, description), \
            system_prompt = COALESCE($4, system_prompt), \
            tools = COALESCE($5, tools), \
            status = COALESCE($6, status), \
            updated_at = now() \
         WHERE id = $1 RETURNING *",
    )
    .bind(id)
    .bind(&body.name)
    .bind(&body.description)
    .bind(&body.system_prompt)
    .bind(&body.tools)
    .bind(&body.status)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "agent not found").into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn list_runs(
    State(state): State<AppState>,
    Path(agent_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, AgentRun>(
        "SELECT * FROM agent_runs WHERE agent_id = $1 ORDER BY created_at DESC",
    )
    .bind(agent_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn start_run(
    State(state): State<AppState>,
    Path(agent_id): Path<Uuid>,
    Json(body): Json<StartRunRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, AgentRun>(
        "INSERT INTO agent_runs (id, agent_id, conversation_id, status, input) \
         VALUES ($1, $2, $3, 'running', $4) RETURNING *",
    )
    .bind(id)
    .bind(agent_id)
    .bind(body.conversation_id)
    .bind(&body.input)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn record_step(
    State(state): State<AppState>,
    Path((_agent_id, run_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<RecordStepRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, AgentRunStep>(
        "INSERT INTO agent_run_steps (id, run_id, step_index, kind, payload) \
         VALUES ($1, $2, $3, $4, $5) RETURNING *",
    )
    .bind(id)
    .bind(run_id)
    .bind(body.step_index)
    .bind(&body.kind)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn submit_human_approval(
    State(state): State<AppState>,
    Path((_agent_id, run_id)): Path<(Uuid, Uuid)>,
    Json(body): Json<HumanApprovalRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let payload = json!({
        "decision": body.decision,
        "reviewer_id": body.reviewer_id,
        "note": body.note,
    });
    match sqlx::query_as::<_, AgentRunStep>(
        "INSERT INTO agent_run_steps (id, run_id, step_index, kind, payload) \
         VALUES ($1, $2, COALESCE((SELECT MAX(step_index) + 1 FROM agent_run_steps WHERE run_id = $2), 0), 'human_approval', $3) RETURNING *",
    )
    .bind(id)
    .bind(run_id)
    .bind(&payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn create_chat_completion(
    State(_state): State<AppState>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    let response = json!({
        "id": Uuid::now_v7(),
        "object": "chat.completion",
        "model": body.get("model").cloned().unwrap_or_else(|| json!("agent-runtime-default")),
        "choices": [{
            "index": 0,
            "message": {
                "role": "assistant",
                "content": "agent-runtime stub: chat completion not yet implemented",
            },
            "finish_reason": "stop",
        }],
        "usage": { "prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0 },
    });
    (StatusCode::OK, Json(response))
}

pub async fn ask_copilot(
    State(_state): State<AppState>,
    Json(body): Json<serde_json::Value>,
) -> impl IntoResponse {
    let response = json!({
        "id": Uuid::now_v7(),
        "answer": "agent-runtime stub: copilot answer not yet implemented",
        "context": body,
    });
    (StatusCode::OK, Json(response))
}
