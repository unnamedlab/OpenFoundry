//! S3.3 revocation handlers.
//!
//! Three endpoints land here:
//!
//! * `POST /governance/sessions/{session_id}/revoke` — write a
//!   single-session revocation row to `auth_runtime.session_revocation`
//!   (TTL 1800 s, aligned with `USER_SESSION_TTL_SECS`). Optionally
//!   accepts a `reason` plus an explicit `user_id` when the session
//!   is not registered as a scoped session in
//!   `auth_runtime.scoped_session_by_id`.
//! * `POST /governance/users/{user_id}/revoke` — fan-out: enumerate
//!   the user's scoped sessions and revoke each one through the
//!   single-session pipeline, ensuring the fan-out partition in
//!   `auth_runtime.user_revocation` and the per-session row in
//!   `auth_runtime.session_revocation` both land.
//! * `GET /governance/sessions/{session_id}/status` — direct
//!   single-partition lookup against `session_revocation`.
//!
//! All three require the caller to either hold the
//! `admin:session-governor` role (Cedar policy
//! [`policies/session_governor.cedar`](../../../policies/session_governor.cedar))
//! or the platform `admin` role.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use session_governance_service::revocation_cassandra::RevocationReason;
use uuid::Uuid;

use crate::AppState;
use crate::common::json_error;

/// Role name asserted by the auth gate. Mirrors the Cedar policy in
/// `policies/session_governor.cedar`.
pub const SESSION_GOVERNOR_ROLE: &str = "admin:session-governor";

/// Default revocation reason applied when the request body omits one.
/// Maps to the `admin_action` enum value persisted in the audit row.
const DEFAULT_REASON: &str = "admin_action";

#[derive(Debug, Default, Deserialize)]
#[serde(default, deny_unknown_fields)]
pub struct RevokeSessionRequest {
    pub reason: Option<String>,
    /// Override for cases where the session is not registered in the
    /// scoped-session tables (e.g. plain access sessions).
    pub user_id: Option<Uuid>,
}

#[derive(Debug, Serialize)]
pub struct RevokeSessionResponse {
    pub session_id: Uuid,
    pub user_id: String,
    pub reason: String,
    pub revoked_at: i64,
}

#[derive(Debug, Serialize)]
pub struct RevokeUserSessionsResponse {
    pub user_id: Uuid,
    pub revoked_session_ids: Vec<Uuid>,
    pub reason: String,
    pub revoked_at: i64,
}

#[derive(Debug, Serialize)]
pub struct SessionStatusResponse {
    pub session_id: Uuid,
    pub revoked: bool,
}

/// Reject callers that do not hold the governance role. Admin keeps
/// access for break-glass; the Cedar policy carries the parallel
/// `forbid` for consumer principals.
fn enforce_session_governor(claims: &auth_middleware::Claims) -> Result<(), Response> {
    if claims.has_role(SESSION_GOVERNOR_ROLE) || claims.has_role("admin") {
        Ok(())
    } else {
        Err(json_error(
            StatusCode::FORBIDDEN,
            format!("missing role {SESSION_GOVERNOR_ROLE}"),
        ))
    }
}

fn parse_reason(value: Option<&str>) -> Result<RevocationReason, Response> {
    match value.unwrap_or(DEFAULT_REASON) {
        "user_logout" => Ok(RevocationReason::UserLogout),
        "admin_action" => Ok(RevocationReason::AdminAction),
        "suspected_compromise" => Ok(RevocationReason::SuspectedCompromise),
        "refresh_token_replay" => Ok(RevocationReason::RefreshTokenReplay),
        "policy_violation" => Ok(RevocationReason::PolicyViolation),
        other => Err(json_error(
            StatusCode::BAD_REQUEST,
            format!("unsupported revocation reason: {other}"),
        )),
    }
}

