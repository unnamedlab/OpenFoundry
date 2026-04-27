pub mod governance;

use axum::{Json, http::StatusCode};
use serde::Serialize;

use crate::models::{
    compliance_report::{ComplianceReport, ComplianceReportRow},
    governance::{GovernanceTemplateApplication, GovernanceTemplateApplicationRow},
    policy_reference::{
        AuthorizationPolicyReference, RestrictedViewReference, RestrictedViewReferenceRow,
    },
};

#[derive(Debug, Serialize)]
pub struct ErrorResponse {
    pub error: String,
}

pub type ServiceResult<T> = Result<Json<T>, (StatusCode, Json<ErrorResponse>)>;

pub fn bad_request(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::BAD_REQUEST,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn forbidden(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::FORBIDDEN,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn not_found(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::NOT_FOUND,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn internal_error(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn db_error(cause: &sqlx::Error) -> (StatusCode, Json<ErrorResponse>) {
    tracing::error!("security-governance-service database error: {cause}");
    internal_error("database operation failed")
}

pub async fn load_template_applications(
    db: &sqlx::PgPool,
) -> Result<Vec<GovernanceTemplateApplication>, sqlx::Error> {
    let rows = sqlx::query_as::<_, GovernanceTemplateApplicationRow>(
        "SELECT id, template_slug, template_name, scope, standards, policy_names, constraint_names, checkpoint_prompts, default_report_standard, applied_by, applied_at, updated_at
         FROM governance_template_applications
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await?;

    rows.into_iter()
        .map(GovernanceTemplateApplication::try_from)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|cause| {
            sqlx::Error::Decode(Box::new(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                cause,
            )))
        })
}

pub async fn load_reports(db: &sqlx::PgPool) -> Result<Vec<ComplianceReport>, sqlx::Error> {
    let rows = sqlx::query_as::<_, ComplianceReportRow>(
        "SELECT id, standard, title, scope, window_start, window_end, generated_at, status, findings, artifact, relevant_event_count, policy_count, control_summary, expires_at
         FROM compliance_reports
         ORDER BY generated_at DESC",
    )
    .fetch_all(db)
    .await?;

    rows.into_iter()
        .map(ComplianceReport::try_from)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|cause| {
            sqlx::Error::Decode(Box::new(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                cause,
            )))
        })
}

pub async fn load_authorization_policies(
    db: &sqlx::PgPool,
) -> Result<Vec<AuthorizationPolicyReference>, sqlx::Error> {
    sqlx::query_as::<_, AuthorizationPolicyReference>(
        "SELECT id, name, resource, action, conditions, enabled, created_at, updated_at
         FROM policies
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await
}

pub async fn load_restricted_views(
    db: &sqlx::PgPool,
) -> Result<Vec<RestrictedViewReference>, sqlx::Error> {
    let rows = sqlx::query_as::<_, RestrictedViewReferenceRow>(
        "SELECT id, name, resource, action, hidden_columns, allowed_markings, enabled
         FROM restricted_views
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await?;

    rows.into_iter()
        .map(RestrictedViewReference::try_from)
        .collect::<Result<Vec<_>, _>>()
        .map_err(|cause| {
            sqlx::Error::Decode(Box::new(std::io::Error::new(
                std::io::ErrorKind::InvalidData,
                cause,
            )))
        })
}
