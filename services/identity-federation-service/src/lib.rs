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
//! The bin (`src/main.rs`) is intentionally empty during the cutover.
//! Handler-by-handler refactor is sequenced through follow-up PRs;
//! this crate exposes the substrate (typed adapters, pure functions,
//! testable trait surfaces) the new handlers will consume. Legacy
//! `handlers/*` and `models/*` directories under `src/` remain in
//! place but are NOT re-exported from this lib — they stay private
//! to the bin until each handler is migrated.

pub mod hardening;
pub mod sessions_cassandra;
