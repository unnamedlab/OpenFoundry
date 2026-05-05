//! Authentication surface.
//!
//! * [`bearer`]    — Axum extractor that validates incoming
//!                   `Authorization: Bearer <jwt>` and `<api_token>`
//!                   headers, surfacing iceberg-scope-aware claims.
//! * [`oauth`]     — `POST /iceberg/v1/oauth/tokens` (client_credentials
//!                   + refresh_token grants per the spec).
//! * [`api_tokens`]— Foundry-internal endpoint that mints long-lived
//!                   bearer tokens tied to the calling user.

pub mod api_tokens;
pub mod bearer;
pub mod oauth;
