//! Data Connection MVP handlers: credentials, source-policy bindings and batch
//! sync definitions. The frontend
//! (`apps/web/src/lib/api/data-connection.ts`) drives every signature here.
//!
//! Credentials are stored encrypted via a fingerprint-only contract; the
//! plaintext never leaves this service. For the MVP the "encryption" is a
//! straight UTF-8 copy of the bytes (stored in `BYTEA`); the ciphertext
//! column is wired so a real KMS envelope can be plugged in without a
//! schema migration.
//!
//! Runtime authority for sync execution does not live here anymore:
//! `runSync` dispatches into `ingestion-replication-service`, and any run
//! listing exposed here is a filtered view of that remote control plane.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use sqlx::types::chrono::{DateTime, Utc};
use uuid::Uuid;

use crate::AppState;

// ─── Credentials ──────────────────────────────────────────────────────────────

#[derive(Debug, Serialize)]
pub struct CredentialResponse {
    pub id: Uuid,
    pub source_id: Uuid,
    pub kind: String,
    pub fingerprint: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct SetCredentialRequest {
    pub kind: String,
    pub value: String,
}

const VALID_CREDENTIAL_KINDS: &[&str] = &[
    "password",
    "api_key",
    "oauth_token",
    "aws_keys",
    "service_account_json",
];

pub async fn set_credential(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
    Json(body): Json<SetCredentialRequest>,
) -> impl IntoResponse {
    if !VALID_CREDENTIAL_KINDS.contains(&body.kind.as_str()) {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": format!("unsupported credential kind: {}", body.kind) })),
        )
            .into_response();
    }

    let mut hasher = Sha256::new();
    hasher.update(body.value.as_bytes());
    let fingerprint = format!("{:x}", hasher.finalize());

    // Encrypt the plaintext with AES-256-GCM before persisting; the column
    // never holds raw secret bytes.
    let ciphertext =
        match crate::credential_crypto::encrypt(&state.credential_key, body.value.as_bytes()) {
            Ok(blob) => blob,
            Err(err) => {
                tracing::error!(?err, "credential encryption failed");
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({ "error": "credential encryption failed" })),
                )
                    .into_response();
            }
        };

    // Upsert (one credential per source/kind).
    let id = Uuid::now_v7();
    let row = sqlx::query_as::<_, (Uuid, Uuid, String, String, DateTime<Utc>)>(
        r#"
        INSERT INTO source_credentials (id, source_id, kind, secret_ciphertext, fingerprint)
        VALUES ($1, $2, $3, $4, $5)
        ON CONFLICT (source_id, kind) DO UPDATE
          SET secret_ciphertext = EXCLUDED.secret_ciphertext,
              fingerprint       = EXCLUDED.fingerprint,
              created_at        = NOW()
        RETURNING id, source_id, kind, fingerprint, created_at
        "#,
    )
    .bind(id)
    .bind(source_id)
    .bind(&body.kind)
    .bind(&ciphertext)
    .bind(&fingerprint)
    .fetch_one(&state.db)
    .await;

    match row {
        Ok((id, source_id, kind, fingerprint, created_at)) => (
            StatusCode::OK,
            Json(CredentialResponse {
                id,
                source_id,
                kind,
                fingerprint,
                created_at,
            }),
        )
            .into_response(),
        Err(err) => internal(err),
    }
}

pub async fn list_credentials(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, (Uuid, Uuid, String, String, DateTime<Utc>)>(
        "SELECT id, source_id, kind, fingerprint, created_at FROM source_credentials WHERE source_id = $1 ORDER BY created_at DESC",
    )
    .bind(source_id)
    .fetch_all(&state.db)
    .await;

    match rows {
        Ok(rows) => {
            let body: Vec<CredentialResponse> = rows
                .into_iter()
                .map(
                    |(id, source_id, kind, fingerprint, created_at)| CredentialResponse {
                        id,
                        source_id,
                        kind,
                        fingerprint,
                        created_at,
                    },
                )
                .collect();
            (StatusCode::OK, Json(body)).into_response()
        }
        Err(err) => internal(err),
    }
}

