use axum::{
    Json,
    extract::State,
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
};
use serde::{Deserialize, Serialize};

use crate::AppState;
use crate::domain::jwt::issue_tokens;
use crate::domain::mfa;
use crate::handlers::common::{client_ip, rate_limited};
use crate::hardening::audit_topic::{
    AuditOutcome, IdentityAuditEvent, MfaOutcome, correlation_id_from_headers,
};
use crate::hardening::rate_limit::{LimitConfig, RateLimitDecision, key as rate_limit_key};
use crate::models::mfa::TotpConfiguration;
use crate::models::user::User;

#[derive(Debug, Deserialize)]
pub struct LoginRequest {
    pub email: String,
    pub password: String,
}

#[derive(Debug, Serialize)]
pub struct TokenResponse {
    pub access_token: String,
    pub refresh_token: String,
    pub token_type: String,
    pub expires_in: i64,
}

#[derive(Debug, Serialize)]
#[serde(tag = "status", rename_all = "snake_case")]
pub enum LoginResponse {
    Authenticated {
        access_token: String,
        refresh_token: String,
        token_type: String,
        expires_in: i64,
    },
    MfaRequired {
        challenge_token: String,
        methods: Vec<String>,
        expires_in: i64,
    },
}

pub async fn login(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(body): Json<LoginRequest>,
) -> impl IntoResponse {
    let correlation_id = correlation_id_from_headers(&headers);
    let ip = client_ip(&headers);
    match state
        .rate_limiter
        .check(
            &rate_limit_key(&body.email, &ip, "/auth/login"),
            &LimitConfig::LOGIN,
        )
        .await
    {
        RateLimitDecision::Allow => {}
        RateLimitDecision::Deny { retry_after_secs } => {
            return rate_limited(retry_after_secs);
        }
    }

    let user = sqlx::query_as::<_, User>(
        "SELECT id, email, name, password_hash, is_active, organization_id, attributes, mfa_enforced, auth_source, created_at, updated_at FROM users WHERE email = $1",
    )
    .bind(&body.email)
    .fetch_optional(&state.db)
    .await;

    let user = match user {
        Ok(Some(u)) if u.is_active => u,
        Ok(Some(_)) => {
            if let Err(error) = state
                .audit
                .record(
                    correlation_id,
                    None,
                    IdentityAuditEvent::Login {
                        user_id: body.email.clone(),
                        ip: ip.clone(),
                        method: "password".into(),
                        outcome: AuditOutcome::Failure,
                    },
                )
                .await
            {
                tracing::error!(%error, "blocking audit failure during disabled-account login");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
            return (
                StatusCode::FORBIDDEN,
                Json(serde_json::json!({ "error": "account disabled" })),
            )
                .into_response();
        }
        _ => {
            if let Err(error) = state
                .audit
                .record(
                    correlation_id,
                    None,
                    IdentityAuditEvent::Login {
                        user_id: body.email.clone(),
                        ip: ip.clone(),
                        method: "password".into(),
                        outcome: AuditOutcome::Failure,
                    },
                )
                .await
            {
                tracing::error!(%error, "blocking audit failure during invalid login");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
            return (
                StatusCode::UNAUTHORIZED,
                Json(serde_json::json!({ "error": "invalid credentials" })),
            )
                .into_response();
        }
    };

    if !verify_password(&body.password, &user.password_hash) {
        if let Err(error) = state
            .audit
            .record(
                correlation_id,
                Some(user.id.to_string()),
                IdentityAuditEvent::Login {
                    user_id: user.id.to_string(),
                    ip: ip.clone(),
                    method: "password".into(),
                    outcome: AuditOutcome::Failure,
                },
            )
            .await
        {
            tracing::error!(%error, "blocking audit failure during password failure");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
        return (
            StatusCode::UNAUTHORIZED,
            Json(serde_json::json!({ "error": "invalid credentials" })),
        )
            .into_response();
    }

    let mfa_configuration = sqlx::query_as::<_, TotpConfiguration>(
        "SELECT user_id, secret, recovery_code_hashes, enabled, verified_at, created_at, updated_at FROM user_mfa_totp WHERE user_id = $1",
    )
    .bind(user.id)
    .fetch_optional(&state.db)
    .await;

    let mfa_configuration = match mfa_configuration {
        Ok(config) => config,
        Err(e) => {
            tracing::error!("failed to load MFA configuration: {e}");
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "login failed" })),
            )
                .into_response();
        }
    };

    let webauthn_configured = match state.webauthn.has_credentials(user.id).await {
        Ok(configured) => configured,
        Err(error) => {
            tracing::error!("failed to load WebAuthn credentials: {error}");
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "login failed" })),
            )
                .into_response();
        }
    };

    let totp_enabled = mfa_configuration
        .as_ref()
        .map(|configuration| configuration.enabled)
        .unwrap_or(false);
    if totp_enabled || webauthn_configured {
        let mut methods = Vec::new();
        if totp_enabled {
            methods.push("totp".to_string());
        }
        if webauthn_configured {
            methods.push("webauthn".to_string());
        }
        if let Err(error) = state
            .audit
            .record(
                correlation_id,
                Some(user.id.to_string()),
                IdentityAuditEvent::MfaChallenge {
                    user_id: user.id.to_string(),
                    factor: methods.join(","),
                    outcome: MfaOutcome::Pass,
                },
            )
            .await
        {
            tracing::error!(%error, "blocking audit failure during MFA challenge");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
        return match mfa::issue_challenge(&state.jwt_config, &user, "password") {
            Ok(challenge_token) => Json(LoginResponse::MfaRequired {
                challenge_token,
                methods,
                expires_in: 300,
            })
            .into_response(),
            Err(e) => {
                tracing::error!("failed to issue MFA challenge: {e}");
                (
                    StatusCode::INTERNAL_SERVER_ERROR,
                    Json(serde_json::json!({ "error": "login failed" })),
                )
                    .into_response()
            }
        };
    }

    if user.mfa_enforced {
        return (
            StatusCode::FORBIDDEN,
            Json(serde_json::json!({ "error": "mfa setup required by administrator" })),
        )
            .into_response();
    }

    match issue_tokens(
        &state.db,
        &state.sessions,
        &state.jwt_config,
        state.jwks.as_ref(),
        &user,
        vec!["password".to_string()],
    )
    .await
    {
        Ok((access_token, refresh_token)) => {
            if let Err(error) = state
                .audit
                .record(
                    correlation_id,
                    Some(user.id.to_string()),
                    IdentityAuditEvent::Login {
                        user_id: user.id.to_string(),
                        ip,
                        method: "password".into(),
                        outcome: AuditOutcome::Success,
                    },
                )
                .await
            {
                tracing::error!(%error, "blocking audit failure during login success");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
            if let Err(error) = state
                .audit
                .record(
                    correlation_id,
                    Some(user.id.to_string()),
                    IdentityAuditEvent::SessionIssued {
                        user_id: user.id.to_string(),
                        session_id: None,
                        method: "password".into(),
                    },
                )
                .await
            {
                tracing::error!(%error, "blocking audit failure during session issue");
                return StatusCode::INTERNAL_SERVER_ERROR.into_response();
            }
            Json(LoginResponse::Authenticated {
                access_token,
                refresh_token,
                token_type: "Bearer".to_string(),
                expires_in: state.jwt_config.access_ttl_secs,
            })
            .into_response()
        }
        Err(e) => {
            tracing::error!("token generation failed: {e}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "token generation failed" })),
            )
                .into_response()
        }
    }
}

fn verify_password(password: &str, hash: &str) -> bool {
    use argon2::PasswordHash;
    use argon2::{Argon2, PasswordVerifier};

    let parsed = match PasswordHash::new(hash) {
        Ok(h) => h,
        Err(_) => return false,
    };
    Argon2::default()
        .verify_password(password.as_bytes(), &parsed)
        .is_ok()
}
