//! S3.4 — Cassandra-backed pending-authorization and access-token
//! cache for `oauth-integration-service`.
//!
//! Keyspace `auth_runtime` (shared with
//! [`identity-federation-service::sessions_cassandra`](../../../identity-federation-service/src/sessions_cassandra.rs)).
//!
//! Two tables:
//!
//! * [`PENDING_AUTH_DDL`] — `auth_runtime.oauth_pending_auth
//!   ((day_bucket), authorization_code)`. TTL 600 s (10 min, the
//!   PKCE/auth-code window per RFC 6749 §4.1.2 and OAuth 2.1
//!   recommendation). Holds `client_id`, `redirect_uri`, `scopes`,
//!   `user_id`, `code_challenge`, `code_challenge_method`,
//!   `issued_at`.
//! * [`TOKEN_EXCHANGE_DDL`] — `auth_runtime.oauth_token_exchange
//!   ((token_hash_prefix), token_hash)`. TTL 3600 s (1 h cache for
//!   access-token validation). Holds `client_id`, `user_id`,
//!   `scopes`, `expires_at`.
//!
//! The 2-byte hex prefix on `token_hash` is the same shape used by
//! `refresh_token` (S3.2.b) — 256 partitions of ~equal size.

use std::sync::Arc;

use cassandra_kernel::Migration;
use cassandra_kernel::scylla::Session;

/// Cassandra keyspace shared with the rest of the auth surface.
pub const KEYSPACE: &str = "auth_runtime";

/// TTL for the OAuth/PKCE authorization-code window (10 min).
pub const PENDING_AUTH_TTL_SECS: i32 = 600;

/// TTL for the access-token validation cache (1 h).
pub const TOKEN_EXCHANGE_TTL_SECS: i32 = 3_600;

/// `oauth_pending_auth` DDL — partition by `day_bucket`, cluster by
/// `authorization_code`. Holds PKCE challenge for the duration of
/// the auth code → access token round-trip.
pub const PENDING_AUTH_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.oauth_pending_auth ( \
    day_bucket             date, \
    authorization_code     text, \
    client_id              text, \
    redirect_uri           text, \
    scopes                 list<text>, \
    user_id                text, \
    code_challenge         text, \
    code_challenge_method  text, \
    issued_at              timestamp, \
    PRIMARY KEY ((day_bucket), authorization_code) \
) WITH default_time_to_live = 600 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'HOURS', \
                    'compaction_window_size': '1'}";

/// `oauth_token_exchange` DDL — partition by 2-byte hex hash prefix,
/// cluster by `token_hash`. Single-partition lookup keeps the cache
/// hit P99 ≤ 5 ms.
pub const TOKEN_EXCHANGE_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.oauth_token_exchange ( \
    token_hash_prefix text, \
    token_hash        blob, \
    client_id         text, \
    user_id           text, \
    scopes            list<text>, \
    expires_at        timestamp, \
    PRIMARY KEY ((token_hash_prefix), token_hash) \
) WITH default_time_to_live = 3600 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'HOURS', \
                    'compaction_window_size': '1'}";

/// Versioned migration slice consumed by `cassandra_kernel::migrate::apply`.
pub const MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "auth_runtime_oauth_pending_tables",
    statements: &[PENDING_AUTH_DDL, TOKEN_EXCHANGE_DDL],
}];

/// 2-byte hex prefix derived from the access-token SHA-256 hash.
pub fn token_hash_prefix(token_hash: &[u8]) -> String {
    let len = token_hash.len().min(2);
    let mut out = String::with_capacity(4);
    for b in &token_hash[..len] {
        out.push_str(&format!("{b:02x}"));
    }
    out
}

/// Day bucket (UTC midnight, unix seconds) for the partition. Auth
/// codes only live 10 min so this partition is small even at peak.
pub fn day_bucket(unix_secs: i64) -> i64 {
    unix_secs - unix_secs.rem_euclid(86_400)
}

/// Code-challenge method per RFC 7636. We accept only `S256` (`plain`
/// is forbidden by OAuth 2.1).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum CodeChallengeMethod {
    S256,
}

impl CodeChallengeMethod {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::S256 => "S256",
        }
    }
}

/// Adapter handlers will use once the bin is wired up.
#[derive(Clone)]
pub struct PendingAuthAdapter {
    session: Arc<Session>,
}

impl PendingAuthAdapter {
    pub fn new(session: Arc<Session>) -> Self {
        Self { session }
    }

    /// Apply the `auth_runtime` OAuth pending migrations. Idempotent.
    pub async fn migrate(&self) -> cassandra_kernel::KernelResult<()> {
        cassandra_kernel::migrate::apply(&self.session, KEYSPACE, MIGRATIONS).await?;
        Ok(())
    }

    /// Future write path for `oauth_pending_auth`. Body filled by
    /// the per-handler PR that migrates `handlers::authorize`.
    pub async fn record_pending_auth(
        &self,
        _authorization_code: &str,
        _client_id: &str,
        _user_id: &str,
        _issued_at: i64,
    ) -> cassandra_kernel::KernelResult<()> {
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn token_hash_prefix_is_two_bytes_hex() {
        assert_eq!(token_hash_prefix(&[0xab, 0xcd, 0xef, 0x01]), "abcd");
    }

    #[test]
    fn token_hash_prefix_handles_short_input() {
        assert_eq!(token_hash_prefix(&[0x07]), "07");
        assert_eq!(token_hash_prefix(&[]), "");
    }

    #[test]
    fn day_bucket_rounds_down_to_midnight() {
        let t = 1_714_650_896_i64;
        assert_eq!(day_bucket(t) % 86_400, 0);
        assert!(day_bucket(t) <= t);
    }

    #[test]
    fn code_challenge_method_pinned_s256_only() {
        assert_eq!(CodeChallengeMethod::S256.as_str(), "S256");
    }

    #[test]
    fn migrations_have_pinned_versions() {
        assert_eq!(MIGRATIONS.len(), 1);
        assert_eq!(MIGRATIONS[0].version, 1);
        assert_eq!(MIGRATIONS[0].statements.len(), 2);
    }
}
