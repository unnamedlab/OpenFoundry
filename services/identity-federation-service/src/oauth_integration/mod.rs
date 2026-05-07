//! `oauth-integration-service` substrate (S3.4).
//!
//! Surfaces the new substrate that handlers will adopt in PRs that
//! follow this stream:
//!
//! * [`pending_auth_cassandra`] — short-lived OAuth/PKCE pending
//!   authorization codes and access-token cache. Cassandra TTL is
//!   the source of truth for expiry.
//! * [`clients_postgres`] — long-lived OAuth client and integration
//!   configuration. Lives in
//!   `pg-schemas.auth_schema.oauth_clients` (consolidated cluster
//!   per S6.1).
//!
//! The bin (`src/main.rs`) is intentionally empty during the
//! cutover — handler-by-handler refactor deferred per ADR-0024.

pub mod clients_postgres;
pub mod pending_auth_cassandra;
