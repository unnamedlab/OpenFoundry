//! P6 — Foundry "Application reference" API-conformance helpers.
//!
//! The Application reference imposes four cross-cutting contracts on
//! the dataset surface:
//!
//!   1. **Cursor pagination** — every list endpoint accepts
//!      `?cursor=&limit=` and returns `{ data, next_cursor, has_more }`.
//!   2. **ETag / 304 Not Modified** — GETs of resources include an
//!      `ETag` header (sha256 of the JSON body); a matching
//!      `If-None-Match` returns 304 with no body.
//!   3. **Batch responses (207 Multi-Status)** — batch endpoints
//!      return one `BatchItemResult` per input.
//!   4. **Unified error envelope** —
//!      `{ code, message, details, request_id }` for every error path.
//!
//! These helpers are mounted under `crate::handlers::conformance` so
//! every handler in the service can reach them without growing the
//! `mod.rs` file.

use axum::Json;
use axum::http::{HeaderMap, HeaderValue, StatusCode, header};
use axum::response::{IntoResponse, Response};
use base64::Engine;
use serde::{Deserialize, Serialize};
use serde_json::Value;

// ─────────────────────────────────────────────────────────────────────────────
// Cursor pagination
// ─────────────────────────────────────────────────────────────────────────────

/// Query string for every paginated list endpoint. The cursor is an
/// opaque base64-url string; producers and consumers must treat it as
/// opaque (the encoding can change without the wire contract
/// changing).
#[derive(Debug, Default, Deserialize)]
#[serde(default)]
pub struct PageQuery {
    pub cursor: Option<String>,
    pub limit: Option<i64>,
}

impl PageQuery {
    /// Effective limit clamped to the [1, 500] band that matches the
    /// existing dataset-versioning surface.
    pub fn effective_limit(&self) -> i64 {
        self.limit.unwrap_or(50).clamp(1, 500)
    }

    /// Decode the cursor as the textual offset of the next item to
    /// emit. `None` ⇒ start at the head of the collection.
    pub fn offset(&self) -> i64 {
        self.cursor
            .as_deref()
            .and_then(decode_offset_cursor)
            .unwrap_or(0)
    }
}

/// Wire envelope for paginated responses.
#[derive(Debug, Serialize)]
pub struct Page<T> {
    pub data: Vec<T>,
    /// Opaque cursor to pass back as `?cursor=` for the next page.
    /// `None` when the caller has reached the end.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub next_cursor: Option<String>,
    pub has_more: bool,
}

impl<T> Page<T> {
    /// Build a page from `data` + the offset that produced it. Use
    /// the helper from your handler after slicing the SQL result.
    pub fn from_slice(data: Vec<T>, offset: i64, limit: i64, has_more: bool) -> Self {
        let next_cursor = if has_more {
            Some(encode_offset_cursor(offset + limit))
        } else {
            None
        };
        Self {
            data,
            next_cursor,
            has_more,
        }
    }
}

/// Encode a 0-based offset into the cursor format the wire contract
/// uses today: `base64url(b"of:<offset>")`. The literal prefix lets
/// consumers tell at a glance that the cursor belongs to the
/// dataset-versioning surface (vs. an opaque token from another
/// service).
pub fn encode_offset_cursor(offset: i64) -> String {
    let raw = format!("of:{offset}");
    base64::engine::general_purpose::URL_SAFE_NO_PAD.encode(raw.as_bytes())
}

pub fn decode_offset_cursor(cursor: &str) -> Option<i64> {
    let bytes = base64::engine::general_purpose::URL_SAFE_NO_PAD
        .decode(cursor.as_bytes())
        .ok()?;
    let text = std::str::from_utf8(&bytes).ok()?;
    text.strip_prefix("of:").and_then(|s| s.parse::<i64>().ok())
}

// ─────────────────────────────────────────────────────────────────────────────
// ETag
// ─────────────────────────────────────────────────────────────────────────────

/// Sha256-hex of the canonical JSON. Wrapped in quotes to match the
/// `If-None-Match` header convention (RFC 7232).
pub fn etag_for(value: &Value) -> String {
    use sha2::{Digest, Sha256};
    let canonical = serde_json::to_string(value).unwrap_or_default();
    let mut hasher = Sha256::new();
    hasher.update(canonical.as_bytes());
    let digest = hasher.finalize();
    format!("\"{:x}\"", digest)
}

/// Returns `true` when the request's `If-None-Match` matches the
/// computed `etag`. The handler should respond 304 in that case.
pub fn if_none_match_matches(headers: &HeaderMap, etag: &str) -> bool {
    headers
        .get(header::IF_NONE_MATCH)
        .and_then(|h| h.to_str().ok())
        .map(|raw| {
            raw.split(',')
                .any(|candidate| candidate.trim() == etag || candidate.trim() == "*")
        })
        .unwrap_or(false)
}

