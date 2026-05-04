//! FASE 3 / Tarea 3.4 — HTTP surface that submits SparkApplication
//! CRs and polls their status.
//!
//! Routes:
//! * `POST /api/v1/pipeline/builds/run` — render + create the CR via
//!   [`crate::spark::submit_pipeline_run`], then persist
//!   `(pipeline_run_id, namespace, spark_app_name, status='SUBMITTED')`
//!   in `pipeline_run_submissions`.
//! * `GET  /api/v1/pipeline/builds/{run_id}/status` — look up the
//!   `(namespace, spark_app_name)` for `run_id`, refresh the row from
//!   the cluster via [`crate::spark::get_pipeline_run_status`], and
//!   return the latest snapshot to the caller.
//!
//! The Kubernetes client lives in [`crate::AppState::kube_client`].
//! When it is `None` (typical for `cargo test` without a kubeconfig)
//! both handlers respond with `503 Service Unavailable` — same
//! contract as the existing `lifecycle_ports` plumbing in
//! `handlers/builds_v1.rs`.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};
use serde_json::json;
use uuid::Uuid;

use crate::AppState;
use crate::spark::{
    self, PipelineRunInput, SparkApplicationType, SparkResourceOverrides, SparkRunStatus,
};

#[derive(Debug, Deserialize)]
pub struct SubmitPipelineRunRequest {
    /// UUID the caller wants to identify this run by. If omitted the
    /// handler generates a fresh UUID v4.
    pub pipeline_run_id: Option<Uuid>,
    pub pipeline_id: String,
    /// Optional human-friendly suffix used in the SparkApplication
    /// name. Defaults to the first 12 chars of `pipeline_run_id`.
    pub run_id: Option<String>,
    pub input_dataset_rid: String,
    pub output_dataset_rid: String,
    /// Defaults to [`SparkApplicationType::Scala`].
    pub application_type: Option<SparkApplicationType>,
    pub pipeline_runner_image: Option<String>,
    pub resources: Option<SparkResourceOverrides>,
}

#[derive(Debug, Serialize)]
pub struct SubmitPipelineRunResponse {
    pub pipeline_run_id: Uuid,
    pub namespace: String,
    pub spark_app_name: String,
    pub status: &'static str,
}

#[derive(Debug, Serialize)]
pub struct PipelineRunStatusResponse {
    pub pipeline_run_id: Uuid,
    pub namespace: String,
    pub spark_app_name: String,
    pub status: &'static str,
    pub error_message: Option<String>,
}

// ---------------------------------------------------------------------------
// POST /api/v1/pipeline/builds/run
// ---------------------------------------------------------------------------

pub async fn submit_pipeline_run(
    State(state): State<AppState>,
    Json(body): Json<SubmitPipelineRunRequest>,
) -> impl IntoResponse {
    let Some(client) = state.kube_client.clone() else {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            "kubernetes client not configured",
        )
            .into_response();
    };

    let pipeline_run_id = body.pipeline_run_id.unwrap_or_else(Uuid::new_v4);
    let run_id = body.run_id.clone().unwrap_or_else(|| {
        // Short stable suffix derived from the UUID — keeps the
        // `pipeline-run-${pipeline_id}-${run_id}` composite name
        // within the 50-char budget enforced by `spark::spark_app_name`.
        pipeline_run_id
            .simple()
            .to_string()
            .chars()
            .take(12)
            .collect()
    });
    let image = body
        .pipeline_runner_image
        .clone()
        .unwrap_or_else(|| state.pipeline_runner_image.clone());
    let app_type = body.application_type.unwrap_or(SparkApplicationType::Scala);

    let input = PipelineRunInput {
        pipeline_id: body.pipeline_id,
        run_id,
        namespace: state.spark_namespace.clone(),
        application_type: app_type,
        pipeline_runner_image: image,
        input_dataset_rid: body.input_dataset_rid,
        output_dataset_rid: body.output_dataset_rid,
        resources: body.resources.unwrap_or_default(),
    };

    let spark_app_name = match spark::submit_pipeline_run(client, &input).await {
        Ok(name) => name,
        Err(err) => return spark_error_response(err),
    };

    if let Err(err) = sqlx::query(
        r#"
        INSERT INTO pipeline_run_submissions
            (pipeline_run_id, spark_app_name, namespace, status)
        VALUES ($1, $2, $3, 'SUBMITTED')
        ON CONFLICT (pipeline_run_id) DO UPDATE
        SET spark_app_name   = EXCLUDED.spark_app_name,
            namespace        = EXCLUDED.namespace,
            status           = 'SUBMITTED',
            error_message    = NULL,
            submitted_at     = NOW(),
            last_observed_at = NOW()
        "#,
    )
    .bind(pipeline_run_id)
    .bind(&spark_app_name)
    .bind(&input.namespace)
    .execute(&state.db)
    .await
    {
        tracing::error!(error = %err, "failed to persist pipeline_run_submissions row");
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            format!("persist submission: {err}"),
        )
            .into_response();
    }

    (
        StatusCode::ACCEPTED,
        Json(SubmitPipelineRunResponse {
            pipeline_run_id,
            namespace: input.namespace,
            spark_app_name,
            status: SparkRunStatus::Submitted.as_str(),
        }),
    )
        .into_response()
}

