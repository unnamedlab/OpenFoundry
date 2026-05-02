//! `X-Consistency` HTTP header → [`ReadConsistency`] (S1.5.c).
//!
//! Header values:
//!
//! | Value      | Mapped consistency             | Cassandra CL  |
//! |------------|--------------------------------|---------------|
//! | `strong`   | [`ReadConsistency::Strong`]    | `LOCAL_QUORUM`|
//! | `eventual` | [`ReadConsistency::Eventual`]  | `LOCAL_ONE`   |
//!
//! Anything else (including missing) defaults to **Strong**, matching
//! the trait-level `Default` impl. Unknown values trigger a
//! `400 Bad Request` so callers learn about typos rather than silently
//! getting stale reads.

use axum::extract::FromRequestParts;
use axum::http::{StatusCode, request::Parts};
use storage_abstraction::repositories::ReadConsistency;

/// Header name. Public so tests and the router middleware can refer
/// to a single source of truth.
pub const HEADER: &str = "X-Consistency";

/// Wrapper extractor. Use as `ConsistencyHint(c): ConsistencyHint`
/// in handler signatures.
#[derive(Debug, Clone, Copy)]
pub struct ConsistencyHint(pub ReadConsistency);

impl Default for ConsistencyHint {
    fn default() -> Self {
        Self(ReadConsistency::Strong)
    }
}

impl<S: Send + Sync> FromRequestParts<S> for ConsistencyHint {
    type Rejection = (StatusCode, String);

    async fn from_request_parts(parts: &mut Parts, _state: &S) -> Result<Self, Self::Rejection> {
        let Some(raw) = parts.headers.get(HEADER) else {
            return Ok(Self::default());
        };
        let value = raw.to_str().map_err(|_| {
            (
                StatusCode::BAD_REQUEST,
                format!("{HEADER} header is not valid ASCII"),
            )
        })?;
        match value.trim().to_ascii_lowercase().as_str() {
            "strong" => Ok(Self(ReadConsistency::Strong)),
            "eventual" => Ok(Self(ReadConsistency::Eventual)),
            other => Err((
                StatusCode::BAD_REQUEST,
                format!("{HEADER} must be `strong` or `eventual`, got `{other}`"),
            )),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::Request;

    async fn extract(value: Option<&str>) -> Result<ConsistencyHint, (StatusCode, String)> {
        let mut req = Request::builder().uri("/").body(()).unwrap();
        if let Some(v) = value {
            req.headers_mut().insert(HEADER, v.parse().unwrap());
        }
        let (mut parts, _) = req.into_parts();
        ConsistencyHint::from_request_parts(&mut parts, &()).await
    }

    #[tokio::test]
    async fn defaults_to_strong_when_absent() {
        let hint = extract(None).await.unwrap();
        assert_eq!(hint.0, ReadConsistency::Strong);
    }

    #[tokio::test]
    async fn parses_known_values_case_insensitively() {
        for (raw, expected) in [
            ("strong", ReadConsistency::Strong),
            ("STRONG", ReadConsistency::Strong),
            ("Eventual", ReadConsistency::Eventual),
            ("  eventual ", ReadConsistency::Eventual),
        ] {
            let hint = extract(Some(raw)).await.unwrap();
            assert_eq!(hint.0, expected, "raw={raw}");
        }
    }

    #[tokio::test]
    async fn rejects_unknown_value() {
        let err = extract(Some("bounded")).await.unwrap_err();
        assert_eq!(err.0, StatusCode::BAD_REQUEST);
    }
}
