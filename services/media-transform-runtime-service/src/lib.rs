//! `media-transform-runtime-service` — worker that runs Foundry-style
//! media access patterns.
//!
//! ## Surface
//!
//! One REST endpoint:
//!
//!   `POST /transform`
//!
//! with body `{kind, mime_type, schema, params, bytes_base64}` returning
//! `{output_bytes_base64?, output_mime_type, compute_seconds, status}`.
//!
//! `compute_seconds` is computed via [`observability::cost_model`] so
//! every worker invocation bills against the same Foundry table the
//! Usage UI surfaces. The audit envelope
//! [`audit_trail::events::AuditEvent::MediaSetAccessPatternInvoked`]
//! is emitted by the *caller* (`media-sets-service`) once the runtime
//! returns — the runtime itself does not own the audit outbox so that
//! the bill-and-audit transaction stays in one place.
//!
//! ## Catalog
//!
//! Every transformation listed in
//! `Data formats/Media sets (unstructured data)/Transforming media.md`
//! has a registered handler. Handlers that need an external binary
//! (`ffmpeg`, `tesseract`, `pdfium`, …) return HTTP 501 with the
//! canonical reason payload `{ status: "NOT_IMPLEMENTED", reason }`
//! so callers can degrade gracefully and the catalog stays stable.

pub mod catalog;
pub mod handlers;
pub mod runtime;

pub use runtime::{TransformInput, TransformOutput, TransformStatus, build_router, AppState};