// ---------------------------------------------------------------------------
// GET /api/v1/pipeline/builds/{run_id}/status
// ---------------------------------------------------------------------------

pub async fn get_pipeline_run_status(
    State(state): State<AppState>,
    Path(run_id): Path<Uuid>,
) -> impl IntoResponse {
    let Some(client) = state.kube_client.clone() else {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            "kubernetes client not configured",
        )
            .into_response();
    };

    let row: Option<(String, String, String, Option<String>)> = match sqlx::query_as(
        r#"
        SELECT spark_app_name, namespace, status, error_message
        FROM pipeline_run_submissions
        WHERE pipeline_run_id = $1
        "#,
    )
    .bind(run_id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(row) => row,
        Err(err) => {
            tracing::error!(error = %err, "lookup pipeline_run_submissions failed");
            return (StatusCode::INTERNAL_SERVER_ERROR, err.to_string()).into_response();
        }
    };

    let Some((spark_app_name, namespace, persisted_status, persisted_err)) = row else {
        return (StatusCode::NOT_FOUND, "unknown pipeline_run_id").into_response();
    };

    match spark::get_pipeline_run_status(client, &namespace, &spark_app_name).await {
        Ok(Some(report)) => {
            // Refresh the persisted row so `last_observed_at` and the
            // status mirror the cluster. Use the stored value as a
            // fallback if the UPDATE fails — the caller still gets a
            // useful answer.
            let updated = sqlx::query(
                r#"
                UPDATE pipeline_run_submissions
                   SET status           = $2,
                       error_message    = $3,
                       last_observed_at = NOW()
                 WHERE pipeline_run_id  = $1
                "#,
            )
            .bind(run_id)
            .bind(report.status.as_str())
            .bind(report.error_message.clone())
            .execute(&state.db)
            .await;
            if let Err(err) = updated {
                tracing::warn!(error = %err, "refresh pipeline_run_submissions failed");
            }

            Json(PipelineRunStatusResponse {
                pipeline_run_id: run_id,
                namespace,
                spark_app_name,
                status: report.status.as_str(),
                error_message: report.error_message,
            })
            .into_response()
        }
        Ok(None) => (
            StatusCode::OK,
            Json(json!({
                "pipeline_run_id": run_id,
                "namespace": namespace,
                "spark_app_name": spark_app_name,
                "status": persisted_status,
                "error_message": persisted_err,
                "note": "SparkApplication CR no longer present in cluster",
            })),
        )
            .into_response(),
        Err(err) => spark_error_response(err),
    }
}

fn spark_error_response(err: spark::SparkSubmitError) -> axum::response::Response {
    let status = match &err {
        spark::SparkSubmitError::InvalidInput(_) => StatusCode::BAD_REQUEST,
        spark::SparkSubmitError::Render(_) => StatusCode::INTERNAL_SERVER_ERROR,
        spark::SparkSubmitError::Kube(_) => StatusCode::BAD_GATEWAY,
    };
    tracing::error!(error = %err, "spark submission failed");
    (status, err.to_string()).into_response()
}
