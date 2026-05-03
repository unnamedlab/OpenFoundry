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
//! Legacy Postgres `oauth_pending_auth` / `oauth_token_exchange` DDL is
//! archived under
//! `docs/architecture/legacy-migrations/oauth-integration-service/`.

use std::sync::Arc;

use cassandra_kernel::Migration;
use cassandra_kernel::scylla::{
    Session,
    frame::value::{CqlDate, CqlTimestamp},
};
use chrono::{DateTime, Utc};

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

impl TryFrom<&str> for CodeChallengeMethod {
    type Error = cassandra_kernel::KernelError;

    fn try_from(value: &str) -> Result<Self, Self::Error> {
        match value {
            "S256" => Ok(Self::S256),
            other => Err(kernel_invalid(format!(
                "unsupported code challenge method: {other}"
            ))),
        }
    }
}

#[derive(Debug, Clone)]
pub struct PendingAuthRecord {
    pub authorization_code: String,
    pub client_id: String,
    pub redirect_uri: String,
    pub scopes: Vec<String>,
    pub user_id: String,
    pub code_challenge: String,
    pub code_challenge_method: CodeChallengeMethod,
    pub issued_at: DateTime<Utc>,
}

#[derive(Debug, Clone)]
pub struct TokenExchangeRecord {
    pub token_hash: String,
    pub client_id: String,
    pub user_id: String,
    pub scopes: Vec<String>,
    pub expires_at: DateTime<Utc>,
}

fn cql_day(unix_secs: i64) -> CqlDate {
    let days_since_epoch = unix_secs.div_euclid(86_400);
    CqlDate(((1_i64 << 31) + days_since_epoch) as u32)
}

fn cql_ts(dt: DateTime<Utc>) -> CqlTimestamp {
    CqlTimestamp(dt.timestamp_millis())
}

fn unix_secs_to_ts(secs: i64) -> CqlTimestamp {
    CqlTimestamp(secs.saturating_mul(1_000))
}

fn cql_ts_to_dt(ts: CqlTimestamp) -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp_millis(ts.0).unwrap_or_else(Utc::now)
}

fn ttl_until(expires_at: DateTime<Utc>) -> i32 {
    (expires_at - Utc::now())
        .num_seconds()
        .clamp(1, i32::MAX as i64) as i32
}

fn kernel_decode_error(error: impl std::fmt::Display) -> cassandra_kernel::KernelError {
    cassandra_kernel::KernelError::ModellingRule(format!("auth_runtime row decode failed: {error}"))
}

fn kernel_invalid(error: impl Into<String>) -> cassandra_kernel::KernelError {
    cassandra_kernel::KernelError::ModellingRule(error.into())
}

