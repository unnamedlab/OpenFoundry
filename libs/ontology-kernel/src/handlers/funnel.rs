use auth_middleware::{
    claims::Claims,
    jwt::{build_access_claims, encode_token},
    layer::AuthUser,
};
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use chrono::{Duration, TimeZone, Utc};
use serde::Deserialize;
use serde_json::{Map, Value, json};
use storage_abstraction::repositories::{ObjectId, ReadConsistency};
use uuid::Uuid;

use crate::{
    AppState,
    domain::{
        access::validate_marking,
        definition_queries,
        funnel_repository::{
            self, CompleteRunInput, CreateRunInput, CreateSourceInput, HealthSourcesParams,
            ListSourcesParams, UpdateSourceInput,
        },
        schema::{load_effective_properties, validate_object_properties},
    },
    handlers::objects::{
        ObjectInstance, append_object_revision, apply_object_write, find_object_id_by_property,
        tenant_from_claims, value_as_store_text,
    },
    models::{
        funnel::{
            CreateOntologyFunnelSourceRequest, GetOntologyFunnelSourceHealthQuery,
            ListOntologyFunnelHealthQuery, ListOntologyFunnelRunsQuery,
            ListOntologyFunnelRunsResponse, ListOntologyFunnelSourcesQuery,
            ListOntologyFunnelSourcesResponse, OntologyFunnelHealthMetricsRow,
            OntologyFunnelHealthResponse, OntologyFunnelPropertyMapping, OntologyFunnelSource,
            OntologyFunnelSourceHealth, OntologyFunnelSourceHealthResponse,
            TriggerOntologyFunnelRunRequest, UpdateOntologyFunnelSourceRequest,
            normalize_default_marking, normalize_funnel_status, normalize_preview_limit,
            normalize_stale_after_hours,
        },
        object_type::ObjectType,
    },
};

#[derive(Debug, Deserialize)]
struct PipelineRunSummary {
    id: Uuid,
    status: String,
    error_message: Option<String>,
}

#[derive(Debug, Deserialize)]
struct DatasetPreviewPayload {
    total_rows: i64,
    rows: Vec<Value>,
    warnings: Vec<String>,
    errors: Vec<String>,
}

struct FunnelExecutionOutcome {
    rows_read: i32,
    inserted_count: i32,
    updated_count: i32,
    skipped_count: i32,
    error_count: i32,
    details: Value,
    error_message: Option<String>,
    pipeline_run_id: Option<Uuid>,
    status: String,
}

