use axum::{
    Json,
    extract::{Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    models::{
        checkpoint::{
            CheckpointEvaluation, CheckpointPolicy, CreatePolicyRequest, EvaluateCheckpointRequest,
            PolicyRow, SensitiveConfigRow, SensitiveInteractionConfig,
        },
        records::{ListRecordsQuery, PurposeRecord, PurposeRecordRow, PurposeTemplate},
    },
};

pub async fn list_policies(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, PolicyRow>(
        "SELECT slug, name, interaction_type, sensitivity, enforcement_mode, prompts, rules, created_at, updated_at
         FROM checkpoint_policies
         ORDER BY interaction_type, slug",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => {
            let mut data = Vec::new();
            for row in rows {
                match CheckpointPolicy::try_from(row) {
                    Ok(policy) => data.push(policy),
                    Err(error) => {
                        tracing::error!("policy decode failed: {error}");
                        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
                    }
                }
            }
            Json(json!({ "data": data })).into_response()
        }
        Err(error) => {
            tracing::error!("list checkpoint policies failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn create_policy(
    State(state): State<AppState>,
    Json(body): Json<CreatePolicyRequest>,
) -> impl IntoResponse {
    let prompts = match serde_json::to_value(&body.prompts) {
        Ok(value) => value,
        Err(error) => {
            tracing::error!("serialize policy prompts failed: {error}");
            return StatusCode::BAD_REQUEST.into_response();
        }
    };
    let rules = match serde_json::to_value(&body.rules) {
        Ok(value) => value,
        Err(error) => {
            tracing::error!("serialize policy rules failed: {error}");
            return StatusCode::BAD_REQUEST.into_response();
        }
    };

    match sqlx::query(
        "INSERT INTO checkpoint_policies (slug, name, interaction_type, sensitivity, enforcement_mode, prompts, rules)
         VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb)
         ON CONFLICT (slug) DO UPDATE
         SET name = EXCLUDED.name,
             interaction_type = EXCLUDED.interaction_type,
             sensitivity = EXCLUDED.sensitivity,
             enforcement_mode = EXCLUDED.enforcement_mode,
             prompts = EXCLUDED.prompts,
             rules = EXCLUDED.rules,
             updated_at = NOW()",
    )
    .bind(&body.slug)
    .bind(&body.name)
    .bind(&body.interaction_type)
    .bind(match body.sensitivity {
        crate::models::checkpoint::InteractionSensitivity::Critical => "critical",
        crate::models::checkpoint::InteractionSensitivity::High => "high",
        crate::models::checkpoint::InteractionSensitivity::Normal => "normal",
    })
    .bind(&body.enforcement_mode)
    .bind(prompts)
    .bind(rules)
    .execute(&state.db)
    .await
    {
        Ok(_) => StatusCode::CREATED.into_response(),
        Err(error) => {
            tracing::error!("upsert checkpoint policy failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn list_sensitive_configs(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, SensitiveConfigRow>(
        "SELECT interaction_type, sensitivity, require_purpose_justification, require_auditable_record, linked_policy_slug
         FROM sensitive_interaction_configs
         ORDER BY interaction_type",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(json!({
            "data": rows.into_iter().map(SensitiveInteractionConfig::from).collect::<Vec<_>>()
        }))
        .into_response(),
        Err(error) => {
            tracing::error!("list sensitive configs failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn list_templates(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, (String, String, String, serde_json::Value, serde_json::Value)>(
        "SELECT slug, name, summary, prompts, required_tags
         FROM purpose_templates
         ORDER BY slug",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => {
            let data = rows
                .into_iter()
                .map(
                    |(slug, name, summary, prompts, required_tags)| PurposeTemplate {
                        slug,
                        name,
                        summary,
                        prompts: serde_json::from_value(prompts).unwrap_or_default(),
                        required_tags: serde_json::from_value(required_tags).unwrap_or_default(),
                    },
                )
                .collect::<Vec<_>>();
            Json(json!({ "data": data })).into_response()
        }
        Err(error) => {
            tracing::error!("list purpose templates failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn evaluate_checkpoint(
    State(state): State<AppState>,
    Json(body): Json<EvaluateCheckpointRequest>,
) -> impl IntoResponse {
    evaluate_and_record(&state, body).await
}

pub async fn evaluate_checkpoint_internal(
    State(state): State<AppState>,
    Json(body): Json<EvaluateCheckpointRequest>,
) -> impl IntoResponse {
    evaluate_and_record(&state, body).await
}

async fn evaluate_and_record(
    state: &AppState,
    body: EvaluateCheckpointRequest,
) -> axum::response::Response {
    let config = match sqlx::query_as::<_, SensitiveConfigRow>(
        "SELECT interaction_type, sensitivity, require_purpose_justification, require_auditable_record, linked_policy_slug
         FROM sensitive_interaction_configs
         WHERE interaction_type = $1",
    )
    .bind(&body.interaction_type)
    .fetch_optional(&state.db)
    .await
    {
        Ok(row) => row.map(SensitiveInteractionConfig::from),
        Err(error) => {
            tracing::error!("load sensitive config failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let policy = if let Some(config) = config.as_ref() {
        if let Some(policy_slug) = config.linked_policy_slug.as_ref() {
            match sqlx::query_as::<_, PolicyRow>(
                "SELECT slug, name, interaction_type, sensitivity, enforcement_mode, prompts, rules, created_at, updated_at
                 FROM checkpoint_policies WHERE slug = $1",
            )
            .bind(policy_slug)
            .fetch_optional(&state.db)
            .await
            {
                Ok(Some(row)) => CheckpointPolicy::try_from(row).ok(),
                Ok(None) => None,
                Err(error) => {
                    tracing::error!("load checkpoint policy failed: {error}");
                    return StatusCode::INTERNAL_SERVER_ERROR.into_response();
                }
            }
        } else {
            None
        }
    } else {
        None
    };

    let justification = body
        .purpose_justification
        .as_ref()
        .map(|value| value.trim())
        .filter(|value| !value.is_empty())
        .map(str::to_string);
    let justification_ok = justification
        .as_ref()
        .map(|value| value.len() >= 20)
        .unwrap_or(false);

    let requires_gate = config
        .as_ref()
        .map(|cfg| cfg.require_purpose_justification)
        .unwrap_or(false)
        || body.requested_private_network
        || body.requires_approval;
    let approved = !requires_gate || justification_ok;
    let status = if approved {
        "approved"
    } else {
        "pending_justification"
    };
    let reason = if approved {
        None
    } else {
        Some("purpose justification is required for this sensitive interaction".to_string())
    };

    let record_id = Uuid::now_v7();
    let tags = match serde_json::to_value(&body.tags) {
        Ok(value) => value,
        Err(error) => {
            tracing::error!("serialize checkpoint tags failed: {error}");
            return StatusCode::BAD_REQUEST.into_response();
        }
    };
    let evidence = json!({
        "requested_private_network": body.requested_private_network,
        "requires_approval": body.requires_approval,
        "submitted_evidence": body.evidence,
    });

    if let Err(error) = sqlx::query(
        "INSERT INTO purpose_records (id, interaction_type, actor_id, purpose_justification, status, policy_slug, tags, evidence, created_at)
         VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8::jsonb, $9)",
    )
    .bind(record_id)
    .bind(&body.interaction_type)
    .bind(body.actor_id)
    .bind(&justification)
    .bind(status)
    .bind(policy.as_ref().map(|value| value.slug.as_str()))
    .bind(tags)
    .bind(evidence)
    .bind(Utc::now())
    .execute(&state.db)
    .await
    {
        tracing::error!("insert purpose record failed: {error}");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    Json(CheckpointEvaluation {
        record_id,
        approved,
        status: status.to_string(),
        required_prompts: policy.map(|value| value.prompts).unwrap_or_default(),
        policy_slug: config.and_then(|value| value.linked_policy_slug),
        reason,
    })
    .into_response()
}

pub async fn list_records(
    State(state): State<AppState>,
    Query(params): Query<ListRecordsQuery>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, PurposeRecordRow>(
        "SELECT id, interaction_type, actor_id, purpose_justification, status, policy_slug, tags, evidence, created_at
         FROM purpose_records
         WHERE ($1::TEXT IS NULL OR interaction_type = $1)
           AND ($2::UUID IS NULL OR actor_id = $2)
         ORDER BY created_at DESC",
    )
    .bind(&params.interaction_type)
    .bind(params.actor_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => {
            let mut data = Vec::new();
            for row in rows {
                match PurposeRecord::try_from(row) {
                    Ok(record) => data.push(record),
                    Err(error) => {
                        tracing::error!("decode purpose record failed: {error}");
                        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
                    }
                }
            }
            Json(json!({ "data": data })).into_response()
        }
        Err(error) => {
            tracing::error!("list purpose records failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}
