use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::domain::lifecycle::PipelineLifecycle;
use crate::domain::pipeline_type::{
    self, ExternalConfig, IncrementalConfig, PipelineType, StreamingConfig,
};
use crate::domain::{compiler, executor};
use crate::models::authoring::PipelineValidationResponse;
use crate::models::pipeline::*;
use auth_middleware::layer::AuthUser;

/// Resolve the pipeline kind from a string, defaulting to `BATCH` when
/// missing or unparseable. Unparseable strings still bubble up as a
/// coherence error via the DB CHECK; we don't fail-fast here so legacy
/// rows continue to load.
fn resolve_pipeline_type(raw: Option<&str>) -> PipelineType {
    raw.and_then(pipeline_type::PipelineType::parse)
        .unwrap_or_default()
}

fn config_to_value<T: serde::Serialize>(opt: &Option<T>) -> Option<Value> {
    opt.as_ref().and_then(|c| serde_json::to_value(c).ok())
}

fn merge_validation_errors(
    mut response: PipelineValidationResponse,
    extra: Vec<String>,
) -> PipelineValidationResponse {
    if !extra.is_empty() {
        response.valid = false;
        response.errors.extend(extra);
    }
    response
}

pub async fn create_pipeline(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreatePipelineRequest>,
) -> impl IntoResponse {
    let CreatePipelineRequest {
        name,
        description,
        status,
        nodes,
        schedule_config,
        retry_policy,
        seed_dataset_rid,
        inputs,
        pipeline_type: pipeline_type_raw,
        external,
        incremental,
        streaming,
        compute_profile_id,
        project_id,
    } = body;
    let id = Uuid::now_v7();
    let description = description.unwrap_or_default();
    let status = status.unwrap_or_else(|| "draft".to_string());
    let schedule_config = schedule_config.unwrap_or_default();
    let retry_policy = retry_policy.unwrap_or_default();
    let pipeline_kind = resolve_pipeline_type(pipeline_type_raw.as_deref());
    // FASE 1 — every fresh pipeline starts in DRAFT lifecycle. Promotion
    // to VALIDATED / DEPLOYED happens via update_pipeline + the FSM.
    let lifecycle = PipelineLifecycle::Draft;

    // P5 — when no DAG was provided, synthesise a passthrough input
    // node from `seed_dataset_rid` / `inputs`. Mirrors Foundry's
    // "Open in Pipeline Builder" entry point: the user lands on a
    // pipeline already wired to read from the upstream dataset.
    let nodes = if nodes.is_empty() {
        synthesize_seed_nodes(seed_dataset_rid.as_deref(), &inputs)
    } else {
        nodes
    };

    let validation = compiler::validate_definition(&status, &schedule_config, &nodes);
    let coherence_errors = pipeline_type::validate_pipeline_type_coherence(
        pipeline_kind,
        external.as_ref(),
        incremental.as_ref(),
        streaming.as_ref(),
    );
    let validation = merge_validation_errors(validation, coherence_errors);
    if !validation.valid {
        return (StatusCode::BAD_REQUEST, Json(validation)).into_response();
    }

    let dag = serde_json::to_value(&nodes).unwrap_or_default();
    let next_run_at = executor::compute_next_run_at_from_parts(&status, &schedule_config);
    let external_value: Option<Value> = config_to_value(&external);
    let incremental_value: Option<Value> = config_to_value(&incremental);
    let streaming_value: Option<Value> = config_to_value(&streaming);

    let result = sqlx::query_as::<_, Pipeline>(
        r#"INSERT INTO pipelines (
               id, name, description, owner_id, dag, status, schedule_config, retry_policy, next_run_at,
               pipeline_type, lifecycle, external_config, incremental_config, streaming_config,
               compute_profile_id, project_id
           )
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
           RETURNING *"#,
    )
    .bind(id)
    .bind(&name)
    .bind(&description)
    .bind(claims.sub)
    .bind(&dag)
    .bind(&status)
    .bind(serde_json::to_value(&schedule_config).unwrap_or_else(|_| json!({})))
    .bind(serde_json::to_value(&retry_policy).unwrap_or_else(|_| json!({})))
    .bind(next_run_at)
    .bind(pipeline_kind.as_str())
    .bind(lifecycle.as_str())
    .bind(external_value)
    .bind(incremental_value)
    .bind(streaming_value)
    .bind(&compute_profile_id)
    .bind(project_id)
    .fetch_one(&state.db)
    .await;

    match result {
        Ok(p) => (StatusCode::CREATED, Json(serde_json::json!(p))).into_response(),
        Err(e) => {
            tracing::error!("create pipeline: {e}");
            (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response()
        }
    }
}

