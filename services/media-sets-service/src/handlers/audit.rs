//! Request-context extraction for audit events.
//!
//! The Axum handlers turn the incoming JWT claims and request headers
//! into an [`AuditContext`] that the operation layer threads down to
//! `audit_trail::events::emit` (per ADR-0022, the audit emission lives
//! in the same Postgres transaction as the primary write). Background
//! callers — gRPC service, retention reaper — build the context with
//! [`AuditContext::for_actor`] directly.
//!
//! ## What we extract from a request
//!
//! * `actor_id` — `claims.sub` (the JWT subject).
//! * `ip` — first `X-Forwarded-For` hop, falling back to `X-Real-IP`.
//! * `user_agent` — verbatim from the `User-Agent` header.
//! * `request_id` — `X-Request-Id` if present, otherwise a fresh UUID.
//!   This doubles as the deterministic seed for the outbox `event_id`,
//!   so an at-least-once retry of the same logical request collapses
//!   to one row.
//! * `correlation_id` — `X-Correlation-Id` (OpenLineage `ol-run-id`).

use audit_trail::events::AuditContext;
use auth_middleware::Claims;
use axum::http::HeaderMap;
use uuid::Uuid;

pub const SERVICE_NAME: &str = "media-sets-service";

/// Build an [`AuditContext`] from the authenticated principal and the
/// raw HTTP headers. Latency is captured by the caller — the audit
/// middleware times the *whole* request, but the per-operation latency
/// is what consumers actually want, so we let each handler decide.
pub fn from_request(claims: &Claims, headers: &HeaderMap) -> AuditContext {
    let request_id = header_str(headers, "x-request-id")
        .map(str::to_string)
        .unwrap_or_else(|| Uuid::now_v7().to_string());

    AuditContext {
        actor_id: Some(claims.sub.to_string()),
        ip: client_ip(headers),
        user_agent: header_str(headers, "user-agent").map(str::to_string),
        request_id: Some(request_id),
        correlation_id: header_str(headers, "x-correlation-id").map(str::to_string),
        latency_ms: None,
        source_service: Some(SERVICE_NAME.to_string()),
    }
}

/// Best-effort client IP. Honours `X-Forwarded-For` (first hop) and
/// then `X-Real-IP`; both can be spoofed, but Foundry's audit schema
/// already carries that caveat ("origin can be spoofed", Audit
/// logs.md → Audit.3 schema reference).
fn client_ip(headers: &HeaderMap) -> Option<String> {
    if let Some(xff) = header_str(headers, "x-forwarded-for") {
        if let Some(first) = xff.split(',').next() {
            let trimmed = first.trim();
            if !trimmed.is_empty() {
                return Some(trimmed.to_string());
            }
        }
    }
    header_str(headers, "x-real-ip").map(str::to_string)
}

fn header_str<'a>(headers: &'a HeaderMap, name: &str) -> Option<&'a str> {
    headers.get(name).and_then(|v| v.to_str().ok())
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::HeaderValue;
    use serde_json::Value;

    fn fake_claims(sub: &str) -> Claims {
        Claims {
            sub: Uuid::parse_str(sub).unwrap_or(Uuid::nil()),
            iat: 0,
            exp: i64::MAX,
            iss: None,
            aud: None,
            jti: Uuid::nil(),
            email: "tester@example.test".into(),
            name: "Tester".into(),
            roles: vec![],
            permissions: vec![],
            org_id: None,
            attributes: Value::Null,
            auth_methods: vec![],
            token_use: None,
            api_key_id: None,
            session_kind: None,
            session_scope: None,
        }
    }

    #[test]
    fn xff_first_hop_wins_over_real_ip() {
        let mut headers = HeaderMap::new();
        headers.insert(
            "x-forwarded-for",
            HeaderValue::from_static("10.0.0.5, 1.2.3.4"),
        );
        headers.insert("x-real-ip", HeaderValue::from_static("10.99.99.99"));
        assert_eq!(client_ip(&headers).as_deref(), Some("10.0.0.5"));
    }

    #[test]
    fn falls_back_to_real_ip_when_xff_missing() {
        let mut headers = HeaderMap::new();
        headers.insert("x-real-ip", HeaderValue::from_static("10.99.99.99"));
        assert_eq!(client_ip(&headers).as_deref(), Some("10.99.99.99"));
    }

    #[test]
    fn from_request_synthesises_request_id_when_header_absent() {
        let claims = fake_claims("00000000-0000-7000-8000-000000000001");
        let headers = HeaderMap::new();
        let ctx = from_request(&claims, &headers);
        assert!(
            ctx.request_id.is_some(),
            "should mint a UUID when header is absent"
        );
        assert_eq!(ctx.source_service.as_deref(), Some(SERVICE_NAME));
    }

    #[test]
    fn from_request_preserves_explicit_request_id() {
        let claims = fake_claims("00000000-0000-7000-8000-000000000001");
        let mut headers = HeaderMap::new();
        headers.insert("x-request-id", HeaderValue::from_static("req-abc-123"));
        let ctx = from_request(&claims, &headers);
        assert_eq!(ctx.request_id.as_deref(), Some("req-abc-123"));
    }
}
