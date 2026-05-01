//! Data Connection MVP handlers: credentials, source-policy bindings, batch
//! sync defs and runs. The frontend
//! (`apps/web/src/lib/api/data-connection.ts`) drives every signature here.
//!
//! Credentials are stored encrypted via a fingerprint-only contract; the
//! plaintext never leaves this service. For the MVP the "encryption" is a
//! straight UTF-8 copy of the bytes (stored in `BYTEA`); the ciphertext
//! column is wired so a real KMS envelope can be plugged in without a
//! schema migration.
//!
//! `runSync` only enqueues a `sync_runs` row in `pending`. The actual
//! materialisation is owned by `ingestion-replication-service`'s
//! `IngestionControlPlane.CreateIngestJob`; the gRPC bridge is wired in a
//! follow-up so the frontend has live data as soon as that service publishes
//! a callback.

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
            Json(CredentialResponse { id, source_id, kind, fingerprint, created_at }),
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
                .map(|(id, source_id, kind, fingerprint, created_at)| CredentialResponse {
                    id, source_id, kind, fingerprint, created_at,
                })
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
                    source_id, policy_id, kind,
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
            Json(SourcePolicyBindingResponse { source_id, policy_id, kind }),
        )
            .into_response(),
        Err(err) => internal(err),
    }
}

