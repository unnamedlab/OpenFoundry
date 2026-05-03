//! `identity-federation-service` library crate.
//!
//! Stream **S3 of `migration-plan-cassandra-foundry-parity.md`**
//! lifts identity-federation to OWASP ASVS L2: signing keys move
//! into Vault transit (S3.1.b), JWKS rotates on a 90/14-day schedule
//! (S3.1.c), MFA grows a WebAuthn second factor (S3.1.d), SCIM 2.0
//! provisioning lands (S3.1.e), refresh-token families detect replay
//! (S3.1.f), audit goes to Kafka `audit.identity.v1` (S3.1.g),
//! `/login`, `/oauth/token`, `/oauth/authorize` get a Redis-backed
//! per-(user,IP) rate limit (S3.1.h), and Cedar policies own admin
//! authz on key rotation and SCIM operations (S3.1.i). Sessions,
//! refresh tokens and OAuth state move to Cassandra `auth_runtime.*`
//! with TTLs (S3.2.a-c); JWKS signing keys remain in
//! `pg-schemas.auth_schema.jwks_keys` (rare rotation, custody in
//! Vault).
//!
//! The bin (`src/main.rs`) is intentionally thin during the cutover.
//! Handler-by-handler refactor is sequenced through follow-up PRs;
//! this crate exposes the substrate (typed adapters, domain functions,
//! testable trait surfaces) the new handlers consume. Runtime session
//! and refresh-token domain code now routes through Cassandra adapters;
//! Postgres remains for control-plane user/RBAC reads.

use auth_middleware::jwt::JwtConfig;
use authz_cedar::AuthzEngine;
use hardening::{
    audit_topic::IdentityAuditService, jwks_rotation::JwksRotationService, rate_limit::RateLimiter,
    webauthn::WebAuthnService,
};
use sessions_cassandra::SessionsAdapter;
use sqlx::PgPool;
use std::sync::Arc;

pub mod cedar_authz;
pub mod domain;
pub mod handlers;
pub mod hardening;
pub mod models;
pub mod sessions_cassandra;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub sessions: SessionsAdapter,
    pub jwks: Option<JwksRotationService>,
    pub webauthn: WebAuthnService,
    pub audit: IdentityAuditService,
    pub rate_limiter: Arc<dyn RateLimiter>,
    /// Cedar engine — owns the bundled `policies/identity_admin.cedar`
    /// set plus any rows hot-loaded from `pg-policy.cedar_policies`.
    /// `AuthzEngine` is `Clone` (its inner state is `Arc`-shared) so
    /// the request extractor `AuthzGuard` clones it cheaply per
    /// request.
    pub authz: Arc<AuthzEngine>,
}
