//! `session-governance-service` substrate (S3.3).
//!
//! Surfaces the new substrate that handlers will adopt in PRs that
//! follow this stream:
//!
//! * [`revocation_cassandra`] — fast revocation list backed by
//!   Cassandra `auth_runtime.session_revocation` (TTL aligned with
//!   the access-token lifetime so rows are auto-collected once the
//!   token they invalidate has expired).
//! * [`policy_postgres`] — policy definitions live in the shared
//!   `pg-policy` cluster; this module pins the schema/table names
//!   so handlers don't drift.
//!
//! The bin (`src/main.rs`) is intentionally empty during the
//! cutover — handler-by-handler refactor is deferred per ADR-0024,
//! same pattern as `approvals-service` (S2.5.b),
//! `automation-operations-service` (S2.7) and
//! `identity-federation-service` (S3.1/S3.2).

pub mod policy_postgres;
pub mod revocation_cassandra;