pub async fn detach_policy(
    State(state): State<AppState>,
    Path((source_id, policy_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    let result = sqlx::query(
        "DELETE FROM source_policy_bindings WHERE source_id = $1 AND policy_id = $2",
    )
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
    let rows = sqlx::query_as::<_, (Uuid, Uuid, Uuid, Option<String>, Option<String>, DateTime<Utc>)>(
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
                .map(|(id, source_id, output_dataset_id, file_glob, schedule_cron, created_at)| {
                    BatchSyncDefResponse {
                        id, source_id, output_dataset_id, file_glob, schedule_cron, created_at,
                    }
                })
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
    let row = sqlx::query_as::<_, (Uuid, Uuid, Uuid, Option<String>, Option<String>, DateTime<Utc>)>(
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
                id, source_id, output_dataset_id, file_glob, schedule_cron, created_at,
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
    // Resolve the sync def → source so we can build an `IngestJobSpec` for
    // ingestion-replication-service.
    let source = sqlx::query_as::<_, (Uuid, String, String, serde_json::Value, Uuid)>(
        r#"
        SELECT s.id, s.name, s.connector_type, s.config, d.output_dataset_id
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
    let output_dataset_id = source.4;

    // Decide initial status + remote ingest_job_id by attempting the gRPC
    // bridge when the URL is configured. Failures degrade gracefully:
    //   * URL unset            → status `pending`, no bridge attempt
    //   * unsupported connector → `failed`, error captured
    //   * gRPC transport error  → `failed`, error captured
    //   * gRPC ok               → `running`, ingest_job_id stored
    let (status, ingest_job_id, error_msg) =
        materialise_via_bridge(&state, &source.1, &source.2, &source.3).await;

    let id = Uuid::now_v7();
    let row = sqlx::query_as::<_, (
        Uuid,
        Uuid,
        String,
        DateTime<Utc>,
        Option<DateTime<Utc>>,
        i64,
        i64,
        Option<String>,
        Option<String>,
    )>(
        r#"
        INSERT INTO sync_runs (id, sync_def_id, status, error, ingest_job_id)
        VALUES ($1, $2, $3, $4, $5)
        RETURNING id, sync_def_id, status, started_at, finished_at,
                  bytes_written, files_written, error, ingest_job_id
        "#,
    )
    .bind(id)
    .bind(sync_id)
    .bind(&status)
    .bind(error_msg.as_deref())
    .bind(ingest_job_id.as_deref())
    .fetch_one(&state.db)
    .await;

    match row {
        Ok((
            id,
            sync_def_id,
            status,
            started_at,
            finished_at,
            bytes_written,
            files_written,
            error,
            ingest_job_id,
        )) => {
            // After a successful sync_run, register a dataset version with
            // dataset-versioning-service so the version graph carries the
            // (content_hash, schema, row_count, source_id) lineage. Failures
            // are logged but do not flip the run to failed: the ingest may
            // have already happened out-of-band via the gRPC bridge.
            let (dataset_version_id, content_hash) = if status == "failed" {
                (None, None)
            } else {
                match register_version_for_run(
                    &state,
                    sync_def_id,
                    output_dataset_id,
                    source.0,
                    &source.1,
                    &source.2,
                    &source.3,
                    id,
                )
                .await
                {
                    Ok(reg) => (
                        Some(reg.dataset_version_id),
                        Some(reg.content_hash),
                    ),
                    Err(err) => {
                        tracing::warn!(
                            sync_run_id = %id,
                            error = %err,
                            "dataset version registration failed; sync_run kept without dataset_version_id"
                        );
                        (None, None)
                    }
                }
            };

            let http_status = if status == "failed" {
                StatusCode::BAD_GATEWAY
            } else {
                StatusCode::ACCEPTED
            };
            (
                http_status,
                Json(SyncRunResponse {
                    id,
                    sync_def_id,
                    status,
                    started_at,
                    finished_at,
                    bytes_written,
                    files_written,
                    error,
                    ingest_job_id,
                    dataset_version_id,
                    content_hash,
                }),
            )
                .into_response()
        }
        Err(err) => internal(err),
    }
}

/// Look up `output_dataset_id` for the given sync_def and call
/// [`crate::domain::dataset_versioning::register_for_run`].
#[allow(clippy::too_many_arguments)]
async fn register_version_for_run(
    state: &AppState,
    sync_def_id: Uuid,
    output_dataset_id: Uuid,
    source_id: Uuid,
    source_name: &str,
    connector_type: &str,
    config: &serde_json::Value,
    sync_run_id: Uuid,
) -> Result<crate::domain::dataset_versioning::VersionRegistration, String> {
    let signature_extra = serde_json::json!({
        "source_name": source_name,
        "connector_type": connector_type,
        "config": config,
    });
    let content_hash = crate::domain::dataset_versioning::signature_for_bridge_run(
        sync_def_id,
        source_id,
        output_dataset_id,
        &signature_extra,
    );
    let content = crate::domain::dataset_versioning::VersionContent {
        source_id,
        output_dataset_id,
        content_hash,
        row_count: 0,
        size_bytes: 0,
        schema: serde_json::json!({
            "connector_type": connector_type,
            "source_name": source_name,
        }),
        message: format!("sync_run {sync_run_id} via {connector_type}"),
        branch_name: None,
    };
    crate::domain::dataset_versioning::register_for_run(
        &state.db,
        &state.http_client,
        &state.dataset_service_url,
        sync_def_id,
        sync_run_id,
        content,
    )
    .await
}

/// Try to issue `CreateIngestJob` against `ingestion-replication-service`.
/// Returns `(status, ingest_job_id, error)` ready to be persisted in `sync_runs`.
async fn materialise_via_bridge(
    state: &AppState,
    source_name: &str,
    connector_type: &str,
    config: &serde_json::Value,
) -> (String, Option<String>, Option<String>) {
    let url = state.ingestion_replication_grpc_url.trim();
    if url.is_empty() {
        // Bridge disabled — keep legacy behaviour.
        return ("pending".to_string(), None, None);
    }

    let spec = match crate::ingestion_bridge::build_spec(
        source_name,
        connector_type,
        config,
        "openfoundry",
        "openfoundry-connect",
    ) {
        Ok(spec) => spec,
        Err(err) => return ("failed".to_string(), None, Some(err.to_string())),
    };

    let mut client =
        match crate::ingestion_bridge::IngestionControlPlaneClient::connect(url.to_string()).await
        {
            Ok(c) => c,
            Err(err) => {
                return (
                    "failed".to_string(),
                    None,
                    Some(format!("connect ingestion gRPC: {err}")),
                );
            }
        };

    let req = crate::ingestion_bridge::CreateIngestJobRequest { spec: Some(spec) };
    match client.create_ingest_job(req).await {
        Ok(resp) => {
            let job = resp.into_inner();
            ("running".to_string(), Some(job.id), None)
        }
        Err(status) => (
            "failed".to_string(),
            None,
            Some(format!("CreateIngestJob: {status}")),
        ),
    }
}

pub async fn list_runs(
    State(state): State<AppState>,
    Path(sync_id): Path<Uuid>,
) -> impl IntoResponse {
    let rows = sqlx::query_as::<_, (Uuid, Uuid, String, DateTime<Utc>, Option<DateTime<Utc>>, i64, i64, Option<String>, Option<String>, Option<Uuid>, Option<String>)>(
        "SELECT id, sync_def_id, status, started_at, finished_at, bytes_written, files_written, error, ingest_job_id, dataset_version_id, content_hash
         FROM sync_runs WHERE sync_def_id = $1 ORDER BY started_at DESC",
    )
    .bind(sync_id)
    .fetch_all(&state.db)
    .await;

    match rows {
        Ok(rows) => {
            let body: Vec<SyncRunResponse> = rows
                .into_iter()
                .map(|(id, sync_def_id, status, started_at, finished_at, bytes_written, files_written, error, ingest_job_id, dataset_version_id, content_hash)| {
                    SyncRunResponse {
                        id, sync_def_id, status, started_at, finished_at, bytes_written, files_written, error, ingest_job_id, dataset_version_id, content_hash,
                    }
                })
                .collect();
            (StatusCode::OK, Json(body)).into_response()
        }
        Err(err) => internal(err),
    }
}

fn internal(err: sqlx::Error) -> axum::response::Response {
    tracing::error!(?err, "data_connection handler db error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(serde_json::json!({ "error": err.to_string() })),
    )
        .into_response()
}