/// Build a `200 OK` response with `ETag` set, OR a `304 Not Modified`
/// response when the request's `If-None-Match` matches. The `body`
/// is canonicalised once for the etag and once for the body — both
/// derive from the same `serde_json::Value`.
pub fn json_with_etag(headers: &HeaderMap, value: Value) -> Response {
    let etag = etag_for(&value);
    if if_none_match_matches(headers, &etag) {
        let mut response = Response::new(axum::body::Body::empty());
        *response.status_mut() = StatusCode::NOT_MODIFIED;
        if let Ok(value) = HeaderValue::from_str(&etag) {
            response.headers_mut().insert(header::ETAG, value);
        }
        return response;
    }
    let mut response = Json(value).into_response();
    if let Ok(value) = HeaderValue::from_str(&etag) {
        response.headers_mut().insert(header::ETAG, value);
    }
    response
}

// ─────────────────────────────────────────────────────────────────────────────
// 207 Multi-Status batch response
// ─────────────────────────────────────────────────────────────────────────────

/// One row in a 207 Multi-Status response. `status` mirrors the HTTP
/// status the equivalent single-item call would produce.
#[derive(Debug, Serialize)]
pub struct BatchItemResult<T> {
    pub status: u16,
    /// Caller-provided id from the request (e.g. the txn id, file id).
    pub id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub data: Option<T>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<ErrorEnvelope>,
}

/// Wrap a list of [`BatchItemResult`]s as a 207 response.
pub fn batch_response<T: Serialize>(items: Vec<BatchItemResult<T>>) -> Response {
    let body = Json(items).into_response();
    let (mut parts, body) = body.into_parts();
    parts.status = StatusCode::MULTI_STATUS;
    Response::from_parts(parts, body)
}

// ─────────────────────────────────────────────────────────────────────────────
// Unified error envelope
// ─────────────────────────────────────────────────────────────────────────────

/// Foundry-style error envelope. Values like `DATASET_NOT_FOUND` are
/// the canonical machine-readable codes; the human-readable
/// `message` mirrors what the handler used to put in `error`.
#[derive(Debug, Serialize)]
pub struct ErrorEnvelope {
    pub code: String,
    pub message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub details: Option<Value>,
    /// Opaque trace id the gateway / log scraper can correlate with
    /// the audit event.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub request_id: Option<String>,
}

impl ErrorEnvelope {
    pub fn new(code: impl Into<String>, message: impl Into<String>) -> Self {
        Self {
            code: code.into(),
            message: message.into(),
            details: None,
            request_id: None,
        }
    }
    pub fn with_details(mut self, details: Value) -> Self {
        self.details = Some(details);
        self
    }
    pub fn with_request_id(mut self, request_id: String) -> Self {
        self.request_id = Some(request_id);
        self
    }
    pub fn into_response(self, status: StatusCode) -> Response {
        let mut response = Json(serde_json::json!({ "error": self })).into_response();
        *response.status_mut() = status;
        response
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn cursor_round_trips_through_encode_decode() {
        let cursor = encode_offset_cursor(42);
        assert_eq!(decode_offset_cursor(&cursor), Some(42));
        // Garbage input falls through cleanly.
        assert_eq!(decode_offset_cursor("not-base64"), None);
        assert_eq!(decode_offset_cursor(""), None);
    }

    #[test]
    fn etag_is_stable_across_logically_equal_payloads() {
        let a = serde_json::json!({ "a": 1, "b": 2 });
        let b = serde_json::json!({ "a": 1, "b": 2 });
        assert_eq!(etag_for(&a), etag_for(&b));
        // Different payload → different etag.
        let c = serde_json::json!({ "a": 1, "b": 3 });
        assert_ne!(etag_for(&a), etag_for(&c));
    }

    #[test]
    fn page_emits_next_cursor_only_when_more_pages_remain() {
        let p = Page::<i32>::from_slice(vec![1, 2, 3], 0, 3, true);
        assert!(p.has_more);
        assert_eq!(
            decode_offset_cursor(p.next_cursor.as_ref().unwrap()),
            Some(3)
        );
        let last = Page::<i32>::from_slice(vec![10], 90, 3, false);
        assert!(last.next_cursor.is_none());
        assert!(!last.has_more);
    }

    #[test]
    fn page_query_clamps_limit_to_band() {
        let q = PageQuery {
            limit: Some(0),
            cursor: None,
        };
        assert_eq!(q.effective_limit(), 1);
        let q = PageQuery {
            limit: Some(10000),
            cursor: None,
        };
        assert_eq!(q.effective_limit(), 500);
    }

    #[test]
    fn if_none_match_matches_quoted_etag_and_wildcard() {
        let mut headers = HeaderMap::new();
        headers.insert(header::IF_NONE_MATCH, HeaderValue::from_static("\"abc\""));
        assert!(if_none_match_matches(&headers, "\"abc\""));
        assert!(!if_none_match_matches(&headers, "\"def\""));
        let mut wild = HeaderMap::new();
        wild.insert(header::IF_NONE_MATCH, HeaderValue::from_static("*"));
        assert!(if_none_match_matches(&wild, "\"abc\""));
    }

    #[test]
    fn error_envelope_serialises_with_application_reference_keys() {
        let env = ErrorEnvelope::new("DATASET_NOT_FOUND", "no such dataset")
            .with_request_id("req-123".into());
        let json = serde_json::to_value(&env).unwrap();
        assert_eq!(json["code"], "DATASET_NOT_FOUND");
        assert_eq!(json["message"], "no such dataset");
        assert_eq!(json["request_id"], "req-123");
        assert!(json.get("details").is_none());
    }
}