fn invalid(message: impl Into<String>) -> Response {
    (
        StatusCode::BAD_REQUEST,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn forbidden(message: impl Into<String>) -> Response {
    (
        StatusCode::FORBIDDEN,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn db_error(message: impl Into<String>) -> Response {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn not_found(message: impl Into<String>) -> Response {
    (
        StatusCode::NOT_FOUND,
        Json(json!({ "error": message.into() })),
    )
        .into_response()
}

fn ensure_owner_or_admin(owner_id: Uuid, claims: &Claims) -> Result<(), String> {
    if claims.has_role("admin") || owner_id == claims.sub {
        Ok(())
    } else {
        Err("forbidden: only the owner can manage this ontology funnel source".to_string())
    }
}

fn validate_source_status(status: &str) -> Result<(), String> {
    if matches!(status.trim(), "active" | "paused") {
        Ok(())
    } else {
        Err("status must be 'active' or 'paused'".to_string())
    }
}

fn issue_service_token(state: &AppState, claims: &Claims) -> Result<String, String> {
    let service_claims = build_access_claims(
        &state.jwt_config,
        Uuid::now_v7(),
        "ontology-service@internal.openfoundry",
        "ontology-service",
        vec!["admin".to_string()],
        vec!["*:*".to_string()],
        claims.org_id,
        json!({
            "service": "ontology-service",
            "classification_clearance": "pii",
            "impersonated_actor_id": claims.sub,
        }),
        vec!["service".to_string()],
    );
    let token = encode_token(&state.jwt_config, &service_claims)
        .map_err(|error| format!("failed to issue service token: {error}"))?;
    Ok(format!("Bearer {token}"))
}

async fn object_type_exists(state: &AppState, object_type_id: Uuid) -> Result<bool, String> {
    definition_queries::object_type_exists(&state.db, object_type_id)
        .await
        .map_err(|error| format!("failed to validate object type: {error}"))
}

async fn dataset_exists(state: &AppState, dataset_id: Uuid) -> Result<bool, String> {
    funnel_repository::dataset_exists(&state.db, dataset_id)
        .await
        .map_err(|error| format!("failed to validate dataset: {error}"))
}

async fn pipeline_exists(state: &AppState, pipeline_id: Uuid) -> Result<bool, String> {
    funnel_repository::pipeline_exists(&state.db, pipeline_id)
        .await
        .map_err(|error| format!("failed to validate pipeline: {error}"))
}

async fn load_source(state: &AppState, id: Uuid) -> Result<Option<OntologyFunnelSource>, String> {
    funnel_repository::load_source(&state.db, id)
        .await
        .map_err(|error| format!("failed to load ontology funnel source: {error}"))
}

async fn load_object_type(
    state: &AppState,
    object_type_id: Uuid,
) -> Result<Option<ObjectType>, String> {
    definition_queries::load_object_type(&state.db, object_type_id)
        .await
        .map_err(|error| format!("failed to load object type: {error}"))
}

async fn load_funnel_health_metrics(
    state: &AppState,
    claims: &Claims,
    source_id: Uuid,
) -> Result<OntologyFunnelHealthMetricsRow, String> {
    let tenant = tenant_from_claims(claims);
    funnel_repository::load_health_metrics(state.stores.actions.as_ref(), &tenant, source_id)
        .await
        .map_err(|error| format!("failed to load ontology funnel health metrics: {error}"))
}

fn build_source_health(
    source: OntologyFunnelSource,
    metrics: OntologyFunnelHealthMetricsRow,
    stale_after_hours: i64,
) -> OntologyFunnelSourceHealth {
    let success_rate = if metrics.total_runs > 0 {
        metrics.successful_runs as f64 / metrics.total_runs as f64
    } else {
        0.0
    };

    let stale_cutoff = Utc::now() - Duration::hours(stale_after_hours.max(1));
    let (health_status, health_reason) = if source.status == "paused" {
        (
            "paused".to_string(),
            "source is paused and will not ingest new batch updates".to_string(),
        )
    } else if metrics.total_runs == 0 {
        (
            "never_run".to_string(),
            "source has not executed any funnel run yet".to_string(),
        )
    } else if metrics
        .last_run_at
        .is_some_and(|last_run_at| last_run_at < stale_cutoff)
    {
        (
            "stale".to_string(),
            format!(
                "no funnel run has completed within the last {} hour(s)",
                stale_after_hours
            ),
        )
    } else {
        match metrics.latest_run_status.as_deref() {
            Some("failed") => (
                "failing".to_string(),
                "latest funnel run failed before completing".to_string(),
            ),
            Some("completed_with_errors" | "dry_run_with_errors") => (
                "degraded".to_string(),
                "latest funnel run completed with row-level or validation errors".to_string(),
            ),
            Some("running") => (
                "degraded".to_string(),
                "a funnel run is currently in progress".to_string(),
            ),
            Some("completed" | "dry_run") => (
                "healthy".to_string(),
                "latest funnel run completed successfully".to_string(),
            ),
            Some(other) => (
                "degraded".to_string(),
                format!("latest funnel run is in status '{other}'"),
            ),
            None => (
                "never_run".to_string(),
                "source has no observable run history".to_string(),
            ),
        }
    };

    OntologyFunnelSourceHealth {
        source,
        health_status,
        health_reason,
        total_runs: metrics.total_runs,
        successful_runs: metrics.successful_runs,
        failed_runs: metrics.failed_runs,
        warning_runs: metrics.warning_runs,
        success_rate,
        avg_duration_ms: metrics.avg_duration_ms,
        p95_duration_ms: metrics.p95_duration_ms,
        max_duration_ms: metrics.max_duration_ms,
        latest_run_status: metrics.latest_run_status,
        last_run_at: metrics.last_run_at,
        last_success_at: metrics.last_success_at,
        last_failure_at: metrics.last_failure_at,
        last_warning_at: metrics.last_warning_at,
        rows_read: metrics.rows_read,
        inserted_count: metrics.inserted_count,
        updated_count: metrics.updated_count,
        skipped_count: metrics.skipped_count,
        error_count: metrics.error_count,
    }
}

fn funnel_health_sort_rank(status: &str) -> i32 {
    match status {
        "failing" => 0,
        "degraded" => 1,
        "stale" => 2,
        "never_run" => 3,
        "paused" => 4,
        "healthy" => 5,
        _ => 6,
    }
}

fn merge_contexts(base: &Value, override_context: Option<&Value>) -> Value {
    match (
        base.as_object(),
        override_context.and_then(Value::as_object),
    ) {
        (Some(base), Some(override_context)) => {
            let mut merged = base.clone();
            for (key, value) in override_context {
                merged.insert(key.clone(), value.clone());
            }
            Value::Object(merged)
        }
        (Some(base), None) => Value::Object(base.clone()),
        _ => override_context.cloned().unwrap_or_else(|| base.clone()),
    }
}

async fn trigger_pipeline_run(
    state: &AppState,
    claims: &Claims,
    source: &OntologyFunnelSource,
    override_context: Option<&Value>,
) -> Result<Option<PipelineRunSummary>, String> {
    let Some(pipeline_id) = source.pipeline_id else {
        return Ok(None);
    };

    let auth_header = issue_service_token(state, claims)?;
    let url = format!(
        "{}/api/v1/pipelines/{pipeline_id}/run",
        state.pipeline_service_url
    );
    let context = merge_contexts(&source.trigger_context, override_context);
    let payload = json!({
        "skip_unchanged": true,
        "context": {
            "trigger": {
                "type": "ontology-funnel",
                "source_id": source.id,
                "object_type_id": source.object_type_id,
                "dataset_id": source.dataset_id
            },
            "funnel": context
        }
    });

    let response = state
        .http_client
        .post(&url)
        .header(reqwest::header::AUTHORIZATION, auth_header)
        .json(&payload)
        .send()
        .await
        .map_err(|error| format!("failed to trigger funnel pipeline: {error}"))?;
    let status = response.status();
    let body = response
        .text()
        .await
        .map_err(|error| format!("failed to read funnel pipeline response: {error}"))?;
    if !status.is_success() {
        return Err(format!(
            "pipeline trigger failed with HTTP {}: {}",
            status.as_u16(),
            body
        ));
    }

    let run: PipelineRunSummary = serde_json::from_str(&body)
        .map_err(|error| format!("failed to decode pipeline run response: {error}"))?;
    if run.status != "completed" {
        return Err(run
            .error_message
            .clone()
            .unwrap_or_else(|| format!("pipeline run finished with status '{}'", run.status)));
    }

    Ok(Some(run))
}

async fn fetch_dataset_preview(
    state: &AppState,
    claims: &Claims,
    source: &OntologyFunnelSource,
    limit: i32,
    dataset_branch: Option<&str>,
    dataset_version: Option<i32>,
) -> Result<DatasetPreviewPayload, String> {
    let auth_header = issue_service_token(state, claims)?;
    let mut url = reqwest::Url::parse(&format!(
        "{}/api/v1/datasets/{}/preview",
        state.dataset_service_url, source.dataset_id
    ))
    .map_err(|error| format!("failed to build dataset preview URL: {error}"))?;
    {
        let mut query = url.query_pairs_mut();
        query.append_pair("limit", &limit.to_string());
        if let Some(branch) = dataset_branch {
            query.append_pair("branch", branch);
        }
        if let Some(version) = dataset_version {
            query.append_pair("version", &version.to_string());
        }
    }

    let response = state
        .http_client
        .get(url)
        .header(reqwest::header::AUTHORIZATION, auth_header)
        .send()
        .await
        .map_err(|error| format!("failed to fetch dataset preview: {error}"))?;
    let status = response.status();
    let body = response
        .text()
        .await
        .map_err(|error| format!("failed to read dataset preview response: {error}"))?;
    if !status.is_success() {
        return Err(format!(
            "dataset preview failed with HTTP {}: {}",
            status.as_u16(),
            body
        ));
    }

    serde_json::from_str(&body)
        .map_err(|error| format!("failed to decode dataset preview payload: {error}"))
}

fn transform_row(
    row: &Value,
    property_mappings: &[OntologyFunnelPropertyMapping],
) -> Result<Value, String> {
    let Some(object) = row.as_object() else {
        return Err("dataset preview row is not a JSON object".to_string());
    };

    if property_mappings.is_empty() {
        return Ok(Value::Object(object.clone()));
    }

    let mut mapped = Map::new();
    for mapping in property_mappings {
        if let Some(value) = object.get(&mapping.source_field) {
            mapped.insert(mapping.target_property.clone(), value.clone());
        }
    }
    Ok(Value::Object(mapped))
}

fn primary_key_value(properties: &Value, primary_key_property: &str) -> Result<String, String> {
    let value = properties
        .get(primary_key_property)
        .ok_or_else(|| format!("missing primary key property '{primary_key_property}'"))?;
    value_as_store_text(value).map_err(|error| {
        format!("failed to serialize primary key property '{primary_key_property}': {error}")
    })
}

async fn find_existing_object_id(
    state: &AppState,
    claims: &Claims,
    object_type_id: Uuid,
    primary_key_property: &str,
    primary_key_value: &str,
) -> Result<Option<Uuid>, String> {
    find_object_id_by_property(
        state,
        claims,
        object_type_id,
        primary_key_property,
        primary_key_value,
        ReadConsistency::Strong,
    )
    .await
    .map_err(|error| format!("failed to look up existing object: {error}"))
}

async fn upsert_object_instance(
    state: &AppState,
    claims: &Claims,
    object_id: Option<Uuid>,
    object_type_id: Uuid,
    properties: &Value,
    marking: &str,
) -> Result<&'static str, String> {
    let now = Utc::now();
    let (object, expected_version, operation) = if let Some(id) = object_id {
        let existing = state
            .stores
            .objects
            .get(
                &tenant_from_claims(claims),
                &ObjectId(id.to_string()),
                ReadConsistency::Strong,
            )
            .await
            .map_err(|error| format!("failed to load existing funnel object: {error}"))?
            .ok_or_else(|| "existing funnel object was not found in object store".to_string())?;
        (
            ObjectInstance {
                id,
                object_type_id,
                properties: properties.clone(),
                created_by: existing
                    .owner
                    .as_ref()
                    .and_then(|owner| Uuid::parse_str(&owner.0).ok())
                    .unwrap_or(claims.sub),
                organization_id: existing
                    .organization_id
                    .as_deref()
                    .and_then(|raw| Uuid::parse_str(raw).ok())
                    .or(claims.org_id),
                marking: marking.to_string(),
                created_at: Utc
                    .timestamp_millis_opt(existing.created_at_ms.unwrap_or(existing.updated_at_ms))
                    .single()
                    .unwrap_or(now),
                updated_at: now,
            },
            Some(existing.version),
            "update",
        )
    } else {
        (
            ObjectInstance {
                id: Uuid::now_v7(),
                object_type_id,
                properties: properties.clone(),
                created_by: claims.sub,
                organization_id: claims.org_id,
                marking: marking.to_string(),
                created_at: now,
                updated_at: now,
            },
            None,
            "insert",
        )
    };

    let outcome = apply_object_write(
        state,
        claims,
        &object,
        expected_version,
        operation,
        json!({
            "source": "ontology_funnel",
        }),
    )
    .await?;
    append_object_revision(
        state,
        claims,
        &object,
        operation,
        outcome.committed_version as i64,
        None,
    )
    .await?;
    Ok(operation)
}

async fn execute_source_run(
    state: &AppState,
    claims: &Claims,
    source: &OntologyFunnelSource,
    request: &TriggerOntologyFunnelRunRequest,
) -> Result<FunnelExecutionOutcome, String> {
    let Some(object_type) = load_object_type(state, source.object_type_id).await? else {
        return Err("object type for funnel source was not found".to_string());
    };
    let primary_key_property = object_type.primary_key_property.clone().ok_or_else(|| {
        "object type must define primary_key_property for ontology funnel sync".to_string()
    })?;
    let definitions = load_effective_properties(&state.db, source.object_type_id)
        .await
        .map_err(|error| format!("failed to load object type properties: {error}"))?;

    let pipeline_run = if request.skip_pipeline {
        None
    } else {
        trigger_pipeline_run(state, claims, source, request.trigger_context.as_ref()).await?
    };

    let preview_limit = request
        .limit
        .map(Some)
        .map(normalize_preview_limit)
        .unwrap_or(source.preview_limit.clamp(1, 1_000));
    let preview = fetch_dataset_preview(
        state,
        claims,
        source,
        preview_limit,
        request
            .dataset_branch
            .as_deref()
            .or(source.dataset_branch.as_deref()),
        request.dataset_version.or(source.dataset_version),
    )
    .await?;

    let mut inserted_count = 0i32;
    let mut updated_count = 0i32;
    let mut skipped_count = 0i32;
    let mut error_count = 0i32;
    let mut row_errors = Vec::new();

    for (index, row) in preview.rows.iter().enumerate() {
        let transformed = match transform_row(row, &source.property_mappings) {
            Ok(transformed) => transformed,
            Err(error) => {
                error_count += 1;
                row_errors.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };
        let normalized = match validate_object_properties(&definitions, &transformed) {
            Ok(normalized) => normalized,
            Err(error) => {
                error_count += 1;
                row_errors.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };
        let primary_key_value = match primary_key_value(&normalized, &primary_key_property) {
            Ok(primary_key_value) => primary_key_value,
            Err(error) => {
                skipped_count += 1;
                row_errors.push(json!({ "row_index": index, "error": error }));
                continue;
            }
        };

        let existing_id = find_existing_object_id(
            state,
            claims,
            source.object_type_id,
            &primary_key_property,
            &primary_key_value,
        )
        .await?;

        if request.dry_run {
            if existing_id.is_some() {
                updated_count += 1;
            } else {
                inserted_count += 1;
            }
            continue;
        }

        match upsert_object_instance(
            state,
            claims,
            existing_id,
            source.object_type_id,
            &normalized,
            &source.default_marking,
        )
        .await?
        {
            "update" => updated_count += 1,
            "insert" => inserted_count += 1,
            _ => skipped_count += 1,
        }
    }

    let status = if error_count > 0 {
        if request.dry_run {
            "dry_run_with_errors"
        } else {
            "completed_with_errors"
        }
    } else if request.dry_run {
        "dry_run"
    } else {
        "completed"
    };

    Ok(FunnelExecutionOutcome {
        rows_read: preview.rows.len() as i32,
        inserted_count,
        updated_count,
        skipped_count,
        error_count,
        details: json!({
            "preview_total_rows": preview.total_rows,
            "warnings": preview.warnings,
            "preview_errors": preview.errors,
            "row_errors": row_errors,
            "primary_key_property": primary_key_property,
            "dry_run": request.dry_run,
            "pipeline_run": pipeline_run.as_ref().map(|run| json!({
                "id": run.id,
                "status": run.status
            }))
        }),
        error_message: None,
        pipeline_run_id: pipeline_run.map(|run| run.id),
        status: status.to_string(),
    })
}

pub async fn list_funnel_sources(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListOntologyFunnelSourcesQuery>,
) -> impl IntoResponse {
    let page = query.page.unwrap_or(1).max(1);
    let per_page = query.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let status_filter = query.status.unwrap_or_default();

    let data = match funnel_repository::list_sources(
        &state.db,
        ListSourcesParams {
            object_type_id: query.object_type_id,
            status_filter: &status_filter,
            is_admin: claims.has_role("admin"),
            actor_id: claims.sub,
            offset,
            limit: per_page,
        },
    )
    .await
    {
        Ok(rows) => rows,
        Err(error) => return db_error(format!("failed to list ontology funnel sources: {error}")),
    };

    let total = match funnel_repository::count_sources(
        &state.db,
        query.object_type_id,
        &status_filter,
        claims.has_role("admin"),
        claims.sub,
    )
    .await
    {
        Ok(total) => total,
        Err(error) => return db_error(format!("failed to count ontology funnel sources: {error}")),
    };

    Json(ListOntologyFunnelSourcesResponse {
        data,
        total,
        page,
        per_page,
    })
    .into_response()
}

pub async fn get_funnel_health(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Query(query): Query<ListOntologyFunnelHealthQuery>,
) -> impl IntoResponse {
    let stale_after_hours = normalize_stale_after_hours(query.stale_after_hours);

    let rows = match funnel_repository::list_sources_for_health(
        &state.db,
        HealthSourcesParams {
            object_type_id: query.object_type_id,
            is_admin: claims.has_role("admin"),
            actor_id: claims.sub,
        },
    )
    .await
    {
        Ok(rows) => rows,
        Err(error) => {
            return db_error(format!(
                "failed to list ontology funnel sources for health: {error}"
            ));
        }
    };

    let mut sources = Vec::new();
    for source in rows {
        let metrics = match load_funnel_health_metrics(&state, &claims, source.id).await {
            Ok(metrics) => metrics,
            Err(error) => return db_error(error),
        };
        sources.push(build_source_health(source, metrics, stale_after_hours));
    }

    sources.sort_by(|left, right| {
        funnel_health_sort_rank(&left.health_status)
            .cmp(&funnel_health_sort_rank(&right.health_status))
            .then_with(|| right.last_run_at.cmp(&left.last_run_at))
    });

    let total_sources = sources.len() as i64;
    let active_sources = sources
        .iter()
        .filter(|source_health| source_health.source.status == "active")
        .count() as i64;
    let paused_sources = sources
        .iter()
        .filter(|source_health| source_health.health_status == "paused")
        .count() as i64;
    let healthy_sources = sources
        .iter()
        .filter(|source_health| source_health.health_status == "healthy")
        .count() as i64;
    let degraded_sources = sources
        .iter()
        .filter(|source_health| source_health.health_status == "degraded")
        .count() as i64;
    let failing_sources = sources
        .iter()
        .filter(|source_health| source_health.health_status == "failing")
        .count() as i64;
    let stale_sources = sources
        .iter()
        .filter(|source_health| source_health.health_status == "stale")
        .count() as i64;
    let never_run_sources = sources
        .iter()
        .filter(|source_health| source_health.health_status == "never_run")
        .count() as i64;

    let total_runs = sources
        .iter()
        .map(|source_health| source_health.total_runs)
        .sum::<i64>();
    let successful_runs = sources
        .iter()
        .map(|source_health| source_health.successful_runs)
        .sum::<i64>();
    let failed_runs = sources
        .iter()
        .map(|source_health| source_health.failed_runs)
        .sum::<i64>();
    let warning_runs = sources
        .iter()
        .map(|source_health| source_health.warning_runs)
        .sum::<i64>();
    let rows_read = sources
        .iter()
        .map(|source_health| source_health.rows_read)
        .sum::<i64>();
    let inserted_count = sources
        .iter()
        .map(|source_health| source_health.inserted_count)
        .sum::<i64>();
    let updated_count = sources
        .iter()
        .map(|source_health| source_health.updated_count)
        .sum::<i64>();
    let skipped_count = sources
        .iter()
        .map(|source_health| source_health.skipped_count)
        .sum::<i64>();
    let error_count = sources
        .iter()
        .map(|source_health| source_health.error_count)
        .sum::<i64>();
    let last_run_at = sources
        .iter()
        .filter_map(|source_health| source_health.last_run_at)
        .max();
    let success_rate = if total_runs > 0 {
        successful_runs as f64 / total_runs as f64
    } else {
        0.0
    };

    Json(OntologyFunnelHealthResponse {
        stale_after_hours,
        total_sources,
        active_sources,
        paused_sources,
        healthy_sources,
        degraded_sources,
        failing_sources,
        stale_sources,
        never_run_sources,
        total_runs,
        successful_runs,
        failed_runs,
        warning_runs,
        success_rate,
        rows_read,
        inserted_count,
        updated_count,
        skipped_count,
        error_count,
        last_run_at,
        sources,
    })
    .into_response()
}

pub async fn get_funnel_source_health(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Query(query): Query<GetOntologyFunnelSourceHealthQuery>,
) -> impl IntoResponse {
    let Some(source) = (match load_source(&state, id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };
    if let Err(error) = ensure_owner_or_admin(source.owner_id, &claims) {
        return forbidden(error);
    }

    let stale_after_hours = normalize_stale_after_hours(query.stale_after_hours);
    let metrics = match load_funnel_health_metrics(&state, &claims, source.id).await {
        Ok(metrics) => metrics,
        Err(error) => return db_error(error),
    };

    Json(OntologyFunnelSourceHealthResponse {
        stale_after_hours,
        source_health: build_source_health(source, metrics, stale_after_hours),
    })
    .into_response()
}

pub async fn create_funnel_source(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Json(body): Json<CreateOntologyFunnelSourceRequest>,
) -> impl IntoResponse {
    if body.name.trim().is_empty() {
        return invalid("name is required");
    }
    if !object_type_exists(&state, body.object_type_id)
        .await
        .unwrap_or(false)
    {
        return invalid("object_type_id does not exist");
    }
    if !dataset_exists(&state, body.dataset_id)
        .await
        .unwrap_or(false)
    {
        return invalid("dataset_id does not exist");
    }
    if let Some(pipeline_id) = body.pipeline_id
        && !pipeline_exists(&state, pipeline_id).await.unwrap_or(false)
    {
        return invalid("pipeline_id does not exist");
    }

    let preview_limit = normalize_preview_limit(body.preview_limit);
    let status = normalize_funnel_status(body.status);
    if let Err(error) = validate_source_status(&status) {
        return invalid(error);
    }
    let default_marking = normalize_default_marking(body.default_marking);
    if let Err(error) = validate_marking(&default_marking) {
        return invalid(error);
    }

    match funnel_repository::create_source(
        &state.db,
        CreateSourceInput {
            id: Uuid::now_v7(),
            name: body.name.trim().to_string(),
            description: body.description.unwrap_or_default(),
            object_type_id: body.object_type_id,
            dataset_id: body.dataset_id,
            pipeline_id: body.pipeline_id,
            dataset_branch: body.dataset_branch,
            dataset_version: body.dataset_version,
            preview_limit,
            default_marking,
            status,
            property_mappings: serde_json::to_value(body.property_mappings.unwrap_or_default())
                .unwrap_or_else(|_| json!([])),
            trigger_context: body.trigger_context.unwrap_or_else(|| json!({})),
            owner_id: claims.sub,
        },
    )
    .await
    {
        Ok(source) => (StatusCode::CREATED, Json(source)).into_response(),
        Err(error) => return db_error(format!("failed to create ontology funnel source: {error}")),
    }
}

pub async fn get_funnel_source(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(source) = (match load_source(&state, id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };

    if let Err(error) = ensure_owner_or_admin(source.owner_id, &claims) {
        return forbidden(error);
    }

    Json(source).into_response()
}

pub async fn update_funnel_source(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateOntologyFunnelSourceRequest>,
) -> impl IntoResponse {
    let Some(existing) = (match load_source(&state, id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };
    if let Err(error) = ensure_owner_or_admin(existing.owner_id, &claims) {
        return forbidden(error);
    }

    if let Some(Some(pipeline_id)) = body.pipeline_id
        && !pipeline_exists(&state, pipeline_id).await.unwrap_or(false)
    {
        return invalid("pipeline_id does not exist");
    }

    let preview_limit = body
        .preview_limit
        .unwrap_or(existing.preview_limit)
        .clamp(1, 1_000);
    let status = body.status.unwrap_or(existing.status.clone());
    if let Err(error) = validate_source_status(&status) {
        return invalid(error);
    }
    let default_marking = body
        .default_marking
        .unwrap_or(existing.default_marking.clone());
    if let Err(error) = validate_marking(&default_marking) {
        return invalid(error);
    }

    match funnel_repository::update_source(
        &state.db,
        UpdateSourceInput {
            id,
            name: body.name.map(|value| value.trim().to_string()),
            description: body.description,
            pipeline_id: body.pipeline_id.unwrap_or(existing.pipeline_id),
            dataset_branch: body.dataset_branch.unwrap_or(existing.dataset_branch),
            dataset_version: body.dataset_version.unwrap_or(existing.dataset_version),
            preview_limit,
            default_marking,
            status,
            property_mappings: serde_json::to_value(
                body.property_mappings.unwrap_or(existing.property_mappings),
            )
            .unwrap_or_else(|_| json!([])),
            trigger_context: body.trigger_context.unwrap_or(existing.trigger_context),
        },
    )
    .await
    {
        Ok(Some(source)) => Json(source).into_response(),
        Ok(None) => not_found("ontology funnel source not found"),
        Err(error) => return db_error(format!("failed to update ontology funnel source: {error}")),
    }
}

pub async fn delete_funnel_source(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(source) = (match load_source(&state, id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };
    if let Err(error) = ensure_owner_or_admin(source.owner_id, &claims) {
        return forbidden(error);
    }

    match funnel_repository::delete_source(&state.db, id).await {
        Ok(true) => StatusCode::NO_CONTENT.into_response(),
        Ok(false) => not_found("ontology funnel source not found"),
        Err(error) => db_error(format!("failed to delete ontology funnel source: {error}")),
    }
}

pub async fn trigger_funnel_run(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<TriggerOntologyFunnelRunRequest>,
) -> impl IntoResponse {
    let Some(source) = (match load_source(&state, id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };
    if let Err(error) = ensure_owner_or_admin(source.owner_id, &claims) {
        return forbidden(error);
    }
    if source.status == "paused" {
        return invalid("ontology funnel source is paused");
    }

    let run_id = Uuid::now_v7();
    let tenant = tenant_from_claims(&claims);
    if let Err(error) = funnel_repository::create_run(
        state.stores.actions.as_ref(),
        tenant.clone(),
        CreateRunInput {
            id: run_id,
            source_id: source.id,
            object_type_id: source.object_type_id,
            dataset_id: source.dataset_id,
            pipeline_id: source.pipeline_id,
            trigger_type: if body.dry_run {
                "manual_dry_run".to_string()
            } else {
                "manual".to_string()
            },
            started_by: claims.sub,
            details: json!({ "started": true }),
        },
    )
    .await
    {
        return db_error(format!("failed to create ontology funnel run: {error}"));
    }

    let outcome = execute_source_run(&state, &claims, &source, &body).await;
    match outcome {
        Ok(outcome) => {
            let finished_at = chrono::Utc::now();
            let _ = funnel_repository::complete_run(
                state.stores.actions.as_ref(),
                tenant.clone(),
                claims.sub,
                CompleteRunInput {
                    id: run_id,
                    source_id: source.id,
                    pipeline_run_id: outcome.pipeline_run_id,
                    status: outcome.status,
                    rows_read: outcome.rows_read,
                    inserted_count: outcome.inserted_count,
                    updated_count: outcome.updated_count,
                    skipped_count: outcome.skipped_count,
                    error_count: outcome.error_count,
                    details: outcome.details,
                    error_message: outcome.error_message,
                    finished_at,
                },
            )
            .await;
            let _ = funnel_repository::mark_source_ran(&state.db, source.id, finished_at).await;
        }
        Err(error) => {
            let _ = funnel_repository::fail_run(
                state.stores.actions.as_ref(),
                tenant.clone(),
                source.id,
                run_id,
                claims.sub,
                &error,
            )
            .await;
            return db_error(error);
        }
    }

    match funnel_repository::load_run_for_source(
        state.stores.actions.as_ref(),
        &tenant,
        source.id,
        run_id,
    )
    .await
    {
        Ok(Some(run)) => Json(run).into_response(),
        Ok(None) => db_error("ontology funnel run completed but could not be reloaded"),
        Err(error) => db_error(format!("failed to reload ontology funnel run: {error}")),
    }
}

pub async fn list_funnel_runs(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Query(query): Query<ListOntologyFunnelRunsQuery>,
) -> impl IntoResponse {
    let Some(source) = (match load_source(&state, id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };
    if let Err(error) = ensure_owner_or_admin(source.owner_id, &claims) {
        return forbidden(error);
    }

    let page = query.page.unwrap_or(1).max(1);
    let per_page = query.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;
    let tenant = tenant_from_claims(&claims);

    let total =
        match funnel_repository::count_runs_for_source(state.stores.actions.as_ref(), &tenant, id)
            .await
        {
            Ok(total) => total,
            Err(error) => {
                return db_error(format!("failed to count ontology funnel runs: {error}"));
            }
        };

    let data = match funnel_repository::list_runs_for_source(
        state.stores.actions.as_ref(),
        &tenant,
        id,
        offset,
        per_page,
    )
    .await
    {
        Ok(data) => data,
        Err(error) => return db_error(format!("failed to list ontology funnel runs: {error}")),
    };

    Json(ListOntologyFunnelRunsResponse {
        data,
        total,
        page,
        per_page,
    })
    .into_response()
}

pub async fn get_funnel_run(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((source_id, run_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    let Some(source) = (match load_source(&state, source_id).await {
        Ok(source) => source,
        Err(error) => return db_error(error),
    }) else {
        return not_found("ontology funnel source not found");
    };
    if let Err(error) = ensure_owner_or_admin(source.owner_id, &claims) {
        return forbidden(error);
    }

    let tenant = tenant_from_claims(&claims);
    match funnel_repository::load_run_for_source(
        state.stores.actions.as_ref(),
        &tenant,
        source_id,
        run_id,
    )
    .await
    {
        Ok(Some(run)) => Json(run).into_response(),
        Ok(None) => not_found("ontology funnel run not found"),
        Err(error) => db_error(format!("failed to load ontology funnel run: {error}")),
    }
}

#[cfg(test)]
mod tests {
    use super::{OntologyFunnelHealthMetricsRow, OntologyFunnelSource, build_source_health};
    use chrono::{Duration, Utc};
    use serde_json::json;
    use uuid::Uuid;

    fn sample_source(status: &str) -> OntologyFunnelSource {
        OntologyFunnelSource {
            id: Uuid::now_v7(),
            name: "tickets-batch".to_string(),
            description: String::new(),
            object_type_id: Uuid::now_v7(),
            dataset_id: Uuid::now_v7(),
            pipeline_id: None,
            dataset_branch: None,
            dataset_version: None,
            preview_limit: 100,
            default_marking: "public".to_string(),
            status: status.to_string(),
            property_mappings: vec![],
            trigger_context: json!({}),
            owner_id: Uuid::now_v7(),
            last_run_at: None,
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    fn sample_metrics(
        latest_run_status: Option<&str>,
        total_runs: i64,
        last_run_at: Option<chrono::DateTime<Utc>>,
    ) -> OntologyFunnelHealthMetricsRow {
        OntologyFunnelHealthMetricsRow {
            total_runs,
            successful_runs: if matches!(latest_run_status, Some("completed" | "dry_run")) {
                total_runs
            } else {
                0
            },
            failed_runs: if latest_run_status == Some("failed") {
                1
            } else {
                0
            },
            warning_runs: if matches!(
                latest_run_status,
                Some("completed_with_errors" | "dry_run_with_errors")
            ) {
                1
            } else {
                0
            },
            avg_duration_ms: Some(1200.0),
            p95_duration_ms: Some(1800.0),
            max_duration_ms: Some(2000),
            latest_run_status: latest_run_status.map(ToString::to_string),
            last_run_at,
            last_success_at: if matches!(latest_run_status, Some("completed" | "dry_run")) {
                last_run_at
            } else {
                None
            },
            last_failure_at: if latest_run_status == Some("failed") {
                last_run_at
            } else {
                None
            },
            last_warning_at: if matches!(
                latest_run_status,
                Some("completed_with_errors" | "dry_run_with_errors")
            ) {
                last_run_at
            } else {
                None
            },
            rows_read: 100,
            inserted_count: 40,
            updated_count: 60,
            skipped_count: 0,
            error_count: if latest_run_status == Some("completed_with_errors") {
                3
            } else {
                0
            },
        }
    }

    #[test]
    fn classifies_healthy_source_when_latest_run_completed() {
        let source = sample_source("active");
        let metrics = sample_metrics(Some("completed"), 4, Some(Utc::now()));

        let health = build_source_health(source, metrics, 24);

        assert_eq!(health.health_status, "healthy");
    }

    #[test]
    fn classifies_failing_source_when_latest_run_failed() {
        let source = sample_source("active");
        let metrics = sample_metrics(Some("failed"), 4, Some(Utc::now()));

        let health = build_source_health(source, metrics, 24);

        assert_eq!(health.health_status, "failing");
    }

    #[test]
    fn classifies_stale_source_when_last_run_is_too_old() {
        let source = sample_source("active");
        let metrics = sample_metrics(Some("completed"), 4, Some(Utc::now() - Duration::hours(48)));

        let health = build_source_health(source, metrics, 24);

        assert_eq!(health.health_status, "stale");
    }

    #[test]
    fn classifies_paused_source_before_considering_runs() {
        let source = sample_source("paused");
        let metrics = sample_metrics(Some("failed"), 4, Some(Utc::now()));

        let health = build_source_health(source, metrics, 24);

        assert_eq!(health.health_status, "paused");
    }

    #[test]
    fn classifies_never_run_source_without_history() {
        let source = sample_source("active");
        let metrics = sample_metrics(None, 0, None);

        let health = build_source_health(source, metrics, 24);

        assert_eq!(health.health_status, "never_run");
    }
}
