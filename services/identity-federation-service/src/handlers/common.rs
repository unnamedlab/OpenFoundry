use crate::domain::rbac;
use crate::models::user::{User, UserResponse};
use auth_middleware::Claims;
use axum::{
    Json,
    http::{HeaderMap, StatusCode, header},
    response::{IntoResponse, Response},
};
use serde_json::json;
use sqlx::PgPool;

pub fn json_error(status: StatusCode, message: impl Into<String>) -> Response {
    (status, Json(json!({ "error": message.into() }))).into_response()
}

pub fn rate_limited(retry_after_secs: u64) -> Response {
    (
        StatusCode::TOO_MANY_REQUESTS,
        [(header::RETRY_AFTER, retry_after_secs.to_string())],
        Json(json!({
            "error": "rate limit exceeded",
            "retry_after_secs": retry_after_secs,
        })),
    )
        .into_response()
}

pub fn client_ip(headers: &HeaderMap) -> String {
    headers
        .get("x-forwarded-for")
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.split(',').next())
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .or_else(|| {
            headers
                .get("x-real-ip")
                .and_then(|value| value.to_str().ok())
                .map(str::trim)
                .filter(|value| !value.is_empty())
        })
        .unwrap_or("unknown")
        .to_string()
}

pub fn require_permission(claims: &Claims, resource: &str, action: &str) -> Result<(), Response> {
    if claims.has_permission(resource, action) {
        Ok(())
    } else {
        Err(json_error(
            StatusCode::FORBIDDEN,
            format!("missing permission {resource}:{action}"),
        ))
    }
}

pub async fn build_user_response(pool: &PgPool, user: User) -> Result<UserResponse, sqlx::Error> {
    let access_bundle = rbac::get_user_access_bundle(pool, user.id)
        .await
        .unwrap_or_default();
    let mfa_enabled = sqlx::query_scalar::<_, bool>(
        "SELECT EXISTS(SELECT 1 FROM user_mfa_totp WHERE user_id = $1 AND enabled = true)",
    )
    .bind(user.id)
    .fetch_one(pool)
    .await
    .unwrap_or(false);

    let mut response = user.into_response(access_bundle.roles);
    response.groups = access_bundle.groups;
    response.permissions = access_bundle.permissions;
    response.mfa_enabled = mfa_enabled;
    Ok(response)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::HeaderValue;

    #[test]
    fn client_ip_prefers_first_forwarded_for() {
        let mut headers = HeaderMap::new();
        headers.insert(
            "x-forwarded-for",
            HeaderValue::from_static("203.0.113.10, 10.0.0.1"),
        );
        headers.insert("x-real-ip", HeaderValue::from_static("198.51.100.7"));
        assert_eq!(client_ip(&headers), "203.0.113.10");
    }

    #[test]
    fn client_ip_falls_back_to_real_ip() {
        let mut headers = HeaderMap::new();
        headers.insert("x-real-ip", HeaderValue::from_static("198.51.100.7"));
        assert_eq!(client_ip(&headers), "198.51.100.7");
    }
}
