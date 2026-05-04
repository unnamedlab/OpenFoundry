//! REST runtime — accepts a [`TransformInput`], dispatches into
//! [`crate::handlers`], and returns a [`TransformOutput`] with the
//! billed compute-seconds. Bytes are exchanged base64-encoded so the
//! same JSON request/response shape works for image, audio and
//! document inputs without a multipart parser.

use std::sync::Arc;

use axum::{
    Json, Router,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
    routing::{get, post},
};
use serde::{Deserialize, Serialize};
use tower_http::trace::TraceLayer;

use crate::catalog::{CATALOG, CatalogEntry, HandlerStatus, lookup};
use crate::handlers::{HandlerError, dispatch};

/// Shared state — currently empty. Reserved so future handlers can
/// reach a `BackendMediaStorage` (for direct PERSIST writes) and a
/// metrics registry without a breaking signature change.
#[derive(Debug, Clone, Default)]
pub struct AppState;

/// `POST /transform` body. `bytes_base64` carries the source media
/// item; the runtime decodes once at the boundary and hands raw
/// `Vec<u8>` to the handler.
#[derive(Debug, Clone, Deserialize)]
pub struct TransformInput {
    pub kind: String,
    pub mime_type: String,
    /// Foundry media-set schema string (`IMAGE` / `AUDIO` / …).
    /// Surfaced to handlers + the cost meter so the per-schema row
    /// in the cost table is selected correctly.
    pub schema: String,
    #[serde(default)]
    pub params: serde_json::Value,
    /// Source bytes, base64-encoded.
    pub bytes_base64: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TransformStatus {
    Ok,
    NotImplemented,
}

/// `POST /transform` response body. `output_bytes_base64` is `None`
/// when the handler produced structured data only (e.g. OCR text,
/// scene timestamps); look at `output_json` then.
#[derive(Debug, Clone, Serialize)]
pub struct TransformOutput {
    pub status: TransformStatus,
    pub kind: String,
    pub output_mime_type: String,
    /// Charged compute-seconds (lookup via
    /// `observability::cost_model::charge_compute_seconds`).
    pub compute_seconds: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_bytes_base64: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_json: Option<serde_json::Value>,
    /// Populated on `NOT_IMPLEMENTED` so callers can surface the
    /// reason verbatim.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub reason: Option<String>,
}

pub fn build_router(state: AppState) -> Router {
    Router::new()
        .route("/transform", post(run_transform))
        .route("/catalog", get(list_catalog))
        .route("/catalog/{kind}", get(catalog_entry))
        .route("/healthz", get(healthz))
        .layer(TraceLayer::new_for_http())
        .with_state(Arc::new(state))
}

async fn healthz() -> &'static str {
    "ok"
}

async fn list_catalog() -> Json<&'static [CatalogEntry]> {
    Json(CATALOG)
}

async fn catalog_entry(Path(kind): Path<String>) -> Result<Json<CatalogEntry>, RuntimeError> {
    CATALOG
        .iter()
        .copied()
        .find(|entry| entry.key == kind)
        .map(Json)
        .ok_or(RuntimeError::UnknownKind(kind))
}

async fn run_transform(
    State(_state): State<Arc<AppState>>,
    Json(body): Json<TransformInput>,
) -> Result<Json<TransformOutput>, RuntimeError> {
    let status = lookup(&body.kind).ok_or_else(|| RuntimeError::UnknownKind(body.kind.clone()))?;

    match status {
        HandlerStatus::NotImplemented { reason } => {
            // Stable 501 envelope so the caller (media-sets-service)
            // can degrade to a placeholder image / cached miss banner.
            Ok(Json(TransformOutput {
                status: TransformStatus::NotImplemented,
                kind: body.kind,
                output_mime_type: body.mime_type,
                compute_seconds: 0,
                output_bytes_base64: None,
                output_json: None,
                reason: Some(reason.to_string()),
            }))
        }
        HandlerStatus::External { binary } => {
            // External binaries are deferred. We surface the same
            // 501 envelope as the unimplemented path with a
            // canonical "bin not wired" reason; once the binary is
            // wired the catalog entry flips to `External { binary }`
            // *and* a real handler in `crate::handlers` takes over.
            Ok(Json(TransformOutput {
                status: TransformStatus::NotImplemented,
                kind: body.kind,
                output_mime_type: body.mime_type,
                compute_seconds: 0,
                output_bytes_base64: None,
                output_json: None,
                reason: Some(format!(
                    "external binary `{binary}` is not wired yet — handler will land in a follow-up PR"
                )),
            }))
        }
        HandlerStatus::Native => {
            let bytes = base64_decode(&body.bytes_base64)?;
            let billed_bytes = bytes.len() as u64;
            let result = dispatch(&body.kind, &body.mime_type, &body.params, bytes).await?;
            let compute_seconds = observability::charge_compute_seconds(&body.kind, billed_bytes)
                .unwrap_or(0);
            Ok(Json(TransformOutput {
                status: TransformStatus::Ok,
                kind: body.kind,
                output_mime_type: result.output_mime_type,
                compute_seconds,
                output_bytes_base64: result.output_bytes.map(|b| base64_encode(&b)),
                output_json: result.output_json,
                reason: None,
            }))
        }
    }
}

