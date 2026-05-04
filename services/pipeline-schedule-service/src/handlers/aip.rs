//! AIP-assisted schedule creation endpoints.
//!
//! ```
//! POST /v1/schedules/aip:generate  { natural_language, project_rid }
//! POST /v1/schedules/aip:explain   { trigger, target }
//! ```
//!
//! Both endpoints delegate to the [`crate::domain::aip`] module. The
//! [`LlmClient`] is injected via Axum `Extension` so production wires
//! a real llm-catalog-service-backed client and tests use a stub.

use std::sync::Arc;

use auth_middleware::layer::AuthUser;
use axum::{
    Extension, Json,
    http::StatusCode,
    response::IntoResponse,
};
use serde::Deserialize;
use serde_json::json;

use crate::domain::aip::{
    AipError, LlmClient, run_explain, run_generate,
};
use crate::domain::metrics;
use crate::domain::trigger::{ScheduleTarget, Trigger};

#[derive(Debug, Deserialize)]
pub struct GenerateBody {
    pub natural_language: String,
    pub project_rid: String,
}

pub async fn generate(
    _user: AuthUser,
    Extension(llm): Extension<Arc<dyn LlmClient>>,
    Json(body): Json<GenerateBody>,
) -> impl IntoResponse {
    metrics::global().aip_generate_total.inc();
    if body.natural_language.trim().is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(json!({"error": "natural_language must not be empty"})),
        )
            .into_response();
    }
    match run_generate(llm.as_ref(), &body.natural_language, &body.project_rid).await {
        Ok(proposal) => Json(proposal).into_response(),
        Err(AipError::LowConfidence(score, floor)) => {
            metrics::global().aip_low_confidence_total.inc();
            (
                StatusCode::UNPROCESSABLE_ENTITY,
                Json(json!({
                    "error": format!(
                        "I couldn't confidently translate that prompt (score {score} < floor {floor}). \
                         Could you clarify the cadence (cron expression or weekday/time) and any event triggers?"
                    ),
                    "confidence": score,
                    "min_confidence": floor,
                })),
            )
                .into_response()
        }
        Err(e) => (
            StatusCode::BAD_GATEWAY,
            Json(json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}

#[derive(Debug, Deserialize)]
pub struct ExplainBody {
    pub trigger: Trigger,
    pub target: ScheduleTarget,
}

pub async fn explain(
    _user: AuthUser,
    Extension(llm): Extension<Arc<dyn LlmClient>>,
    Json(body): Json<ExplainBody>,
) -> impl IntoResponse {
    match run_explain(llm.as_ref(), &body.trigger, &body.target).await {
        Ok(prose) => Json(json!({ "explanation": prose })).into_response(),
        Err(e) => (
            StatusCode::BAD_GATEWAY,
            Json(json!({"error": e.to_string()})),
        )
            .into_response(),
    }
}
