//! S3.1 — Identity hardening substrate.
//!
//! Each submodule owns one ASVS L2 control. Modules are pure where
//! possible (calculators, invariants) and trait-shaped where they
//! cross a process boundary (Vault, Redis, Kafka, IdP). Concrete
//! adapters are `*Stub` types that log + return canned values, so
//! the substrate compiles end-to-end without depending on real
//! infra; the production wiring lands handler-by-handler.

pub mod audit_topic;
pub mod jwks_rotation;
pub mod rate_limit;
pub mod refresh_family;
pub mod scim;
pub mod vault_signer;
pub mod webauthn;
