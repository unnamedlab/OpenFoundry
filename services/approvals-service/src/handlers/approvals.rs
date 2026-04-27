use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::{
    AppState,
    domain::runtime,
    models::{
        approval::{
            ApprovalDecisionRequest, CreateApprovalRequest, CreateApprovalResponse,
            ListApprovalsQuery, WorkflowApproval,
        },
        execution::WorkflowRun,
    },
};

pub async fn create_approval(
    State(state): State<AppState>,
    Json(body): Json<CreateApprovalRequest>,
) -> impl IntoResponse {
    let existing = sqlx::query_as::<_, WorkflowApproval>(
        r#"SELECT * FROM workflow_approvals
           WHERE workflow_run_id = $1 AND step_id = $2 AND status = 'pending'
           ORDER BY requested_at DESC
           LIMIT 1"#,
    )
    .bind(body.workflow_run_id)
    .bind(&body.step_id)
    .fetch_optional(&state.db)
    .await;

    let approval = match existing {
        Ok(Some(approval)) => {
            return Json(CreateApprovalResponse {
                approval,
                created: false,
            })
            .into_response();
        }
        Ok(None) => match sqlx::query_as::<_, WorkflowApproval>(
            r#"INSERT INTO workflow_approvals (
                   id, workflow_id, workflow_run_id, step_id, title, instructions, assigned_to, payload
               )
               VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
               RETURNING *"#,
        )
        .bind(Uuid::now_v7())
        .bind(body.workflow_id)
        .bind(body.workflow_run_id)
        .bind(&body.step_id)
        .bind(&body.title)
        .bind(&body.instructions)
        .bind(body.assigned_to)
        .bind(&body.payload)
        .fetch_one(&state.db)
        .await
        {
            Ok(approval) => approval,
            Err(error) => {
                tracing::error!("approval creation failed: {error}");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
        },
        Err(error) => {
            tracing::error!("approval lookup before create failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    Json(CreateApprovalResponse {
        approval,
        created: true,
    })
    .into_response()
}

pub async fn list_approvals(
    State(state): State<AppState>,
    Query(params): Query<ListApprovalsQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    let offset = (page - 1) * per_page;

    let approvals = sqlx::query_as::<_, WorkflowApproval>(
        r#"SELECT * FROM workflow_approvals
           WHERE ($1::TEXT IS NULL OR status = $1)
             AND ($2::UUID IS NULL OR assigned_to = $2)
             AND ($3::UUID IS NULL OR workflow_id = $3)
           ORDER BY requested_at DESC
           LIMIT $4 OFFSET $5"#,
    )
    .bind(&params.status)
    .bind(params.assigned_to)
    .bind(params.workflow_id)
    .bind(per_page)
    .bind(offset)
    .fetch_all(&state.db)
    .await;

    let total = sqlx::query_scalar::<_, i64>(
        r#"SELECT COUNT(*) FROM workflow_approvals
           WHERE ($1::TEXT IS NULL OR status = $1)
             AND ($2::UUID IS NULL OR assigned_to = $2)
             AND ($3::UUID IS NULL OR workflow_id = $3)"#,
    )
    .bind(&params.status)
    .bind(params.assigned_to)
    .bind(params.workflow_id)
    .fetch_one(&state.db)
    .await
    .unwrap_or(0);

    match approvals {
        Ok(data) => Json(serde_json::json!({
            "data": data,
            "page": page,
            "per_page": per_page,
            "total": total,
        }))
        .into_response(),
        Err(error) => {
            tracing::error!("list approvals failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn decide_approval(
    State(state): State<AppState>,
    Path(approval_id): Path<Uuid>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Json(body): Json<ApprovalDecisionRequest>,
) -> impl IntoResponse {
    let approval =
        sqlx::query_as::<_, WorkflowApproval>(r#"SELECT * FROM workflow_approvals WHERE id = $1"#)
            .bind(approval_id)
            .fetch_optional(&state.db)
            .await;

    let Some(approval) = (match approval {
        Ok(approval) => approval,
        Err(error) => {
            tracing::error!("approval lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    }) else {
        return StatusCode::NOT_FOUND.into_response();
    };

    if approval.status != "pending" {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "approval is not pending" })),
        )
            .into_response();
    }

    let normalized_decision = if body.decision.eq_ignore_ascii_case("approved") {
        "approved"
    } else {
        "rejected"
    };
    let reviewed_payload = runtime::upsert_approval_review_payload(
        &approval.payload,
        normalized_decision,
        claims.sub,
        body.comment.as_deref(),
        &body.payload,
    );

    let mut updated_approval = match sqlx::query_as::<_, WorkflowApproval>(
        r#"UPDATE workflow_approvals
           SET status = CASE WHEN LOWER($2) = 'approved' THEN 'approved' ELSE 'rejected' END,
               decision = $2,
               payload = $3,
               decided_at = NOW(),
               decided_by = $4
           WHERE id = $1
           RETURNING *"#,
    )
    .bind(approval_id)
    .bind(normalized_decision)
    .bind(&reviewed_payload)
    .bind(claims.sub)
    .fetch_one(&state.db)
    .await
    {
        Ok(updated_approval) => updated_approval,
        Err(error) => {
            tracing::error!("approval update failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let run = match sqlx::query_as::<_, WorkflowRun>(r#"SELECT * FROM workflow_runs WHERE id = $1"#)
        .bind(updated_approval.workflow_run_id)
        .fetch_one(&state.db)
        .await
    {
        Ok(run) => run,
        Err(error) => {
            tracing::error!("run lookup for approval failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let mut context = run.context.clone();
    runtime::insert_approval_decision(
        &mut context,
        &updated_approval.step_id,
        normalized_decision,
        claims.sub,
        &body.payload,
        body.comment.as_deref(),
    );

    if normalized_decision == "approved" && updated_approval.payload.get("proposal").is_some() {
        match runtime::apply_approval_proposal(
            &state,
            &mut context,
            &updated_approval.step_id,
            &updated_approval.payload,
            &claims,
        )
        .await
        {
            Ok(response) => {
                let execution_payload = if response.is_null() {
                    runtime::annotate_approval_proposal_execution(
                        &updated_approval.payload,
                        "approved_pending_manual_apply",
                        None,
                        None,
                    )
                } else {
                    runtime::annotate_approval_proposal_execution(
                        &updated_approval.payload,
                        "applied",
                        Some(&response),
                        None,
                    )
                };

                match sqlx::query_as::<_, WorkflowApproval>(
                    r#"UPDATE workflow_approvals
                       SET payload = $2
                       WHERE id = $1
                       RETURNING *"#,
                )
                .bind(updated_approval.id)
                .bind(&execution_payload)
                .fetch_one(&state.db)
                .await
                {
                    Ok(reloaded_approval) => updated_approval = reloaded_approval,
                    Err(error) => {
                        tracing::error!("approval execution payload update failed: {error}");
                        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
                    }
                }
            }
            Err(error) => {
                let failed_payload = runtime::annotate_approval_proposal_execution(
                    &updated_approval.payload,
                    "failed",
                    None,
                    Some(&error),
                );
                let _ = sqlx::query_as::<_, WorkflowApproval>(
                    r#"UPDATE workflow_approvals
                       SET payload = $2
                       WHERE id = $1
                       RETURNING *"#,
                )
                .bind(updated_approval.id)
                .bind(&failed_payload)
                .fetch_one(&state.db)
                .await;

                let _ = runtime::fail_run(&state, run.id, &context, error.clone()).await;
                return (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({ "error": error })),
                )
                    .into_response();
            }
        }
    }

    if normalized_decision == "rejected" && updated_approval.payload.get("proposal").is_some() {
        let rejected_payload = runtime::annotate_approval_proposal_execution(
            &updated_approval.payload,
            "rejected",
            None,
            None,
        );

        match sqlx::query_as::<_, WorkflowApproval>(
            r#"UPDATE workflow_approvals
               SET payload = $2
               WHERE id = $1
               RETURNING *"#,
        )
        .bind(updated_approval.id)
        .bind(&rejected_payload)
        .fetch_one(&state.db)
        .await
        {
            Ok(reloaded_approval) => updated_approval = reloaded_approval,
            Err(error) => {
                tracing::error!("approval rejection payload update failed: {error}");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
        }
    }

    match runtime::continue_workflow_after_approval(
        &state,
        &updated_approval,
        normalized_decision,
        &context,
    )
    .await
    {
        Ok(run) => Json(serde_json::json!({
            "approval": updated_approval,
            "run": run,
        }))
        .into_response(),
        Err(error) => {
            tracing::error!("continue after approval failed: {error}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": error })),
            )
                .into_response()
        }
    }
}
