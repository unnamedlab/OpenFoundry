//! Foundry-style "Functions on objects → Media" surface.
//!
//! The doc lists the universal operations every media reference
//! property exposes inside a function:
//!
//!   * `read_raw(item)` — raw bytes (`Blob` / `BytesIO`).
//!   * `ocr(item)` — OCR'd text (image + document schemas).
//!   * `extract_text(item)` — text without OCR (documents).
//!   * `transcribe_audio(item)` — text + per-segment timestamps.
//!   * `read_metadata(item)` — JSON metadata (mime, size, sha256, …).
//!
//! Each function calls into a [`MediaFunctionRuntime`] trait so the
//! binary can wire the real `media-transform-runtime-service` HTTP
//! client without coupling tests to network access. Tests use
//! [`MockMediaRuntime`] (declared below) which records every call
//! and returns scripted responses.
//!
//! The `MediaItem` parameter mirrors the Foundry shape (`MediaItem`
//! object on TypeScript v1, `MediaReference` on v2 / Python). We use
//! the local [`MediaItemHandle`] struct so the kernel does not have
//! to depend on `core-models` and the function service stays
//! self-contained.

use async_trait::async_trait;
use bytes::Bytes;
use serde::{Deserialize, Serialize};
use thiserror::Error;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq, Hash)]
#[serde(rename_all = "camelCase")]
pub struct MediaItemHandle {
    pub media_set_rid: String,
    pub media_item_rid: String,
    /// Optional: when present, helps the runtime pick the right
    /// branch view — defaults to `main`.
    #[serde(default)]
    pub branch: Option<String>,
    /// Optional: helps the runtime pick the right rate row from the
    /// cost table without an extra Postgres roundtrip.
    #[serde(default)]
    pub schema: Option<String>,
}