pub async fn revoke_session(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(session_id): Path<Uuid>,
    body: Option<Json<RevokeSessionRequest>>,
) -> Response {
    if let Err(response) = enforce_session_governor(&claims) {
        return response;
    }
    let Json(body) = body.unwrap_or_default();
    let reason = match parse_reason(body.reason.as_deref()) {
        Ok(reason) => reason,
        Err(response) => return response,
    };

    let user_id_text = match resolve_user_id(&state, session_id, body.user_id).await {
        Ok(value) => value,
        Err(response) => return response,
    };
    let revoked_at = Utc::now().timestamp();

    if let Err(error) = state
        .revocation
        .revoke_session(&user_id_text, session_id, reason, revoked_at)
        .await
    {
        tracing::error!(?error, %session_id, "failed to write revocation row");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    if let Err(error) = state
        .sessions
        .revoke_scoped_session(session_id, claims.sub, true)
        .await
    {
        tracing::warn!(?error, %session_id, "scoped-session mirror update failed; continuing");
    }

    Json(RevokeSessionResponse {
        session_id,
        user_id: user_id_text,
        reason: reason.as_str().to_string(),
        revoked_at,
    })
    .into_response()
}

pub async fn revoke_user_sessions(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(user_id): Path<Uuid>,
    body: Option<Json<RevokeSessionRequest>>,
) -> Response {
    if let Err(response) = enforce_session_governor(&claims) {
        return response;
    }
    let Json(body) = body.unwrap_or_default();
    let reason = match parse_reason(body.reason.as_deref()) {
        Ok(reason) => reason,
        Err(response) => return response,
    };

    let scoped_sessions = match state.sessions.list_scoped_sessions(user_id).await {
        Ok(rows) => rows,
        Err(error) => {
            tracing::error!(?error, %user_id, "failed to list scoped sessions for user revocation");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let revoked_at = Utc::now().timestamp();
    let user_id_text = user_id.to_string();
    let mut revoked = Vec::with_capacity(scoped_sessions.len());

    for record in scoped_sessions {
        if record.revoked_at.is_some() {
            continue;
        }
        if let Err(error) = state
            .revocation
            .revoke_session(&user_id_text, record.id, reason, revoked_at)
            .await
        {
            tracing::error!(?error, session_id = %record.id, "failed to write revocation row");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
        if let Err(error) = state
            .sessions
            .revoke_scoped_session(record.id, user_id, true)
            .await
        {
            tracing::warn!(
                ?error,
                session_id = %record.id,
                "scoped-session mirror update failed during user fan-out"
            );
        }
        revoked.push(record.id);
    }

    Json(RevokeUserSessionsResponse {
        user_id,
        revoked_session_ids: revoked,
        reason: reason.as_str().to_string(),
        revoked_at,
    })
    .into_response()
}

pub async fn session_status(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(session_id): Path<Uuid>,
) -> Response {
    if let Err(response) = enforce_session_governor(&claims) {
        return response;
    }

    match state.revocation.is_session_revoked(session_id).await {
        Ok(revoked) => Json(SessionStatusResponse {
            session_id,
            revoked,
        })
        .into_response(),
        Err(error) => {
            tracing::error!(?error, %session_id, "failed to read revocation status");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

async fn resolve_user_id(
    state: &AppState,
    session_id: Uuid,
    body_user_id: Option<Uuid>,
) -> Result<String, Response> {
    if let Some(user_id) = body_user_id {
        return Ok(user_id.to_string());
    }
    match state.sessions.get_scoped_session(session_id).await {
        Ok(Some(record)) => Ok(record.user_id.to_string()),
        Ok(None) => Err(json_error(
            StatusCode::BAD_REQUEST,
            "user_id is required when the session is not registered as scoped",
        )),
        Err(error) => {
            tracing::error!(?error, %session_id, "failed to load session for revocation");
            Err(StatusCode::INTERNAL_SERVER_ERROR.into_response())
        }
    }
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::*;

    fn claims_with_roles(roles: Vec<String>) -> auth_middleware::Claims {
        auth_middleware::Claims {
            sub: Uuid::nil(),
            iat: 0,
            exp: i64::MAX,
            iss: None,
            aud: None,
            jti: Uuid::nil(),
            email: "ops@example.com".to_string(),
            name: "Ops".to_string(),
            roles,
            permissions: vec![],
            org_id: None,
            attributes: json!({}),
            auth_methods: vec!["password".to_string()],
            token_use: Some("access".to_string()),
            api_key_id: None,
            session_kind: None,
            session_scope: None,
        }
    }

    #[test]
    fn governance_role_or_admin_passes_gate() {
        assert!(
            enforce_session_governor(&claims_with_roles(vec![SESSION_GOVERNOR_ROLE.into()]))
                .is_ok()
        );
        assert!(enforce_session_governor(&claims_with_roles(vec!["admin".into()])).is_ok());
    }

    #[test]
    fn unprivileged_caller_gets_forbidden() {
        assert!(enforce_session_governor(&claims_with_roles(vec!["viewer".into()])).is_err());
    }

    #[test]
    fn parse_reason_defaults_to_admin_action() {
        assert_eq!(
            parse_reason(None).expect("default"),
            RevocationReason::AdminAction
        );
        assert_eq!(
            parse_reason(Some("user_logout")).expect("known"),
            RevocationReason::UserLogout
        );
    }

    #[test]
    fn parse_reason_rejects_unknown_values() {
        assert!(parse_reason(Some("nope")).is_err());
    }
}