pub async fn list_pipelines(
    _user: AuthUser,
    State(state): State<AppState>,
    Query(params): Query<ListPipelinesQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let search = params.search.unwrap_or_default();
    let pattern = format!("%{search}%");

    let total: i64 = sqlx::query_scalar(
        r#"SELECT COUNT(*) FROM pipelines
           WHERE name ILIKE $1
             AND ($2::TEXT IS NULL OR status = $2)"#,
    )
    .bind(&pattern)
    .bind(&params.status)
    .fetch_one(&state.db)
    .await
    .unwrap_or(0);

    let pipelines = sqlx::query_as::<_, Pipeline>(
        r#"SELECT * FROM pipelines
           WHERE name ILIKE $1
             AND ($2::TEXT IS NULL OR status = $2)
           ORDER BY created_at DESC LIMIT $3 OFFSET $4"#,
    )
    .bind(&pattern)
    .bind(&params.status)
    .bind(per_page)
    .bind(offset)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    Json(ListPipelinesResponse {
        data: pipelines,
        total,
        page,
        per_page,
    })
}

pub async fn get_pipeline(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, Pipeline>("SELECT * FROM pipelines WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(p)) => Json(serde_json::json!(p)).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn update_pipeline(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdatePipelineRequest>,
) -> impl IntoResponse {
    let existing = match sqlx::query_as::<_, Pipeline>("SELECT * FROM pipelines WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(pipeline)) => pipeline,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            return (StatusCode::INTERNAL_SERVER_ERROR, error.to_string()).into_response();
        }
    };

    let existing_name = existing.name.clone();
    let existing_description = existing.description.clone();
    let existing_status = existing.status.clone();
    let existing_dag = existing.dag.clone();
    let existing_schedule = existing.schedule();
    let existing_retry_policy = existing.parsed_retry_policy();
    let existing_kind = existing.pipeline_kind();
    let existing_lifecycle = existing.lifecycle_state();
    let existing_external = existing.external_config_typed();
    let existing_incremental = existing.incremental_config_typed();
    let existing_streaming = existing.streaming_config_typed();
    let existing_compute_profile = existing.compute_profile_id.clone();
    let existing_project_id = existing.project_id;

    let name = body.name.unwrap_or(existing_name);
    let description = body.description.unwrap_or(existing_description);
    let status = body.status.unwrap_or(existing_status);
    let nodes = match body.nodes {
        Some(nodes) => nodes,
        None => match existing.parsed_nodes() {
            Ok(nodes) => nodes,
            Err(error) => {
                return (StatusCode::INTERNAL_SERVER_ERROR, error).into_response();
            }
        },
    };
    let schedule_config = body.schedule_config.unwrap_or(existing_schedule);
    let retry_policy = body.retry_policy.unwrap_or(existing_retry_policy);
    let pipeline_kind = body
        .pipeline_type
        .as_deref()
        .and_then(PipelineType::parse)
        .unwrap_or(existing_kind);
    let external: Option<ExternalConfig> = body.external.or(existing_external);
    let incremental: Option<IncrementalConfig> = body.incremental.or(existing_incremental);
    let streaming: Option<StreamingConfig> = body.streaming.or(existing_streaming);
    let compute_profile_id = body.compute_profile_id.or(existing_compute_profile);
    let project_id = body.project_id.or(existing_project_id);

    let lifecycle = if let Some(raw) = body.lifecycle.as_deref() {
        let target = match PipelineLifecycle::parse(raw) {
            Ok(t) => t,
            Err(e) => return (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
        };
        match existing_lifecycle.transition(target) {
            Ok(t) => t,
            Err(e) => return (StatusCode::CONFLICT, e.to_string()).into_response(),
        }
    } else {
        existing_lifecycle
    };

    let validation = compiler::validate_definition(&status, &schedule_config, &nodes);
    let coherence_errors = pipeline_type::validate_pipeline_type_coherence(
        pipeline_kind,
        external.as_ref(),
        incremental.as_ref(),
        streaming.as_ref(),
    );
    let validation = merge_validation_errors(validation, coherence_errors);
    if !validation.valid {
        return (StatusCode::BAD_REQUEST, Json(validation)).into_response();
    }

    let dag = serde_json::to_value(&nodes).unwrap_or(existing_dag);
    let next_run_at = executor::compute_next_run_at_from_parts(&status, &schedule_config);
    let external_value: Option<Value> = config_to_value(&external);
    let incremental_value: Option<Value> = config_to_value(&incremental);
    let streaming_value: Option<Value> = config_to_value(&streaming);

    let result = sqlx::query_as::<_, Pipeline>(
        r#"UPDATE pipelines SET
           name = $2,
           description = $3,
           dag = $4,
           status = $5,
           schedule_config = $6,
           retry_policy = $7,
           next_run_at = $8,
           pipeline_type = $9,
           lifecycle = $10,
           external_config = $11,
           incremental_config = $12,
           streaming_config = $13,
           compute_profile_id = $14,
           project_id = $15,
           updated_at = NOW()
           WHERE id = $1
           RETURNING *"#,
    )
    .bind(id)
    .bind(&name)
    .bind(&description)
    .bind(&dag)
    .bind(&status)
    .bind(serde_json::to_value(&schedule_config).unwrap_or_else(|_| json!({})))
    .bind(serde_json::to_value(&retry_policy).unwrap_or_else(|_| json!({})))
    .bind(next_run_at)
    .bind(pipeline_kind.as_str())
    .bind(lifecycle.as_str())
    .bind(external_value)
    .bind(incremental_value)
    .bind(streaming_value)
    .bind(&compute_profile_id)
    .bind(project_id)
    .fetch_optional(&state.db)
    .await;

    match result {
        Ok(Some(p)) => Json(serde_json::json!(p)).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

/// P5 — build seed input nodes when the caller leaves `nodes` empty.
/// One passthrough node per `inputs[]` entry; if both lists are
/// non-empty the explicit `inputs` win. The dataset RID is stored
/// under `config.seed_dataset_rid` so a later resolution step (or a
/// catalog lookup) can replace it with the canonical UUID.
fn synthesize_seed_nodes(
    seed_dataset_rid: Option<&str>,
    inputs: &[crate::models::pipeline::PipelineInputSeed],
) -> Vec<crate::models::pipeline::PipelineNode> {
    use crate::models::pipeline::PipelineNode;
    let mut nodes = Vec::new();
    let combined: Vec<(String, Option<String>)> = if !inputs.is_empty() {
        inputs
            .iter()
            .map(|i| (i.dataset_rid.clone(), i.label.clone()))
            .collect()
    } else if let Some(rid) = seed_dataset_rid {
        vec![(rid.to_string(), None)]
    } else {
        Vec::new()
    };

    for (i, (rid, label)) in combined.into_iter().enumerate() {
        let node_id = format!("input_{i}");
        let label = label.unwrap_or_else(|| format!("Dataset input {}", i + 1));
        nodes.push(PipelineNode {
            id: node_id,
            label,
            transform_type: "passthrough".to_string(),
            config: serde_json::json!({
                "seed_dataset_rid": rid,
            }),
            depends_on: Vec::new(),
            input_dataset_ids: Vec::new(),
            output_dataset_id: None,
            ..PipelineNode::default()
        });
    }
    nodes
}

pub async fn delete_pipeline(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query("DELETE FROM pipelines WHERE id = $1")
        .bind(id)
        .execute(&state.db)
        .await
    {
        Ok(r) if r.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => StatusCode::NOT_FOUND.into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}
