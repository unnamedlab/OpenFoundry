//! JobSpec publish / list handlers.
//!
//! Routes wired in `main.rs`:
//!
//! ```text
//!   POST /pipelines/{rid}/branches/{branch}/job-specs
//!   GET  /pipelines/{rid}/branches/{branch}/job-specs
//!   GET  /datasets/{rid}/job-specs?on_branch=master
//! ```
//!
//! The publish path is **idempotent** by content hash: republishing the
//! same `(pipeline_rid, branch, output_dataset_rid, content_hash)`
//! returns the existing row with `new_version = false`. Republishing
//! with a changed payload bumps `version` and overwrites the JSONB
//! columns.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::models::job_spec::{
    JobSpecInput, JobSpecRow, ListByDatasetQuery, ListByPipelineQuery, PublishJobSpecRequest,
    PublishJobSpecResponse, content_hash,
};

/// `POST /pipelines/{pipeline_rid}/branches/{branch}/job-specs`
///
/// Idempotent on `(pipeline_rid, branch, output_dataset_rid,
/// content_hash)`. Returns 200 + `{ new_version: false }` for a no-op
/// republish, 201 + `{ new_version: true }` when a new version is
/// minted (insert or version bump).
pub async fn publish_job_spec(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
    Path((pipeline_rid, branch_name)): Path<(String, String)>,
    Json(body): Json<PublishJobSpecRequest>,
) -> Result<(StatusCode, Json<PublishJobSpecResponse>), (StatusCode, Json<Value>)> {
    let pipeline_rid = pipeline_rid.trim().to_string();
    let branch_name = branch_name.trim().to_string();
    let output_dataset_rid = body.output_dataset_rid.trim().to_string();
    let output_branch = body
        .output_branch
        .as_deref()
        .map(str::trim)
        .filter(|s| !s.is_empty())
        .unwrap_or(&branch_name)
        .to_string();
    if pipeline_rid.is_empty()
        || branch_name.is_empty()
        || output_dataset_rid.is_empty()
        || output_branch.is_empty()
    {
        return Err(bad_request(
            "pipeline_rid, branch, output_dataset_rid and output_branch must be non-empty",
        ));
    }

    let inputs: Vec<JobSpecInput> = body.inputs.into_iter().map(|i| i.into_input()).collect();

    // Reject inputs that don't follow the dataset RID shape so a typo
    // can't slip through publish-time validation.
    for input in &inputs {
        if !input.input.starts_with("ri.foundry.main.dataset.") {
            return Err(bad_request(
                "every input.input must be a dataset RID (ri.foundry.main.dataset.<uuid>)",
            ));
        }
    }

    let hash = content_hash(&body.job_spec_json, &inputs);
    let inputs_json =
        serde_json::to_value(&inputs).map_err(|e| internal(&format!("encode inputs: {e}")))?;

    // Look for an existing row at the unique key. Three branches:
    //  1) None: insert a brand-new row (version = 1, new_version = true).
    //  2) Some, same hash: no-op (return existing, new_version = false).
    //  3) Some, different hash: version bump + overwrite columns.
    let existing = sqlx::query_as::<_, JobSpecRow>(
        r#"SELECT * FROM pipeline_job_specs
            WHERE pipeline_rid = $1 AND branch_name = $2 AND output_dataset_rid = $3
            FOR UPDATE"#,
    )
    .bind(&pipeline_rid)
    .bind(&branch_name)
    .bind(&output_dataset_rid)
    .fetch_optional(&state.db)
    .await
    .map_err(|e| internal(&format!("lookup job spec: {e}")))?;

    let (row, new_version, status) = match existing {
        None => {
            let row = sqlx::query_as::<_, JobSpecRow>(
                r#"INSERT INTO pipeline_job_specs (
                       id, pipeline_rid, branch_name, output_dataset_rid,
                       output_branch, job_spec_json, inputs, content_hash, version,
                       published_by
                   )
                   VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 1, $9)
                   RETURNING *"#,
            )
            .bind(Uuid::now_v7())
            .bind(&pipeline_rid)
            .bind(&branch_name)
            .bind(&output_dataset_rid)
            .bind(&output_branch)
            .bind(&body.job_spec_json)
            .bind(&inputs_json)
            .bind(&hash)
            .bind(claims.sub)
            .fetch_one(&state.db)
            .await
            .map_err(|e| internal(&format!("insert job spec: {e}")))?;
            (row, true, StatusCode::CREATED)
        }
        Some(prev) if prev.content_hash == hash && prev.output_branch == output_branch => {
            (prev, false, StatusCode::OK)
        }
        Some(prev) => {
            let row = sqlx::query_as::<_, JobSpecRow>(
                r#"UPDATE pipeline_job_specs
                      SET output_branch = $2,
                          job_spec_json = $3,
                          inputs        = $4,
                          content_hash  = $5,
                          version       = version + 1,
                          published_by  = $6,
                          published_at  = NOW()
                    WHERE id = $1
                    RETURNING *"#,
            )
            .bind(prev.id)
            .bind(&output_branch)
            .bind(&body.job_spec_json)
            .bind(&inputs_json)
            .bind(&hash)
            .bind(claims.sub)
            .fetch_one(&state.db)
            .await
            .map_err(|e| internal(&format!("update job spec: {e}")))?;
            (row, true, StatusCode::CREATED)
        }
    };

    Ok((
        status,
        Json(PublishJobSpecResponse {
            new_version,
            job_spec: row,
        }),
    ))
}

/// `GET /pipelines/{pipeline_rid}/branches/{branch}/job-specs`
pub async fn list_by_pipeline_branch(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path((pipeline_rid, branch_name)): Path<(String, String)>,
    Query(query): Query<ListByPipelineQuery>,
) -> Result<Json<Vec<JobSpecRow>>, (StatusCode, Json<Value>)> {
    let rows = sqlx::query_as::<_, JobSpecRow>(
        r#"SELECT * FROM pipeline_job_specs
            WHERE pipeline_rid = $1 AND branch_name = $2
              AND ($3::text IS NULL OR output_dataset_rid = $3)
            ORDER BY published_at DESC"#,
    )
    .bind(pipeline_rid.trim())
    .bind(branch_name.trim())
    .bind(query.output_dataset_rid.as_deref().map(str::trim))
    .fetch_all(&state.db)
    .await
    .map_err(|e| internal(&format!("list job specs: {e}")))?;
    Ok(Json(rows))
}

/// `GET /datasets/{rid}/job-specs?on_branch=master`
pub async fn list_by_dataset(
    AuthUser(_claims): AuthUser,
    State(state): State<AppState>,
    Path(dataset_rid): Path<String>,
    Query(query): Query<ListByDatasetQuery>,
) -> Result<Json<Vec<JobSpecRow>>, (StatusCode, Json<Value>)> {
    let rows = sqlx::query_as::<_, JobSpecRow>(
        r#"SELECT * FROM pipeline_job_specs
            WHERE output_dataset_rid = $1
              AND ($2::text IS NULL OR branch_name = $2)
            ORDER BY published_at DESC"#,
    )
    .bind(dataset_rid.trim())
    .bind(query.on_branch.as_deref().map(str::trim))
    .fetch_all(&state.db)
    .await
    .map_err(|e| internal(&format!("list job specs by dataset: {e}")))?;
    Ok(Json(rows))
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
}

fn internal(msg: &str) -> (StatusCode, Json<Value>) {
    tracing::error!(%msg, "pipeline-authoring-service: job_spec internal error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": msg })),
    )
}