// ─── Source ↔ egress-policy bindings ─────────────────────────────────────────

#[derive(Debug, Serialize)]
pub struct SourcePolicyBindingResponse {
    pub source_id: Uuid,
    pub policy_id: Uuid,
    pub kind: String,
}

#[derive(Debug, Deserialize)]
pub struct AttachPolicyRequest {
    pub policy_id: Uuid,
    #[serde(default = "default_binding_kind")]
    pub kind: String,
}

fn default_binding_kind() -> String {
    "direct".to_string()
}

pub async fn list_source_policies(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
) -> impl IntoResponse {
    // Returns the joined policy rows fetched from network-boundary-service.
    // For the MVP we return only the bindings; the frontend rehydrates the
    // policy detail by listing the global egress catalog separately.
    let rows = sqlx::query_as::<_, (Uuid, Uuid, String)>(
        "SELECT source_id, policy_id, kind FROM source_policy_bindings WHERE source_id = $1",
    )
    .bind(source_id)
    .fetch_all(&state.db)
    .await;

    match rows {
        Ok(rows) => {
            let body: Vec<SourcePolicyBindingResponse> = rows
                .into_iter()
                .map(|(source_id, policy_id, kind)| SourcePolicyBindingResponse {
                    source_id,
                    policy_id,
                    kind,
                })
                .collect();
            (StatusCode::OK, Json(body)).into_response()
        }
        Err(err) => internal(err),
    }
}

pub async fn attach_policy(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
    Json(body): Json<AttachPolicyRequest>,
) -> impl IntoResponse {
    if body.kind != "direct" && body.kind != "agent_proxy" {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "kind must be 'direct' or 'agent_proxy'" })),
        )
            .into_response();
    }
    let row = sqlx::query_as::<_, (Uuid, Uuid, String)>(
        r#"
        INSERT INTO source_policy_bindings (source_id, policy_id, kind)
        VALUES ($1, $2, $3)
        ON CONFLICT (source_id, policy_id) DO UPDATE SET kind = EXCLUDED.kind
        RETURNING source_id, policy_id, kind
        "#,
    )
    .bind(source_id)
    .bind(body.policy_id)
    .bind(&body.kind)
    .fetch_one(&state.db)
    .await;

    match row {
        Ok((source_id, policy_id, kind)) => (
            StatusCode::OK,
            Json(SourcePolicyBindingResponse {
                source_id,
                policy_id,
                kind,
            }),
        )
            .into_response(),
        Err(err) => internal(err),
    }
}