impl MediaItemHandle {
    pub fn new(set_rid: impl Into<String>, item_rid: impl Into<String>) -> Self {
        Self {
            media_set_rid: set_rid.into(),
            media_item_rid: item_rid.into(),
            branch: None,
            schema: None,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TranscriptSegment {
    /// Start offset in seconds.
    pub start: f64,
    /// End offset in seconds.
    pub end: f64,
    pub text: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Transcription {
    pub text: String,
    pub segments: Vec<TranscriptSegment>,
}

#[derive(Debug, Error)]
pub enum MediaFunctionError {
    #[error("media item not found: {0}")]
    NotFound(String),
    #[error("media transformation `{kind}` is not implemented yet: {reason}")]
    NotImplemented { kind: String, reason: String },
    #[error("runtime error: {0}")]
    Runtime(String),
}

pub type MediaFunctionResult<T> = Result<T, MediaFunctionError>;

/// What the binary wires against `media-transform-runtime-service`
/// and what tests stub. Keep the surface tight: all five functions
/// the Foundry doc lists, plus a metadata accessor.
///
/// Implementations must be `Send + Sync` because the function runtime
/// shares the same trait object across concurrent `tokio` handlers.
#[async_trait]
pub trait MediaFunctionRuntime: Send + Sync {
    async fn read_raw(&self, item: &MediaItemHandle) -> MediaFunctionResult<Bytes>;
    async fn ocr(&self, item: &MediaItemHandle) -> MediaFunctionResult<String>;
    async fn extract_text(&self, item: &MediaItemHandle) -> MediaFunctionResult<String>;
    async fn transcribe_audio(
        &self,
        item: &MediaItemHandle,
    ) -> MediaFunctionResult<Transcription>;
    async fn read_metadata(
        &self,
        item: &MediaItemHandle,
    ) -> MediaFunctionResult<serde_json::Value>;
}

// ───────────────────── Public function entry points ──────────────────────
//
// These are the names the Foundry doc lists. They take an explicit
// runtime so callers (Python / TS function bodies, tests, the gRPC
// surface) can pin which implementation runs without a global.

pub async fn read_raw(
    runtime: &dyn MediaFunctionRuntime,
    item: &MediaItemHandle,
) -> MediaFunctionResult<Bytes> {
    runtime.read_raw(item).await
}

pub async fn ocr(
    runtime: &dyn MediaFunctionRuntime,
    item: &MediaItemHandle,
) -> MediaFunctionResult<String> {
    runtime.ocr(item).await
}

pub async fn extract_text(
    runtime: &dyn MediaFunctionRuntime,
    item: &MediaItemHandle,
) -> MediaFunctionResult<String> {
    runtime.extract_text(item).await
}

pub async fn transcribe_audio(
    runtime: &dyn MediaFunctionRuntime,
    item: &MediaItemHandle,
) -> MediaFunctionResult<Transcription> {
    runtime.transcribe_audio(item).await
}

pub async fn read_metadata(
    runtime: &dyn MediaFunctionRuntime,
    item: &MediaItemHandle,
) -> MediaFunctionResult<serde_json::Value> {
    runtime.read_metadata(item).await
}

// ───────────────────── Mock runtime (test fixture) ──────────────────────
//
// Lives in the lib (not behind `cfg(test)`) so cross-crate tests can
// reach it without a `dev-dependency` cycle. The runtime is
// intentionally simple — it records calls + returns scripted
// responses keyed by `media_item_rid`.

use std::collections::HashMap;
use std::sync::Mutex;

#[derive(Debug, Default)]
pub struct MockMediaRuntime {
    pub raw: Mutex<HashMap<String, Bytes>>,
    pub ocr: Mutex<HashMap<String, String>>,
    pub text: Mutex<HashMap<String, String>>,
    pub transcripts: Mutex<HashMap<String, Transcription>>,
    pub metadata: Mutex<HashMap<String, serde_json::Value>>,
    /// Per-method call log so tests can assert ordering / parameter
    /// shape without leaking the entire scripted state.
    pub call_log: Mutex<Vec<(String, MediaItemHandle)>>,
}

impl MockMediaRuntime {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn put_raw(&self, item_rid: &str, bytes: impl Into<Bytes>) -> &Self {
        self.raw.lock().unwrap().insert(item_rid.into(), bytes.into());
        self
    }
    pub fn put_ocr(&self, item_rid: &str, text: impl Into<String>) -> &Self {
        self.ocr.lock().unwrap().insert(item_rid.into(), text.into());
        self
    }
    pub fn put_text(&self, item_rid: &str, text: impl Into<String>) -> &Self {
        self.text.lock().unwrap().insert(item_rid.into(), text.into());
        self
    }
    pub fn put_transcript(&self, item_rid: &str, t: Transcription) -> &Self {
        self.transcripts.lock().unwrap().insert(item_rid.into(), t);
        self
    }
    pub fn put_metadata(&self, item_rid: &str, value: serde_json::Value) -> &Self {
        self.metadata.lock().unwrap().insert(item_rid.into(), value);
        self
    }

    pub fn calls(&self) -> Vec<(String, MediaItemHandle)> {
        self.call_log.lock().unwrap().clone()
    }
}

#[async_trait]
impl MediaFunctionRuntime for MockMediaRuntime {
    async fn read_raw(&self, item: &MediaItemHandle) -> MediaFunctionResult<Bytes> {
        self.call_log
            .lock()
            .unwrap()
            .push(("read_raw".into(), item.clone()));
        self.raw
            .lock()
            .unwrap()
            .get(&item.media_item_rid)
            .cloned()
            .ok_or_else(|| MediaFunctionError::NotFound(item.media_item_rid.clone()))
    }

    async fn ocr(&self, item: &MediaItemHandle) -> MediaFunctionResult<String> {
        self.call_log
            .lock()
            .unwrap()
            .push(("ocr".into(), item.clone()));
        self.ocr
            .lock()
            .unwrap()
            .get(&item.media_item_rid)
            .cloned()
            .ok_or_else(|| MediaFunctionError::NotImplemented {
                kind: "ocr".into(),
                reason: format!(
                    "no OCR scripted for `{}` in MockMediaRuntime",
                    item.media_item_rid
                ),
            })
    }

    async fn extract_text(&self, item: &MediaItemHandle) -> MediaFunctionResult<String> {
        self.call_log
            .lock()
            .unwrap()
            .push(("extract_text".into(), item.clone()));
        self.text
            .lock()
            .unwrap()
            .get(&item.media_item_rid)
            .cloned()
            .ok_or_else(|| MediaFunctionError::NotImplemented {
                kind: "extract_text".into(),
                reason: format!(
                    "no extract_text scripted for `{}` in MockMediaRuntime",
                    item.media_item_rid
                ),
            })
    }

    async fn transcribe_audio(
        &self,
        item: &MediaItemHandle,
    ) -> MediaFunctionResult<Transcription> {
        self.call_log
            .lock()
            .unwrap()
            .push(("transcribe_audio".into(), item.clone()));
        self.transcripts
            .lock()
            .unwrap()
            .get(&item.media_item_rid)
            .cloned()
            .ok_or_else(|| MediaFunctionError::NotImplemented {
                kind: "transcribe_audio".into(),
                reason: format!(
                    "no transcript scripted for `{}` in MockMediaRuntime",
                    item.media_item_rid
                ),
            })
    }

    async fn read_metadata(
        &self,
        item: &MediaItemHandle,
    ) -> MediaFunctionResult<serde_json::Value> {
        self.call_log
            .lock()
            .unwrap()
            .push(("read_metadata".into(), item.clone()));
        self.metadata
            .lock()
            .unwrap()
            .get(&item.media_item_rid)
            .cloned()
            .ok_or_else(|| MediaFunctionError::NotFound(item.media_item_rid.clone()))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn handle(item: &str) -> MediaItemHandle {
        MediaItemHandle::new("ri.foundry.main.media_set.x", item)
    }

    #[tokio::test]
    async fn read_raw_returns_scripted_bytes() {
        let mock = MockMediaRuntime::new();
        mock.put_raw("a", Bytes::from_static(b"hello"));
        let bytes = read_raw(&mock, &handle("a")).await.unwrap();
        assert_eq!(bytes, Bytes::from_static(b"hello"));
    }

    #[tokio::test]
    async fn ocr_surfaces_not_implemented_when_unscripted() {
        let mock = MockMediaRuntime::new();
        let err = ocr(&mock, &handle("missing")).await.unwrap_err();
        assert!(matches!(err, MediaFunctionError::NotImplemented { .. }));
    }
}