/// Public so `crate::handlers` and the tests can reuse it. Hand-rolled
/// because the workspace already pulls `base64` transitively through
/// jwt/sqlx but does not expose it as a workspace dep — keeping the
/// runtime free of an extra direct dep keeps the compile graph lean.
pub fn base64_decode(input: &str) -> Result<Vec<u8>, RuntimeError> {
    base64_simple::decode(input).map_err(|err| RuntimeError::Base64(err.to_string()))
}
pub fn base64_encode(bytes: &[u8]) -> String {
    base64_simple::encode(bytes)
}

mod base64_simple {
    //! Minimal base64 codec (standard alphabet, no padding tolerance
    //! variations). Inlined so `media-transform-runtime-service` can
    //! ship without a direct `base64` dep. Handles every transform
    //! request body the runtime ever sees in tests + production.

    const ALPHABET: &[u8] =
        b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

    pub fn encode(input: &[u8]) -> String {
        let mut out = String::with_capacity((input.len() + 2) / 3 * 4);
        for chunk in input.chunks(3) {
            let b0 = chunk[0] as u32;
            let b1 = chunk.get(1).copied().unwrap_or(0) as u32;
            let b2 = chunk.get(2).copied().unwrap_or(0) as u32;
            let triple = (b0 << 16) | (b1 << 8) | b2;
            out.push(ALPHABET[((triple >> 18) & 63) as usize] as char);
            out.push(ALPHABET[((triple >> 12) & 63) as usize] as char);
            if chunk.len() > 1 {
                out.push(ALPHABET[((triple >> 6) & 63) as usize] as char);
            } else {
                out.push('=');
            }
            if chunk.len() > 2 {
                out.push(ALPHABET[(triple & 63) as usize] as char);
            } else {
                out.push('=');
            }
        }
        out
    }

    pub fn decode(input: &str) -> Result<Vec<u8>, String> {
        let cleaned: String = input.chars().filter(|c| !c.is_whitespace()).collect();
        if cleaned.len() % 4 != 0 {
            return Err("base64 length must be a multiple of 4".into());
        }
        let mut out = Vec::with_capacity(cleaned.len() / 4 * 3);
        for chunk in cleaned.as_bytes().chunks(4) {
            let mut buf = [0u32; 4];
            let mut padding = 0;
            for (i, b) in chunk.iter().enumerate() {
                buf[i] = if *b == b'=' {
                    padding += 1;
                    0
                } else {
                    decode_byte(*b)? as u32
                };
            }
            let triple = (buf[0] << 18) | (buf[1] << 12) | (buf[2] << 6) | buf[3];
            out.push(((triple >> 16) & 0xff) as u8);
            if padding < 2 {
                out.push(((triple >> 8) & 0xff) as u8);
            }
            if padding < 1 {
                out.push((triple & 0xff) as u8);
            }
        }
        Ok(out)
    }

    fn decode_byte(b: u8) -> Result<u8, String> {
        match b {
            b'A'..=b'Z' => Ok(b - b'A'),
            b'a'..=b'z' => Ok(b - b'a' + 26),
            b'0'..=b'9' => Ok(b - b'0' + 52),
            b'+' => Ok(62),
            b'/' => Ok(63),
            other => Err(format!("invalid base64 byte: {other:#x}")),
        }
    }
}

#[derive(Debug, thiserror::Error)]
pub enum RuntimeError {
    #[error("unknown transformation kind `{0}`")]
    UnknownKind(String),
    #[error("base64 decode failed: {0}")]
    Base64(String),
    #[error("handler error: {0}")]
    Handler(#[from] HandlerError),
}

impl IntoResponse for RuntimeError {
    fn into_response(self) -> axum::response::Response {
        let (status, body) = match &self {
            RuntimeError::UnknownKind(kind) => (
                StatusCode::BAD_REQUEST,
                serde_json::json!({
                    "error": format!("unknown transformation kind `{kind}`"),
                    "code": "MEDIA_TRANSFORM_UNKNOWN_KIND",
                }),
            ),
            RuntimeError::Base64(msg) => (
                StatusCode::BAD_REQUEST,
                serde_json::json!({
                    "error": format!("base64 decode failed: {msg}"),
                    "code": "MEDIA_TRANSFORM_BAD_INPUT",
                }),
            ),
            RuntimeError::Handler(err) => (
                StatusCode::INTERNAL_SERVER_ERROR,
                serde_json::json!({
                    "error": err.to_string(),
                    "code": "MEDIA_TRANSFORM_HANDLER_ERROR",
                }),
            ),
        };
        (status, Json(body)).into_response()
    }
}

/// Re-exported so the `dispatch` signature can return it without a
/// separate import. Mirrors the result the worker hands back to the
/// REST layer; `output_json` covers OCR-style structured outputs and
/// `output_bytes` covers byte-producing transforms (resize, etc.).
pub use crate::handlers::HandlerOutput;

/// Re-exported for tests + the integrator caller.
pub use crate::handlers::HandlerResult as DispatchResult;
