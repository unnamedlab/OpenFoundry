//! TASK G — Webhooks invocation surface for `connector-management-service`.
//!
//! Exposes `POST /api/v1/webhooks/{id}/invoke`, which the ontology-actions
//! kernel calls for both writeback and post-rules side effects. The webhook
//! definitions themselves are stored in the `data_connections` table with
//! `source_type = 'webhook'` (rows include `url`, `method`, `headers`,
//! `input_schema`, `output_schema`, `auth_ref` in the JSONB `config`
//! column).
//!
//! This handler is intentionally minimal — it loads the webhook config,
//! issues the upstream HTTP call, and returns the response payload + a
//! best-effort `output_parameters` mapping so the kernel can expose the
//! captured values to subsequent rules.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use reqwest::{Method, Url};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value, json};
use sqlx::Row;
use std::collections::HashMap;
use uuid::Uuid;

use crate::AppState;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WebhookDefinition {
    pub url: String,
    #[serde(default = "default_method")]
    pub method: String,
    #[serde(default)]
    pub headers: HashMap<String, String>,
    #[serde(default)]
    pub input_schema: Value,
    #[serde(default)]
    pub output_schema: Value,
    #[serde(default)]
    pub auth_ref: Option<String>,
}

fn default_method() -> String {
    "POST".to_string()
}

#[derive(Debug, Deserialize)]
pub struct InvokeWebhookRequest {
    #[serde(default)]
    pub inputs: Value,
}

#[derive(Debug, Serialize)]
pub struct InvokeWebhookResponse {
    pub status: u16,
    pub response: Value,
    pub output_parameters: Value,
}

pub async fn invoke_webhook(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<InvokeWebhookRequest>,
) -> impl IntoResponse {
    let definition = match load_webhook_definition(&state, id).await {
        Ok(Some(definition)) => definition,
        Ok(None) => {
            return (
                StatusCode::NOT_FOUND,
                Json(json!({ "error": "webhook not found" })),
            )
                .into_response();
        }
        Err(error) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(json!({ "error": format!("failed to load webhook: {error}") })),
            )
                .into_response();
        }
    };

    let parsed_url = match Url::parse(&definition.url) {
        Ok(url) => url,
        Err(error) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(json!({ "error": format!("invalid webhook url: {error}") })),
            )
                .into_response();
        }
    };
    let method = match Method::from_bytes(definition.method.to_uppercase().as_bytes()) {
        Ok(method) => method,
        Err(_) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(json!({ "error": format!("invalid webhook method '{}'", definition.method) })),
            )
                .into_response();
        }
    };

    let mut request = state.http_client.request(method, parsed_url);
    for (header, value) in &definition.headers {
        request = request.header(header, value);
    }
    let response = match request.json(&body.inputs).send().await {
        Ok(response) => response,
        Err(error) => {
            return (
                StatusCode::BAD_GATEWAY,
                Json(json!({ "error": format!("webhook upstream error: {error}") })),
            )
                .into_response();
        }
    };

    let status = response.status();
    let text = response.text().await.unwrap_or_default();
    let response_value: Value = if text.trim().is_empty() {
        Value::Null
    } else {
        serde_json::from_str(&text).unwrap_or(Value::String(text))
    };
    let output_parameters = response_value
        .as_object()
        .and_then(|map| map.get("output_parameters").cloned())
        .unwrap_or(Value::Object(Map::new()));

    Json(InvokeWebhookResponse {
        status: status.as_u16(),
        response: response_value,
        output_parameters,
    })
    .into_response()
}

async fn load_webhook_definition(
    state: &AppState,
    id: Uuid,
) -> Result<Option<WebhookDefinition>, sqlx::Error> {
    let row = sqlx::query(
        r#"SELECT config FROM data_connections
           WHERE id = $1 AND source_type = 'webhook'"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await?;
    let Some(row) = row else { return Ok(None) };
    let config: Value = row.try_get("config")?;
    let definition: WebhookDefinition =
        serde_json::from_value(config).map_err(|error| sqlx::Error::Decode(Box::new(error)))?;
    Ok(Some(definition))
}
