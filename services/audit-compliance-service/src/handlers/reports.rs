use auth_middleware::layer::AuthUser;
use axum::{Json, extract::State};

use crate::{
    AppState,
    domain::{export, security},
    handlers::{ServiceResult, db_error, internal_error, load_events, load_reports},
    models::{
        ListResponse,
        compliance_report::{ComplianceReport, ComplianceReportRequest},
    },
};

pub async fn list_reports(
    State(state): State<AppState>,
) -> ServiceResult<ListResponse<ComplianceReport>> {
    let reports = load_reports(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse { items: reports }))
}

pub async fn generate_report(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Json(request): Json<ComplianceReportRequest>,
) -> ServiceResult<ComplianceReport> {
    let events = security::filter_events_for_claims(
        load_events(&state.db)
            .await
            .map_err(|cause| db_error(&cause))?,
        &claims,
    );
    let report = export::build_report(&request, &events, &[]);
    let findings = serde_json::to_value(&report.findings)
        .map_err(|cause| internal_error(cause.to_string()))?;
    let artifact = serde_json::to_value(&report.artifact)
        .map_err(|cause| internal_error(cause.to_string()))?;

    sqlx::query(
		"INSERT INTO compliance_reports (id, standard, title, scope, window_start, window_end, generated_at, status, findings, artifact, relevant_event_count, policy_count, control_summary, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb, $10::jsonb, $11, $12, $13, $14)",
	)
	.bind(report.id)
	.bind(report.standard.as_str())
	.bind(&report.title)
	.bind(&report.scope)
	.bind(report.window_start)
	.bind(report.window_end)
	.bind(report.generated_at)
	.bind(&report.status)
	.bind(findings)
	.bind(artifact)
	.bind(report.relevant_event_count)
	.bind(report.policy_count)
	.bind(&report.control_summary)
	.bind(report.expires_at)
	.execute(&state.db)
	.await
	.map_err(|cause| db_error(&cause))?;

    Ok(Json(report))
}
