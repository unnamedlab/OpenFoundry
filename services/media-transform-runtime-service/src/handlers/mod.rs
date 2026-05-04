//! Native (pure-Rust) handlers + the dispatch table the REST runtime
//! uses to route a `kind` to its implementation. External-binary
//! handlers (`ffmpeg`, `tesseract`, …) are intentionally absent; the
//! runtime returns 501 for those kinds via the catalog without
//! reaching this module.

pub mod image_ops;

#[derive(Debug, thiserror::Error)]
pub enum HandlerError {
    #[error("unsupported mime type `{0}` for transformation `{1}`")]
    UnsupportedMime(String, String),
    #[error("invalid params for `{0}`: {1}")]
    InvalidParams(String, String),
    #[error("decode failed: {0}")]
    Decode(String),
    #[error("encode failed: {0}")]
    Encode(String),
    #[error("io: {0}")]
    Io(#[from] std::io::Error),
}

/// What a handler hands back to the REST layer.
#[derive(Debug, Clone, Default)]
pub struct HandlerOutput {
    pub output_mime_type: String,
    pub output_bytes: Option<Vec<u8>>,
    pub output_json: Option<serde_json::Value>,
}

pub type HandlerResult = Result<HandlerOutput, HandlerError>;

/// Dispatch a request to the matching native handler. Catalogue-only
/// `kind`s (External / NotImplemented) never reach this function —
/// the REST layer short-circuits them with the catalog status.
pub async fn dispatch(
    kind: &str,
    mime_type: &str,
    params: &serde_json::Value,
    bytes: Vec<u8>,
) -> HandlerResult {
    match kind {
        "thumbnail" => image_ops::thumbnail(mime_type, params, bytes),
        "resize" => image_ops::resize(mime_type, params, bytes),
        "resize_within_bounding_box" => image_ops::resize_within_bbox(mime_type, params, bytes),
        "rotate" => image_ops::rotate(mime_type, params, bytes),
        "crop" => image_ops::crop(mime_type, params, bytes),
        "grayscale" => image_ops::grayscale(mime_type, bytes),
        other => Err(HandlerError::InvalidParams(
            other.to_string(),
            "no native handler — caller should not have reached dispatch()".into(),
        )),
    }
}
