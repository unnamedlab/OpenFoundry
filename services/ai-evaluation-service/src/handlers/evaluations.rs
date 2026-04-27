use std::{collections::BTreeSet, time::Instant};

use axum::{
    Json,
    extract::State,
};
use chrono::Utc;
use serde_json::{Value, json};
use sqlx::{query_as, types::Json as SqlJson};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{evaluation, llm},
    models::{
        conversation::{
            ChatAttachment, EvaluateGuardrailsRequest, EvaluateGuardrailsResponse,
            GuardrailVerdict, LlmUsageSummary, ProviderBenchmarkRequest,
            ProviderBenchmarkResponse, ProviderBenchmarkResult, ProviderBenchmarkScore,
        },
        provider::{LlmProvider, ProviderRow},
    },
};

use super::{ServiceResult, bad_request, db_error, not_found};

async fn load_provider_rows(db: &sqlx::PgPool) -> Result<Vec<ProviderRow>, sqlx::Error> {
    query_as::<_, ProviderRow>(
        r#"
        SELECT
            id,
            name,
            provider_type,
            model_name,
            endpoint_url,
            api_mode,
            credential_reference,
            enabled,
            load_balance_weight,
            max_output_tokens,
            cost_tier,
            tags,
            route_rules,
            health_state,
            created_at,
            updated_at
        FROM ai_providers
        ORDER BY updated_at DESC, created_at DESC
        "#,
    )
    .fetch_all(db)
    .await
}

fn summarize_title(content: &str) -> String {
    let mut chars = content.trim().chars();
    let title = chars.by_ref().take(60).collect::<String>();
    if chars.next().is_some() {
        format!("{title}...")
    } else if title.is_empty() {
        "New benchmark".to_string()
    } else {
        title
    }
}

fn preview_text(content: &str, limit: usize) -> String {
    let mut chars = content.trim().chars();
    let preview = chars.by_ref().take(limit).collect::<String>();
    if chars.next().is_some() {
        format!("{preview}...")
    } else {
        preview
    }
}

fn attachment_context(attachments: &[ChatAttachment]) -> String {
    if attachments.is_empty() {
        return "none".to_string();
    }

    attachments
        .iter()
        .map(|attachment| {
            let label = attachment
                .name
                .as_deref()
                .filter(|value| !value.trim().is_empty())
                .unwrap_or("attachment");
            match attachment.kind.as_str() {
                "image_url" => format!(
                    "- {label}: image url {}",
                    attachment.url.as_deref().unwrap_or("missing-url")
                ),
                "image_base64" => format!(
                    "- {label}: embedded {} image",
                    attachment.mime_type.as_deref().unwrap_or("unknown")
                ),
                _ => format!(
                    "- {label}: {}",
                    attachment.text.as_deref().unwrap_or("text attachment")
                ),
            }
        })
        .collect::<Vec<_>>()
        .join("\n")
}

fn required_modalities(attachments: &[ChatAttachment]) -> Vec<String> {
    let mut modalities = vec!["text".to_string()];
    if attachments
        .iter()
        .any(|attachment| attachment.kind.starts_with("image"))
    {
        modalities.push("image".to_string());
    }
    modalities
}

fn modality_label(required_modalities: &[String]) -> &'static str {
    if required_modalities
        .iter()
        .any(|modality| modality.eq_ignore_ascii_case("image"))
    {
        "image+text"
    } else {
        "text"
    }
}

fn privacy_reason(guardrail: &GuardrailVerdict, require_private_network: bool) -> Option<String> {
    if require_private_network {
        Some("private network explicitly requested".to_string())
    } else if guardrail
        .flags
        .iter()
        .any(|flag| flag.kind.starts_with("pii_"))
    {
        Some("PII detected in prompt, preferring private-network providers".to_string())
    } else {
        None
    }
}

fn usage_summary(
    provider: &LlmProvider,
    prompt_tokens: i32,
    completion_tokens: i32,
    latency_ms: i32,
    cache_hit: bool,
) -> LlmUsageSummary {
    let total_tokens = prompt_tokens.max(0) + completion_tokens.max(0);

    LlmUsageSummary {
        prompt_tokens,
        completion_tokens,
        total_tokens,
        estimated_cost_usd: evaluation::estimated_cost_usd(
            provider,
            prompt_tokens,
            completion_tokens,
            cache_hit,
        ),
        latency_ms,
        network_scope: provider.route_rules.network_scope.clone(),
        cache_hit,
    }
}

