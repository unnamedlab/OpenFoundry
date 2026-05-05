//! HTTP handler modules.
//!
//! * [`rest_catalog`] — Iceberg REST Catalog OpenAPI spec endpoints.
//! * [`auth`]         — OAuth2 token issuer + bearer middleware + API
//!                       token administration.
//! * [`admin`]        — Foundry-internal endpoints powering the
//!                       `/iceberg-tables` UI.

pub mod admin;
pub mod auth;
pub mod diagnose;
pub mod errors;
pub mod markings;
pub mod rest_catalog;