pub async fn detach_policy(
    State(state): State<AppState>,
    Path((source_id, policy_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    let result =
        sqlx::query("DELETE FROM source_policy_bindings WHERE source_id = $1 AND policy_id = $2")
            .bind(source_id)
            .bind(policy_id)
            .execute(&state.db)
            .await;

    match result {
        Ok(_) => StatusCode::NO_CONTENT.into_response(),
        Err(err) => internal(err),
    }
}

// ─── Batch sync defs and runs ────────────────────────────────────────────────

#[derive(Debug, Serialize)]
pub struct BatchSyncDefResponse {
    pub id: Uuid,
    pub source_id: Uuid,
    pub output_dataset_id: Uuid,
    pub file_glob: Option<String>,
    pub schedule_cron: Option<String>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct CreateBatchSyncRequest {
    pub source_id: Uuid,
    pub output_dataset_id: Uuid,
    pub file_glob: Option<String>,
    pub schedule_cron: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct SyncRunResponse {
    pub id: Uuid,
    pub sync_def_id: Uuid,
    pub status: String,
    pub started_at: DateTime<Utc>,
    pub finished_at: Option<DateTime<Utc>>,
    pub bytes_written: i64,
    pub files_written: i64,
    pub error: Option<String>,
    pub ingest_job_id: Option<String>,
    pub dataset_version_id: Option<Uuid>,
    pub content_hash: Option<String>,
}

pub async fn list_syncs(
    State(state): State<AppState>,
    Path(source_id): Path<Uuid>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<
        _,
        (
            Uuid,
            Uuid,
            Uuid,
            Option<String>,
            Option<String>,
            DateTime<Utc>,
        ),
    >(
        "SELECT id, source_id, output_dataset_id, file_glob, schedule_cron, created_at
         FROM batch_sync_defs WHERE source_id = $1 ORDER BY created_at DESC",
    )
    .bind(source_id)
    .fetch_all(&state.db)
    .await;

    match rows {
        Ok(rows) => {
            let body: Vec<BatchSyncDefResponse> = rows
                .into_iter()
                .map(
                    |(id, source_id, output_dataset_id, file_glob, schedule_cron, created_at)| {
                        BatchSyncDefResponse {
                            id,
                            source_id,
                            output_dataset_id,
                            file_glob,
                            schedule_cron,
                            created_at,
                        }
                    },
                )
                .collect();
            (StatusCode::OK, Json(body)).into_response()
        }
        Err(err) => internal(err),
    }
}

pub async fn create_sync(
    State(state): State<AppState>,
    Json(body): Json<CreateBatchSyncRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let row = sqlx::query_as::<
        _,
        (
            Uuid,
            Uuid,
            Uuid,
            Option<String>,
            Option<String>,
            DateTime<Utc>,
        ),
    >(
        r#"
        INSERT INTO batch_sync_defs (id, source_id, output_dataset_id, file_glob, schedule_cron)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, source_id, output_dataset_id, file_glob, schedule_cron, created_at
        "#,
    )
    .bind(id)
    .bind(body.source_id)
    .bind(body.output_dataset_id)
    .bind(body.file_glob)
    .bind(body.schedule_cron)
    .fetch_one(&state.db)
    .await;

    match row {
        Ok((id, source_id, output_dataset_id, file_glob, schedule_cron, created_at)) => (
            StatusCode::CREATED,
            Json(BatchSyncDefResponse {
                id,
                source_id,
                output_dataset_id,
                file_glob,
                schedule_cron,
                created_at,
            }),
        )
            .into_response(),
        Err(err) => internal(err),
    }
}

pub async fn run_sync(
    State(state): State<AppState>,
    Path(sync_id): Path<Uuid>,
) -> impl IntoResponse {
    // Tarea 9 — observabilidad: time the entire run_sync handler so we can
    // emit `connector_sync_duration_seconds` once the connector kind is
    // known (after the SELECT below). The connector type is captured from
    // the row.2 column.
    let started = std::time::Instant::now();
    // Resolve the sync def → source so we can build an `IngestJobSpec` for
    // ingestion-replication-service. The sync definition stays local; the
    // resulting runtime job does not.
    let source = sqlx::query_as::<_, (String, String, serde_json::Value)>(
        r#"
        SELECT s.name, s.connector_type, s.config
        FROM batch_sync_defs d
        JOIN connections s ON s.id = d.source_id
        WHERE d.id = $1
        "#,
    )
    .bind(sync_id)
    .fetch_optional(&state.db)
    .await;

    let source = match source {
        Ok(Some(row)) => row,
        Ok(None) => {
            return (
                StatusCode::NOT_FOUND,
                Json(serde_json::json!({ "error": "sync def not found" })),
            )
                .into_response();
        }
        Err(err) => return internal(err),
    };
    let job_name = sync_job_name(&source.0, sync_id);

    let dispatched = match materialise_via_bridge(&state, &job_name, &source.1, &source.2).await {
        Ok(job) => job,
        Err((status, error)) => {
            tracing::warn!(
                target: "connector.run_sync",
                connector_kind = %source.1,
                sync_def_id = %sync_id,
                bridge_status = %status,
                error = %error,
                "sync dispatch failed"
            );
            state
                .metrics
                .record_sync(&source.1, 0, started.elapsed().as_secs_f64(), false);
            return (
                status,
                Json(serde_json::json!({
                    "error": error,
                    "sync_def_id": sync_id,
                })),
            )
                .into_response();
        }
    };

    // Tarea 9 — observabilidad: emit a span event with connector kind so
    // OTel exporters group sync_run telemetry consistently with
    // `connector.test_connection`.
    tracing::info!(
        target: "connector.run_sync",
        connector_kind = %source.1,
        sync_def_id = %sync_id,
        bridge_status = %dispatched.status,
        ingest_job_id = %dispatched.id,
        "sync run dispatched"
    );

    let http_status = if dispatched.status == "failed" {
        StatusCode::BAD_GATEWAY
    } else {
        StatusCode::ACCEPTED
    };
    let duration_seconds = started.elapsed().as_secs_f64();
    state.metrics.record_sync(
        &source.1,
        0,
        duration_seconds,
        dispatched.status != "failed",
    );

    match sync_run_response_from_ingest_job(sync_id, dispatched) {
        Ok(response) => (http_status, Json(response)).into_response(),
        Err(error) => (
            StatusCode::BAD_GATEWAY,
            Json(serde_json::json!({ "error": error, "sync_def_id": sync_id })),
        )
            .into_response(),
    }
}

/// Issue `CreateIngestJob` against `ingestion-replication-service`.
async fn materialise_via_bridge(
    state: &AppState,
    source_name: &str,
    connector_type: &str,
    config: &serde_json::Value,
) -> Result<crate::ingestion_bridge::IngestJob, (StatusCode, String)> {
    let url = state.ingestion_replication_grpc_url.trim();
    if url.is_empty() {
        return Err((
            StatusCode::SERVICE_UNAVAILABLE,
            "ingestion-replication gRPC bridge is not configured".to_string(),
        ));
    }

    let spec = match crate::ingestion_bridge::build_spec(
        source_name,
        connector_type,
        config,
        "openfoundry",
        "openfoundry-connect",
    ) {
        Ok(spec) => spec,
        Err(err) => return Err((StatusCode::BAD_REQUEST, err.to_string())),
    };

    let mut client = match crate::ingestion_bridge::IngestionControlPlaneClient::connect(
        url.to_string(),
    )
    .await
    {
        Ok(c) => c,
        Err(err) => {
            return Err((
                StatusCode::BAD_GATEWAY,
                format!("connect ingestion gRPC: {err}"),
            ));
        }
    };

    let req = crate::ingestion_bridge::CreateIngestJobRequest { spec: Some(spec) };
    match client.create_ingest_job(req).await {
        Ok(resp) => Ok(resp.into_inner()),
        Err(status) => Err((
            StatusCode::BAD_GATEWAY,
            format!("CreateIngestJob: {status}"),
        )),
    }
}

pub async fn list_runs(
    State(state): State<AppState>,
    Path(sync_id): Path<Uuid>,
) -> impl IntoResponse {
    let source = sqlx::query_as::<_, (String,)>(
        r#"
        SELECT s.name
        FROM batch_sync_defs d
        JOIN connections s ON s.id = d.source_id
        WHERE d.id = $1
        "#,
    )
    .bind(sync_id)
    .fetch_optional(&state.db)
    .await;

    let source = match source {
        Ok(Some(source)) => source,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(err) => return internal(err),
    };

    let mut client = match connect_ingestion_control_plane(&state).await {
        Ok(client) => client,
        Err(response) => return response,
    };

    let response = match client
        .list_ingest_jobs(crate::ingestion_bridge::ListIngestJobsRequest {})
        .await
    {
        Ok(response) => response.into_inner(),
        Err(error) => {
            return (
                StatusCode::BAD_GATEWAY,
                Json(serde_json::json!({ "error": format!("ListIngestJobs: {error}") })),
            )
                .into_response();
        }
    };

    let job_name = sync_job_name(&source.0, sync_id);
    let mut runs = Vec::new();
    for job in response.jobs {
        let matches_sync_def = job
            .spec
            .as_ref()
            .map(|spec| spec.name == job_name)
            .unwrap_or(false);
        if !matches_sync_def {
            continue;
        }
        match sync_run_response_from_ingest_job(sync_id, job) {
            Ok(run) => runs.push(run),
            Err(error) => {
                tracing::warn!(sync_def_id = %sync_id, "remote ingest job decode failed: {error}");
            }
        }
    }

    (StatusCode::OK, Json(runs)).into_response()
}

async fn connect_ingestion_control_plane(
    state: &AppState,
) -> Result<
    crate::ingestion_bridge::IngestionControlPlaneClient<tonic::transport::Channel>,
    axum::response::Response,
> {
    let url = state.ingestion_replication_grpc_url.trim();
    if url.is_empty() {
        return Err((
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": "ingestion-replication gRPC bridge is not configured",
            })),
        )
            .into_response());
    }

    crate::ingestion_bridge::IngestionControlPlaneClient::connect(url.to_string())
        .await
        .map_err(|error| {
            (
                StatusCode::BAD_GATEWAY,
                Json(serde_json::json!({
                    "error": format!("connect ingestion gRPC: {error}"),
                })),
            )
                .into_response()
        })
}

fn sync_job_name(source_name: &str, sync_id: Uuid) -> String {
    let mut slug = String::with_capacity(source_name.len());
    let mut last_was_dash = false;
    for ch in source_name.chars() {
        let normalized = if ch.is_ascii_alphanumeric() {
            Some(ch.to_ascii_lowercase())
        } else {
            Some('-')
        };
        if let Some(value) = normalized {
            if value == '-' {
                if last_was_dash {
                    continue;
                }
                last_was_dash = true;
            } else {
                last_was_dash = false;
            }
            slug.push(value);
        }
    }
    let slug = slug.trim_matches('-');
    let slug = if slug.is_empty() { "sync" } else { slug };
    let suffix = sync_id.to_string();
    let max_slug_len = 63usize.saturating_sub(suffix.len() + 1);
    let slug = &slug[..slug.len().min(max_slug_len)];
    format!("{slug}-{suffix}")
}

fn sync_run_response_from_ingest_job(
    sync_def_id: Uuid,
    job: crate::ingestion_bridge::IngestJob,
) -> Result<SyncRunResponse, String> {
    let id = Uuid::parse_str(&job.id).map_err(|error| error.to_string())?;
    let started_at = parse_rfc3339_utc(&job.created_at)?;
    let updated_at = parse_rfc3339_utc(&job.updated_at)?;
    let finished_at = if is_remote_terminal_status(&job.status) {
        Some(updated_at)
    } else {
        None
    };

    Ok(SyncRunResponse {
        id,
        sync_def_id,
        status: job.status.clone(),
        started_at,
        finished_at,
        bytes_written: 0,
        files_written: 0,
        error: if job.error.trim().is_empty() {
            None
        } else {
            Some(job.error)
        },
        ingest_job_id: Some(job.id),
        dataset_version_id: None,
        content_hash: None,
    })
}

fn parse_rfc3339_utc(raw: &str) -> Result<DateTime<Utc>, String> {
    chrono::DateTime::parse_from_rfc3339(raw)
        .map(|value| value.with_timezone(&Utc))
        .map_err(|error| error.to_string())
}

fn is_remote_terminal_status(status: &str) -> bool {
    matches!(status, "materialized" | "failed")
}

fn internal(err: sqlx::Error) -> axum::response::Response {
    tracing::error!(?err, "data_connection handler db error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(serde_json::json!({ "error": err.to_string() })),
    )
        .into_response()
}