fn token_hash_bytes(token_hash: &str) -> Vec<u8> {
    token_hash.as_bytes().to_vec()
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

    /// Persist the short-lived OAuth/PKCE authorization round-trip
    /// state in Cassandra.
    pub async fn record_pending_auth(
        &self,
        authorization_code: &str,
        client_id: &str,
        redirect_uri: &str,
        scopes: &[String],
        user_id: &str,
        code_challenge: &str,
        code_challenge_method: CodeChallengeMethod,
        issued_at: i64,
    ) -> cassandra_kernel::KernelResult<()> {
        self.session
            .query(
                "INSERT INTO auth_runtime.oauth_pending_auth \
                 (day_bucket, authorization_code, client_id, redirect_uri, scopes, user_id, \
                  code_challenge, code_challenge_method, issued_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    cql_day(issued_at),
                    authorization_code,
                    client_id,
                    redirect_uri,
                    scopes,
                    user_id,
                    code_challenge,
                    code_challenge_method.as_str(),
                    unix_secs_to_ts(issued_at),
                    PENDING_AUTH_TTL_SECS,
                ),
            )
            .await?;
        Ok(())
    }

    /// Lookup by authorization code. We probe both today's bucket and
    /// yesterday's to handle requests crossing UTC midnight inside the
    /// 10-minute auth-code TTL.
    pub async fn get_pending_auth(
        &self,
        authorization_code: &str,
        observed_at: i64,
    ) -> cassandra_kernel::KernelResult<Option<PendingAuthRecord>> {
        for bucket in [day_bucket(observed_at), day_bucket(observed_at - 86_400)] {
            let result = self
                .session
                .query(
                    "SELECT client_id, redirect_uri, scopes, user_id, code_challenge, \
                            code_challenge_method, issued_at \
                     FROM auth_runtime.oauth_pending_auth \
                     WHERE day_bucket = ? AND authorization_code = ?",
                    (cql_day(bucket), authorization_code),
                )
                .await?;

            let mut rows = result.rows_typed_or_empty::<(
                String,
                String,
                Vec<String>,
                String,
                String,
                String,
                CqlTimestamp,
            )>();
            if let Some(row) = rows.next() {
                let (
                    client_id,
                    redirect_uri,
                    scopes,
                    user_id,
                    code_challenge,
                    code_challenge_method,
                    issued_at,
                ) = row.map_err(kernel_decode_error)?;
                return Ok(Some(PendingAuthRecord {
                    authorization_code: authorization_code.to_string(),
                    client_id,
                    redirect_uri,
                    scopes,
                    user_id,
                    code_challenge,
                    code_challenge_method: CodeChallengeMethod::try_from(
                        code_challenge_method.as_str(),
                    )?,
                    issued_at: cql_ts_to_dt(issued_at),
                }));
            }
        }
        Ok(None)
    }

    pub async fn consume_pending_auth(
        &self,
        authorization_code: &str,
        observed_at: i64,
    ) -> cassandra_kernel::KernelResult<Option<PendingAuthRecord>> {
        let record = self
            .get_pending_auth(authorization_code, observed_at)
            .await?;
        if let Some(record) = &record {
            self.session
                .query(
                    "DELETE FROM auth_runtime.oauth_pending_auth \
                     WHERE day_bucket = ? AND authorization_code = ?",
                    (cql_day(record.issued_at.timestamp()), authorization_code),
                )
                .await?;
        }
        Ok(record)
    }

    pub async fn store_token_exchange(
        &self,
        token_hash: &str,
        client_id: &str,
        user_id: &str,
        scopes: &[String],
        expires_at: DateTime<Utc>,
    ) -> cassandra_kernel::KernelResult<()> {
        let hash_bytes = token_hash_bytes(token_hash);
        let prefix = token_hash_prefix(&hash_bytes);
        self.session
            .query(
                "INSERT INTO auth_runtime.oauth_token_exchange \
                 (token_hash_prefix, token_hash, client_id, user_id, scopes, expires_at) \
                 VALUES (?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    prefix.as_str(),
                    hash_bytes,
                    client_id,
                    user_id,
                    scopes,
                    cql_ts(expires_at),
                    ttl_until(expires_at),
                ),
            )
            .await?;
        Ok(())
    }

    pub async fn get_token_exchange(
        &self,
        token_hash: &str,
    ) -> cassandra_kernel::KernelResult<Option<TokenExchangeRecord>> {
        let hash_bytes = token_hash_bytes(token_hash);
        let prefix = token_hash_prefix(&hash_bytes);
        let result = self
            .session
            .query(
                "SELECT client_id, user_id, scopes, expires_at \
                 FROM auth_runtime.oauth_token_exchange \
                 WHERE token_hash_prefix = ? AND token_hash = ?",
                (prefix.as_str(), hash_bytes),
            )
            .await?;
        let mut rows = result.rows_typed_or_empty::<(String, String, Vec<String>, CqlTimestamp)>();
        let Some(row) = rows.next() else {
            return Ok(None);
        };
        let (client_id, user_id, scopes, expires_at) = row.map_err(kernel_decode_error)?;
        Ok(Some(TokenExchangeRecord {
            token_hash: token_hash.to_string(),
            client_id,
            user_id,
            scopes,
            expires_at: cql_ts_to_dt(expires_at),
        }))
    }

    pub async fn delete_token_exchange(
        &self,
        token_hash: &str,
    ) -> cassandra_kernel::KernelResult<()> {
        let hash_bytes = token_hash_bytes(token_hash);
        let prefix = token_hash_prefix(&hash_bytes);
        self.session
            .query(
                "DELETE FROM auth_runtime.oauth_token_exchange \
                 WHERE token_hash_prefix = ? AND token_hash = ?",
                (prefix.as_str(), hash_bytes),
            )
            .await?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const ACTIVE_OAUTH_CONFIG_SQL: &str =
        include_str!("../migrations/20260427010100_oauth_applications_and_integrations.sql");
    const ACTIVE_MIGRATIONS_README: &str = include_str!("../migrations/README.md");
    const LEGACY_OAUTH_RUNTIME_SQL: &str = include_str!(
        "../../../docs/architecture/legacy-migrations/oauth-integration-service/20260427000000_oauth_runtime_state.sql"
    );

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

    #[test]
    fn active_postgres_migrations_do_not_create_oauth_runtime_tables() {
        assert!(!ACTIVE_OAUTH_CONFIG_SQL.contains("CREATE TABLE IF NOT EXISTS oauth_pending_auth"));
        assert!(
            !ACTIVE_OAUTH_CONFIG_SQL.contains("CREATE TABLE IF NOT EXISTS oauth_token_exchange")
        );
        assert!(
            ACTIVE_MIGRATIONS_README
                .contains("Ephemeral OAuth runtime state does **not** belong in Postgres")
        );
    }

    #[test]
    fn archived_postgres_runtime_ddl_is_preserved_for_cutover_audit() {
        assert!(LEGACY_OAUTH_RUNTIME_SQL.contains("CREATE TABLE IF NOT EXISTS oauth_pending_auth"));
        assert!(
            LEGACY_OAUTH_RUNTIME_SQL.contains("CREATE TABLE IF NOT EXISTS oauth_token_exchange")
        );
    }
}
