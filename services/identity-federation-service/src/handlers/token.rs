use axum::{
    Json,
    extract::State,
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
};
use serde::Deserialize;

use crate::AppState;
use crate::domain::jwt::{
    get_refresh_token, issue_tokens, refresh_token_matches, revoke_refresh_token,
};
use crate::handlers::common::{client_ip, rate_limited};
use crate::handlers::login::TokenResponse;
use crate::hardening::audit_topic::{IdentityAuditEvent, correlation_id_from_headers};
use crate::hardening::rate_limit::{LimitConfig, RateLimitDecision, key as rate_limit_key};
use crate::models::user::User;

#[derive(Debug, Deserialize)]
pub struct RefreshRequest {
    pub refresh_token: String,
}

pub async fn refresh(
    State(state): State<AppState>,
    headers: HeaderMap,
    Json(body): Json<RefreshRequest>,
) -> impl IntoResponse {
    let correlation_id = correlation_id_from_headers(&headers);
    let ip = client_ip(&headers);
    match state
        .rate_limiter
        .check(
            &rate_limit_key("", &ip, "/auth/token/refresh"),
            &LimitConfig::OAUTH_TOKEN,
        )
        .await
    {
        RateLimitDecision::Allow => {}
        RateLimitDecision::Deny { retry_after_secs } => {
            return rate_limited(retry_after_secs);
        }
    }

    // Decode the refresh token
    let claims = match auth_middleware::jwt::decode_token(&state.jwt_config, &body.refresh_token) {
        Ok(c) if c.token_use.as_deref() == Some("refresh") => c,
        Err(_) => {
            return (
                StatusCode::UNAUTHORIZED,
                Json(serde_json::json!({ "error": "invalid refresh token" })),
            )
                .into_response();
        }
        _ => {
            return (
                StatusCode::UNAUTHORIZED,
                Json(serde_json::json!({ "error": "invalid refresh token" })),
            )
                .into_response();
        }
    };

    let Some(stored_token) = (match get_refresh_token(&state.sessions, claims.jti).await {
        Ok(record) => record,
        Err(e) => {
            tracing::error!("failed to load refresh token: {e}");
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "token refresh failed" })),
            )
                .into_response();
        }
    }) else {
        return (
            StatusCode::UNAUTHORIZED,
            Json(serde_json::json!({ "error": "refresh token revoked" })),
        )
            .into_response();
    };

    if !refresh_token_matches(&stored_token, &body.refresh_token) {
        if let Err(error) = state
            .audit
            .record(
                correlation_id,
                Some(stored_token.user_id.to_string()),
                IdentityAuditEvent::RefreshTokenReplay {
                    user_id: stored_token.user_id.to_string(),
                    family_id: claims.jti,
                },
            )
            .await
        {
            tracing::error!(%error, "blocking audit failure during refresh replay");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
        return (
            StatusCode::UNAUTHORIZED,
            Json(serde_json::json!({ "error": "refresh token revoked" })),
        )
            .into_response();
    }

    // Fetch the user
    let user = sqlx::query_as::<_, User>(
        "SELECT id, email, name, password_hash, is_active, organization_id, attributes, mfa_enforced, auth_source, created_at, updated_at FROM users WHERE id = $1 AND is_active = true",
    )
    .bind(claims.sub)
    .fetch_optional(&state.db)
    .await;

    let user = match user {
        Ok(Some(u)) => u,
        _ => {
            return (
                StatusCode::UNAUTHORIZED,
                Json(serde_json::json!({ "error": "user not found or disabled" })),
            )
                .into_response();
        }
    };

    if let Err(e) = revoke_refresh_token(&state.sessions, claims.jti).await {
        tracing::error!("failed to revoke refresh token: {e}");
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({ "error": "token refresh failed" })),
        )
            .into_response();
    }

    match issue_tokens(
        &state.db,
        &state.sessions,
        &state.jwt_config,
        state.jwks.as_ref(),
        &user,
        vec!["refresh".to_string()],
    )
    .await
    {
        Ok((access_token, refresh_token)) => Json(TokenResponse {
            access_token,
            refresh_token,
            token_type: "Bearer".to_string(),
            expires_in: state.jwt_config.access_ttl_secs,
        })
        .into_response(),
        Err(e) => {
            tracing::error!("token refresh failed: {e}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": "token refresh failed" })),
            )
                .into_response()
        }
    }
}