async fn record_usage_event(
    db: &sqlx::PgPool,
    provider_id: Uuid,
    request_kind: &str,
    use_case: &str,
    modality: &str,
    usage: &LlmUsageSummary,
    benchmark_group_id: Option<Uuid>,
    metadata: Value,
) -> Result<(), sqlx::Error> {
    sqlx::query(
        r#"
        INSERT INTO ai_llm_usage_events (
            id,
            provider_id,
            conversation_id,
            request_kind,
            use_case,
            network_scope,
            modality,
            cache_hit,
            prompt_tokens,
            completion_tokens,
            total_tokens,
            estimated_cost_usd,
            latency_ms,
            benchmark_group_id,
            metadata
        )
        VALUES ($1, $2, NULL, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
        "#,
    )
    .bind(Uuid::now_v7())
    .bind(provider_id)
    .bind(request_kind)
    .bind(use_case)
    .bind(&usage.network_scope)
    .bind(modality)
    .bind(usage.cache_hit)
    .bind(usage.prompt_tokens)
    .bind(usage.completion_tokens)
    .bind(usage.total_tokens)
    .bind(usage.estimated_cost_usd as f64)
    .bind(usage.latency_ms)
    .bind(benchmark_group_id)
    .bind(SqlJson(metadata))
    .execute(db)
    .await?;

    Ok(())
}

pub async fn benchmark_providers(
    State(state): State<AppState>,
    Json(body): Json<ProviderBenchmarkRequest>,
) -> ServiceResult<ProviderBenchmarkResponse> {
    if body.prompt.trim().is_empty() {
        return Err(bad_request("benchmark prompt is required"));
    }

    let prompt_guardrail = llm::guardrails::evaluate_text(&body.prompt);
    if prompt_guardrail.blocked {
        return Err(bad_request(
            "benchmark prompt is blocked by guardrails; sanitize it before benchmarking",
        ));
    }

    let providers = load_provider_rows(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?
        .into_iter()
        .map(Into::into)
        .collect::<Vec<LlmProvider>>();
    if providers.is_empty() {
        return Err(not_found("no AI providers configured"));
    }

    let candidate_providers = if body.provider_ids.is_empty() {
        providers.clone()
    } else {
        providers
            .iter()
            .filter(|provider| body.provider_ids.contains(&provider.id))
            .cloned()
            .collect::<Vec<_>>()
    };
    if candidate_providers.is_empty() {
        return Err(not_found(
            "no benchmark providers matched the requested ids",
        ));
    }

    let required_modalities = required_modalities(&body.attachments);
    let privacy_reason = privacy_reason(&prompt_guardrail, body.require_private_network);
    let routed = llm::gateway::route_providers(
        &candidate_providers,
        None,
        &body.use_case,
        &required_modalities,
        body.require_private_network,
        privacy_reason.is_some(),
    );
    if body.require_private_network && routed.is_empty() {
        return Err(bad_request(
            "no private-network AI provider is configured for this benchmark",
        ));
    }
    if routed.is_empty() {
        return Err(not_found("no eligible providers support this benchmark"));
    }

    let benchmark_group_id = Uuid::now_v7();
    let system_prompt = body.system_prompt.unwrap_or_else(|| {
        "You are an enterprise AI benchmark harness. Answer the user prompt clearly and concretely."
            .to_string()
    });
    let prompt_used = format!(
        "{}\n\nUser request: {}\n\nAttachments:\n{}",
        system_prompt,
        prompt_guardrail.redacted_text,
        attachment_context(&body.attachments)
    );
    let mut results = Vec::new();

    for provider in routed {
        let started_at = Instant::now();
        let completion = llm::runtime::complete_text(
            &state.http_client,
            &provider,
            &system_prompt,
            &body.prompt,
            &body.attachments,
            0.2,
            body.max_tokens,
        )
        .await;
        let latency_ms = started_at.elapsed().as_millis().min(i32::MAX as u128) as i32;

        match completion {
            Ok(completion) => {
                let prompt_tokens = completion
                    .prompt_tokens
                    .max(llm::gateway::estimate_tokens(&prompt_used));
                let completion_tokens = completion
                    .completion_tokens
                    .max(llm::gateway::estimate_tokens(&completion.text));
                let mut usage = usage_summary(
                    &provider,
                    prompt_tokens,
                    completion_tokens,
                    latency_ms,
                    false,
                );
                usage.total_tokens = completion
                    .total_tokens
                    .max(prompt_tokens + completion_tokens);
                let reply_guardrail = llm::guardrails::evaluate_text(&completion.text);
                record_usage_event(
                    &state.db,
                    provider.id,
                    "benchmark",
                    &body.use_case,
                    modality_label(&required_modalities),
                    &usage,
                    Some(benchmark_group_id),
                    json!({
                        "rubric_keywords": body.rubric_keywords,
                        "provider_name": provider.name,
                    }),
                )
                .await
                .map_err(|cause| db_error(&cause))?;

                results.push(ProviderBenchmarkResult {
                    provider_id: provider.id,
                    provider_name: provider.name,
                    network_scope: usage.network_scope.clone(),
                    reply_preview: preview_text(&completion.text, 280),
                    prompt_tokens: usage.prompt_tokens,
                    completion_tokens: usage.completion_tokens,
                    total_tokens: usage.total_tokens,
                    estimated_cost_usd: usage.estimated_cost_usd,
                    latency_ms: usage.latency_ms,
                    cache_hit: false,
                    guardrail: reply_guardrail,
                    score: ProviderBenchmarkScore {
                        quality: 0.0,
                        latency: 0.0,
                        cost: 0.0,
                        safety: 0.0,
                        overall: 0.0,
                    },
                    error: None,
                });
            }
            Err(error) => {
                results.push(ProviderBenchmarkResult {
                    provider_id: provider.id,
                    provider_name: provider.name,
                    network_scope: provider.route_rules.network_scope.clone(),
                    reply_preview: String::new(),
                    prompt_tokens: 0,
                    completion_tokens: 0,
                    total_tokens: 0,
                    estimated_cost_usd: 0.0,
                    latency_ms,
                    cache_hit: false,
                    guardrail: GuardrailVerdict::default(),
                    score: ProviderBenchmarkScore {
                        quality: 0.0,
                        latency: 0.0,
                        cost: 0.0,
                        safety: 0.0,
                        overall: 0.0,
                    },
                    error: Some(error),
                });
            }
        }
    }

    let successful = results
        .iter()
        .filter(|result| result.error.is_none())
        .collect::<Vec<_>>();
    let min_latency = successful
        .iter()
        .map(|result| result.latency_ms as f32)
        .fold(f32::INFINITY, f32::min);
    let max_latency = successful
        .iter()
        .map(|result| result.latency_ms as f32)
        .fold(0.0, f32::max);
    let min_cost = successful
        .iter()
        .map(|result| result.estimated_cost_usd)
        .fold(f32::INFINITY, f32::min);
    let max_cost = successful
        .iter()
        .map(|result| result.estimated_cost_usd)
        .fold(0.0, f32::max);

    for result in &mut results {
        if result.error.is_some() {
            continue;
        }

        let quality = evaluation::quality_score(&result.reply_preview, &body.rubric_keywords);
        let safety = evaluation::safety_score(&result.guardrail);
        let latency =
            evaluation::normalized_score(result.latency_ms as f32, min_latency, max_latency, true);
        let cost =
            evaluation::normalized_score(result.estimated_cost_usd, min_cost, max_cost, true);
        result.score = ProviderBenchmarkScore {
            quality,
            latency,
            cost,
            safety,
            overall: evaluation::overall_benchmark_score(quality, safety, latency, cost),
        };
    }

    results.sort_by(|left, right| right.score.overall.total_cmp(&left.score.overall));
    let recommended_provider_id = results
        .iter()
        .find(|result| result.error.is_none())
        .map(|result| result.provider_id);

    Ok(Json(ProviderBenchmarkResponse {
        benchmark_group_id,
        use_case: body.use_case,
        prompt_excerpt: summarize_title(&body.prompt),
        required_modalities,
        requested_private_network: body.require_private_network,
        recommended_provider_id,
        results,
        created_at: Utc::now(),
    }))
}

pub async fn evaluate_guardrails(
    State(_state): State<AppState>,
    Json(body): Json<EvaluateGuardrailsRequest>,
) -> ServiceResult<EvaluateGuardrailsResponse> {
    if body.content.trim().is_empty() {
        return Err(bad_request("guardrail evaluation requires content"));
    }

    let verdict = llm::guardrails::evaluate_text(&body.content);
    let mut recommendations = BTreeSet::new();

    if verdict.blocked {
        recommendations
            .insert("Remove prompt-injection or toxic content before retrying.".to_string());
    }
    for flag in &verdict.flags {
        if flag.kind.starts_with("pii_") {
            recommendations
                .insert("Redact PII before routing prompts to external LLM providers.".to_string());
        }
    }
    if recommendations.is_empty() {
        recommendations
            .insert("No blocking issues detected; response is safe to continue.".to_string());
    }

    Ok(Json(EvaluateGuardrailsResponse {
        verdict: verdict.clone(),
        risk_score: evaluation::risk_score(&verdict),
        recommendations: recommendations.into_iter().collect(),
    }))
}
