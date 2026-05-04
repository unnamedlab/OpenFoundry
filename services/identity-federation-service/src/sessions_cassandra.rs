//! S3.2 — Cassandra-backed session, refresh-token and OAuth-state
//! storage for `identity-federation-service`.
//!
//! Three tables in keyspace `auth_runtime` (bootstrapped by
//! [`infra/k8s/platform/manifests/cassandra/keyspaces-job.yaml`](../../../../infra/k8s/platform/manifests/cassandra/keyspaces-job.yaml)):
//!
//! * [`USER_SESSION_DDL`] — `auth_runtime.user_session
//!   ((user_id, hour_bucket), session_id)`, TTL 1800 s (sliding via
//!   re-write on touch). PK includes `user_id` + `hour_bucket` to
//!   bound partition size and avoid hot-spotting.
//! * [`REFRESH_TOKEN_DDL`] — `auth_runtime.refresh_token
//!   ((token_hash_prefix), token_hash, family_id, …)`, TTL
//!   `2_592_000` s (30 d). The 2-byte prefix gives 256 partitions of
//!   roughly equal size.
//! * [`OAUTH_STATE_DDL`] — `auth_runtime.oauth_state
//!   ((day_bucket), state)`, TTL 600 s.
//! * [`SCOPED_SESSION_BY_USER_DDL`] / [`SCOPED_SESSION_BY_ID_DDL`] —
//!   Cassandra mirrors for self-service scoped-session listing and
//!   revocation.
//! * [`REFRESH_TOKEN_BY_ID_DDL`] — lookup table that preserves the
//!   token-id API shape without reviving the legacy Postgres table.
//! * [`REPOSITORY_SESSION_BY_ID_DDL`] — point-lookup table for the
//!   shared `storage-abstraction::SessionStore` implementation in
//!   `cassandra-kernel`.
//!
//! All three tables use the
//! `cassandra_kernel::Migration` ledger so the schema lands
//! idempotently on every startup.
//!
//! The adapter [`SessionsAdapter`] is the typed surface handlers use
//! once the bin is wired up. `Arc<scylla::Session>` is injected at
//! construction time. Legacy Postgres `refresh_tokens` and
//! `scoped_sessions` DDL is archived under
//! `docs/architecture/legacy-migrations/identity-federation-service/`.

use std::sync::Arc;

use cassandra_kernel::Migration;
use cassandra_kernel::scylla::{Session, frame::value::CqlTimestamp};
use chrono::{DateTime, Utc};
use uuid::Uuid;

/// Cassandra keyspace name. Matches the post-install Job
/// (`infra/k8s/platform/manifests/cassandra/keyspaces-job.yaml`).
pub const KEYSPACE: &str = "auth_runtime";

/// Sliding TTL for `user_session` rows (30 min).
pub const USER_SESSION_TTL_SECS: i32 = 1800;

/// Absolute TTL for `refresh_token` rows (30 d).
pub const REFRESH_TOKEN_TTL_SECS: i32 = 2_592_000;

/// Absolute TTL for `oauth_state` rows (10 min).
pub const OAUTH_STATE_TTL_SECS: i32 = 600;

/// `user_session` DDL — partition `(user_id, hour_bucket)`, cluster
/// by `session_id`. TTL is set on every write (sliding 30-min).
pub const USER_SESSION_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.user_session ( \
    user_id        text, \
    hour_bucket    timestamp, \
    session_id     uuid, \
    issued_at      timestamp, \
    last_seen_at   timestamp, \
    user_agent     text, \
    ip_address     inet, \
    mfa_level      text, \
    PRIMARY KEY ((user_id, hour_bucket), session_id) \
) WITH CLUSTERING ORDER BY (session_id ASC) \
  AND default_time_to_live = 1800 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'MINUTES', \
                    'compaction_window_size': '30'}";

/// `refresh_token` DDL — partition `(token_hash_prefix)` (the first
/// 2 bytes of the SHA-256 hash, encoded as hex), cluster by
/// `token_hash`. Family info travels with the row so replay
/// detection (S3.1.f) is a single-partition lookup.
pub const REFRESH_TOKEN_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.refresh_token ( \
    token_hash_prefix text, \
    token_hash        blob, \
    family_id         uuid, \
    user_id           text, \
    issued_at         timestamp, \
    expires_at        timestamp, \
    revoked_at        timestamp, \
    rotated_to        blob, \
    PRIMARY KEY ((token_hash_prefix), token_hash) \
) WITH default_time_to_live = 2592000 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'DAYS', \
                    'compaction_window_size': '7'}";

/// `oauth_state` DDL — partition `(day_bucket)`, cluster by `state`.
/// Holds PKCE `code_verifier` + redirect URI for the duration of the
/// authorization round-trip.
pub const OAUTH_STATE_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.oauth_state ( \
    day_bucket    date, \
    state         text, \
    redirect_uri  text, \
    code_verifier text, \
    client_id     text, \
    issued_at     timestamp, \
    PRIMARY KEY ((day_bucket), state) \
) WITH default_time_to_live = 600 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'HOURS', \
                    'compaction_window_size': '1'}";

/// `scoped_session_by_user` — list sessions for the self-service UI.
/// Rows are also mirrored to `scoped_session_by_id` so revocation by
/// session id stays single-partition.
pub const SCOPED_SESSION_BY_USER_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.scoped_session_by_user ( \
    user_id       text, \
    created_at    timestamp, \
    session_id    uuid, \
    label         text, \
    session_kind  text, \
    scope         text, \
    guest_email   text, \
    guest_name    text, \
    expires_at    timestamp, \
    revoked_at    timestamp, \
    PRIMARY KEY ((user_id), created_at, session_id) \
) WITH CLUSTERING ORDER BY (created_at DESC, session_id ASC) \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'DAYS', \
                    'compaction_window_size': '1'}";

/// Direct lookup table used by revocation and token introspection.
pub const SCOPED_SESSION_BY_ID_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.scoped_session_by_id ( \
    session_id    uuid PRIMARY KEY, \
    user_id       text, \
    created_at    timestamp, \
    label         text, \
    session_kind  text, \
    scope         text, \
    guest_email   text, \
    guest_name    text, \
    expires_at    timestamp, \
    revoked_at    timestamp \
) WITH compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'DAYS', \
                    'compaction_window_size': '1'}";

/// Token-id lookup table preserving the legacy refresh-token API
/// shape while the replay-detection table remains hash-partitioned.
pub const REFRESH_TOKEN_BY_ID_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.refresh_token_by_id ( \
    token_id           uuid PRIMARY KEY, \
    token_hash_prefix  text, \
    token_hash         blob, \
    family_id          uuid, \
    user_id            text, \
    issued_at          timestamp, \
    expires_at         timestamp, \
    revoked_at         timestamp \
) WITH default_time_to_live = 2592000 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'DAYS', \
                    'compaction_window_size': '7'}";

/// Generic repository session table used by
/// `cassandra_kernel::repos::CassandraSessionStore`.
///
/// The partition key is `(tenant, session_id)` so every trait lookup is a
/// single-row read and tenants with many concurrent sessions cannot create a
/// hot tenant-wide partition. Identity-specific listing, refresh-token
/// rotation and governance state stay in the tables above.
pub const REPOSITORY_SESSION_BY_ID_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.sessions_by_id ( \
    tenant      text, \
    session_id  text, \
    subject     text, \
    attributes  map<text, text>, \
    issued_at   timestamp, \
    expires_at  timestamp, \
    PRIMARY KEY ((tenant, session_id)) \
) WITH default_time_to_live = 0 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'HOURS', \
                    'compaction_window_size': '1'}";

/// Versioned migration slice consumed by `cassandra_kernel::migrate::apply`.
pub const MIGRATIONS: &[Migration] = &[
    Migration {
        version: 1,
        name: "auth_runtime_session_tables",
        statements: &[USER_SESSION_DDL, REFRESH_TOKEN_DDL, OAUTH_STATE_DDL],
    },
    Migration {
        version: 2,
        name: "auth_runtime_scoped_sessions_and_refresh_lookup",
        statements: &[
            SCOPED_SESSION_BY_USER_DDL,
            SCOPED_SESSION_BY_ID_DDL,
            REFRESH_TOKEN_BY_ID_DDL,
        ],
    },
    Migration {
        version: 4,
        name: "auth_runtime_repository_sessions_by_id",
        statements: &[REPOSITORY_SESSION_BY_ID_DDL],
    },
];

/// Persisted representation of a scoped/guest session.
#[derive(Debug, Clone)]
pub struct ScopedSessionRecord {
    pub id: Uuid,
    pub user_id: Uuid,
    pub label: String,
    pub session_kind: String,
    pub scope: serde_json::Value,
    pub guest_email: Option<String>,
    pub guest_name: Option<String>,
    pub expires_at: DateTime<Utc>,
    pub revoked_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
}

/// Persisted representation of a refresh token.
#[derive(Debug, Clone)]
pub struct RefreshTokenRecord {
    pub id: Uuid,
    pub user_id: Uuid,
    pub token_hash: String,
    pub expires_at: DateTime<Utc>,
    pub revoked: bool,
    pub created_at: DateTime<Utc>,
}

/// Bucket-key derivation for `refresh_token`. The prefix is the
/// first 2 bytes (hex-encoded) of the SHA-256 hash → 256 buckets,
/// each covering ~1/256 of all live tokens.
pub fn token_hash_prefix(token_hash: &[u8]) -> String {
    let len = token_hash.len().min(2);
    let mut out = String::with_capacity(4);
    for b in &token_hash[..len] {
        out.push_str(&format!("{b:02x}"));
    }
    out
}

/// Hour bucket for `user_session` partition. Rounds the issuing
/// instant down to the hour. Crossing the hour boundary just creates
/// a new partition; old rows TTL out.
pub fn hour_bucket(issued_at_unix_secs: i64) -> i64 {
    issued_at_unix_secs - issued_at_unix_secs.rem_euclid(3600)
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

fn ttl_until(expires_at: DateTime<Utc>, grace_secs: i64) -> i32 {
    let ttl = (expires_at - Utc::now())
        .num_seconds()
        .saturating_add(grace_secs)
        .clamp(1, i32::MAX as i64);
    ttl as i32
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

fn token_hash_string(bytes: Vec<u8>) -> cassandra_kernel::KernelResult<String> {
    String::from_utf8(bytes).map_err(|error| kernel_invalid(format!("invalid token hash: {error}")))
}

/// Cassandra adapter used by identity handlers for access sessions,
/// scoped/guest sessions, refresh tokens and OAuth state.
#[derive(Clone)]
pub struct SessionsAdapter {
    session: Arc<Session>,
}

impl SessionsAdapter {
    pub fn new(session: Arc<Session>) -> Self {
        Self { session }
    }

    /// Apply the `auth_runtime` session migrations against the
    /// shared Cassandra cluster. Idempotent.
    pub async fn migrate(&self) -> cassandra_kernel::KernelResult<()> {
        cassandra_kernel::migrate::apply(&self.session, KEYSPACE, MIGRATIONS).await?;
        Ok(())
    }

    pub async fn record_session(
        &self,
        user_id: &str,
        session_id: Uuid,
        issued_at: i64,
    ) -> cassandra_kernel::KernelResult<()> {
        let issued_at_ts = unix_secs_to_ts(issued_at);
        let hour_bucket = unix_secs_to_ts(hour_bucket(issued_at));
        self.session
            .query(
                "INSERT INTO auth_runtime.user_session \
                 (user_id, hour_bucket, session_id, issued_at, last_seen_at) \
                 VALUES (?, ?, ?, ?, ?) USING TTL ?",
                (
                    user_id,
                    hour_bucket,
                    session_id,
                    issued_at_ts,
                    issued_at_ts,
                    USER_SESSION_TTL_SECS,
                ),
            )
            .await?;
        Ok(())
    }

    pub async fn record_scoped_session(
        &self,
        record: ScopedSessionRecord,
    ) -> cassandra_kernel::KernelResult<()> {
        let scope = serde_json::to_string(&record.scope)
            .map_err(|error| kernel_invalid(format!("invalid session scope: {error}")))?;
        let ttl = ttl_until(record.expires_at, 86_400);
        let user_id = record.user_id.to_string();
        let created_at = cql_ts(record.created_at);
        let expires_at = cql_ts(record.expires_at);
        let revoked_at = record.revoked_at.map(cql_ts);

        self.session
            .query(
                "INSERT INTO auth_runtime.scoped_session_by_user \
                 (user_id, created_at, session_id, label, session_kind, scope, guest_email, \
                  guest_name, expires_at, revoked_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    user_id.as_str(),
                    created_at,
                    record.id,
                    record.label.as_str(),
                    record.session_kind.as_str(),
                    scope.as_str(),
                    record.guest_email.as_deref(),
                    record.guest_name.as_deref(),
                    expires_at,
                    revoked_at,
                    ttl,
                ),
            )
            .await?;

        self.session
            .query(
                "INSERT INTO auth_runtime.scoped_session_by_id \
                 (session_id, user_id, created_at, label, session_kind, scope, guest_email, \
                  guest_name, expires_at, revoked_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    record.id,
                    user_id.as_str(),
                    created_at,
                    record.label.as_str(),
                    record.session_kind.as_str(),
                    scope.as_str(),
                    record.guest_email.as_deref(),
                    record.guest_name.as_deref(),
                    expires_at,
                    revoked_at,
                    ttl,
                ),
            )
            .await?;
        Ok(())
    }

    pub async fn list_scoped_sessions(
        &self,
        user_id: Uuid,
    ) -> cassandra_kernel::KernelResult<Vec<ScopedSessionRecord>> {
        let user_id_text = user_id.to_string();
        let result = self
            .session
            .query(
                "SELECT session_id, label, session_kind, scope, guest_email, guest_name, \
                        expires_at, revoked_at, created_at \
                 FROM auth_runtime.scoped_session_by_user WHERE user_id = ?",
                (user_id_text.as_str(),),
            )
            .await?;

        let mut records = Vec::new();
        for row in result.rows_typed_or_empty::<(
            Uuid,
            String,
            String,
            String,
            Option<String>,
            Option<String>,
            CqlTimestamp,
            Option<CqlTimestamp>,
            CqlTimestamp,
        )>() {
            let (
                id,
                label,
                session_kind,
                scope,
                guest_email,
                guest_name,
                expires_at,
                revoked_at,
                created_at,
            ) = row.map_err(kernel_decode_error)?;
            records.push(ScopedSessionRecord {
                id,
                user_id,
                label,
                session_kind,
                scope: serde_json::from_str(&scope)
                    .map_err(|error| kernel_invalid(format!("invalid session scope: {error}")))?,
                guest_email,
                guest_name,
                expires_at: cql_ts_to_dt(expires_at),
                revoked_at: revoked_at.map(cql_ts_to_dt),
                created_at: cql_ts_to_dt(created_at),
            });
        }
        Ok(records)
    }

    pub async fn revoke_scoped_session(
        &self,
        session_id: Uuid,
        user_id: Uuid,
        allow_any_user: bool,
    ) -> cassandra_kernel::KernelResult<bool> {
        let Some(record) = self.get_scoped_session(session_id).await? else {
            return Ok(false);
        };
        if !allow_any_user && record.user_id != user_id {
            return Ok(false);
        }
        if record.revoked_at.is_some() {
            return Ok(false);
        }

        let revoked_at = cql_ts(Utc::now());
        let ttl = ttl_until(record.expires_at, 86_400);
        self.session
            .query(
                "UPDATE auth_runtime.scoped_session_by_id USING TTL ? \
                 SET revoked_at = ? WHERE session_id = ?",
                (ttl, revoked_at, session_id),
            )
            .await?;
        self.session
            .query(
                "UPDATE auth_runtime.scoped_session_by_user USING TTL ? \
                 SET revoked_at = ? WHERE user_id = ? AND created_at = ? AND session_id = ?",
                (
                    ttl,
                    revoked_at,
                    record.user_id.to_string(),
                    cql_ts(record.created_at),
                    session_id,
                ),
            )
            .await?;
        Ok(true)
    }

    pub async fn get_scoped_session(
        &self,
        session_id: Uuid,
    ) -> cassandra_kernel::KernelResult<Option<ScopedSessionRecord>> {
        let result = self
            .session
            .query(
                "SELECT user_id, created_at, label, session_kind, scope, guest_email, guest_name, \
                        expires_at, revoked_at \
                 FROM auth_runtime.scoped_session_by_id WHERE session_id = ?",
                (session_id,),
            )
            .await?;

        let mut rows = result.rows_typed_or_empty::<(
            String,
            CqlTimestamp,
            String,
            String,
            String,
            Option<String>,
            Option<String>,
            CqlTimestamp,
            Option<CqlTimestamp>,
        )>();
        let Some(row) = rows.next() else {
            return Ok(None);
        };
        let (
            user_id,
            created_at,
            label,
            session_kind,
            scope,
            guest_email,
            guest_name,
            expires_at,
            revoked_at,
        ) = row.map_err(kernel_decode_error)?;
        let user_id = Uuid::parse_str(&user_id)
            .map_err(|error| kernel_invalid(format!("invalid user id: {error}")))?;
        Ok(Some(ScopedSessionRecord {
            id: session_id,
            user_id,
            label,
            session_kind,
            scope: serde_json::from_str(&scope)
                .map_err(|error| kernel_invalid(format!("invalid session scope: {error}")))?,
            guest_email,
            guest_name,
            expires_at: cql_ts_to_dt(expires_at),
            revoked_at: revoked_at.map(cql_ts_to_dt),
            created_at: cql_ts_to_dt(created_at),
        }))
    }

    pub async fn store_refresh_token(
        &self,
        user_id: Uuid,
        token_id: Uuid,
        token_hash: &str,
        expires_at: DateTime<Utc>,
    ) -> cassandra_kernel::KernelResult<()> {
        let hash_bytes = token_hash_bytes(token_hash);
        let prefix = token_hash_prefix(&hash_bytes);
        let now = Utc::now();
        let ttl = ttl_until(expires_at, 0);
        self.session
            .query(
                "INSERT INTO auth_runtime.refresh_token \
                 (token_hash_prefix, token_hash, family_id, user_id, issued_at, expires_at) \
                 VALUES (?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    prefix.as_str(),
                    hash_bytes.clone(),
                    token_id,
                    user_id.to_string(),
                    cql_ts(now),
                    cql_ts(expires_at),
                    ttl,
                ),
            )
            .await?;
        self.session
            .query(
                "INSERT INTO auth_runtime.refresh_token_by_id \
                 (token_id, token_hash_prefix, token_hash, family_id, user_id, issued_at, expires_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    token_id,
                    prefix.as_str(),
                    hash_bytes,
                    token_id,
                    user_id.to_string(),
                    cql_ts(now),
                    cql_ts(expires_at),
                    ttl,
                ),
            )
            .await?;
        Ok(())
    }

    pub async fn get_refresh_token(
        &self,
        token_id: Uuid,
    ) -> cassandra_kernel::KernelResult<Option<RefreshTokenRecord>> {
        let result = self
            .session
            .query(
                "SELECT user_id, token_hash, expires_at, revoked_at, issued_at \
                 FROM auth_runtime.refresh_token_by_id WHERE token_id = ?",
                (token_id,),
            )
            .await?;
        let mut rows = result.rows_typed_or_empty::<(
            String,
            Vec<u8>,
            CqlTimestamp,
            Option<CqlTimestamp>,
            CqlTimestamp,
        )>();
        let Some(row) = rows.next() else {
            return Ok(None);
        };
        let (user_id, token_hash, expires_at, revoked_at, issued_at) =
            row.map_err(kernel_decode_error)?;
        Ok(Some(RefreshTokenRecord {
            id: token_id,
            user_id: Uuid::parse_str(&user_id)
                .map_err(|error| kernel_invalid(format!("invalid user id: {error}")))?,
            token_hash: token_hash_string(token_hash)?,
            expires_at: cql_ts_to_dt(expires_at),
            revoked: revoked_at.is_some(),
            created_at: cql_ts_to_dt(issued_at),
        }))
    }

    pub async fn revoke_refresh_token(&self, token_id: Uuid) -> cassandra_kernel::KernelResult<()> {
        let result = self
            .session
            .query(
                "SELECT token_hash_prefix, token_hash, expires_at \
                 FROM auth_runtime.refresh_token_by_id WHERE token_id = ?",
                (token_id,),
            )
            .await?;
        let mut rows = result.rows_typed_or_empty::<(String, Vec<u8>, CqlTimestamp)>();
        let Some(row) = rows.next() else {
            return Ok(());
        };
        let (prefix, token_hash, expires_at) = row.map_err(kernel_decode_error)?;
        let expires_at = cql_ts_to_dt(expires_at);
        let ttl = ttl_until(expires_at, 0);
        let revoked_at = cql_ts(Utc::now());

        self.session
            .query(
                "UPDATE auth_runtime.refresh_token_by_id USING TTL ? \
                 SET revoked_at = ? WHERE token_id = ?",
                (ttl, revoked_at, token_id),
            )
            .await?;
        self.session
            .query(
                "UPDATE auth_runtime.refresh_token USING TTL ? \
                 SET revoked_at = ? WHERE token_hash_prefix = ? AND token_hash = ?",
                (ttl, revoked_at, prefix.as_str(), token_hash),
            )
            .await?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    macro_rules! repo_file {
        ($relative:literal $(,)?) => {
            include_str!(concat!("../", $relative))
        };
    }

    #[test]
    fn token_hash_prefix_is_two_bytes_hex() {
        let h = [0xde_u8, 0xad, 0xbe, 0xef];
        assert_eq!(token_hash_prefix(&h), "dead");
    }

    #[test]
    fn token_hash_prefix_handles_short_input() {
        assert_eq!(token_hash_prefix(&[0x01]), "01");
        assert_eq!(token_hash_prefix(&[]), "");
    }

    #[test]
    fn hour_bucket_rounds_down() {
        // 12:34:56 UTC → 12:00:00.
        let t = 1_714_650_896_i64;
        assert_eq!(hour_bucket(t) % 3600, 0);
        assert!(hour_bucket(t) <= t);
        assert!(t - hour_bucket(t) < 3600);
    }

    #[test]
    fn migrations_have_pinned_versions() {
        assert_eq!(MIGRATIONS.len(), 3);
        assert_eq!(MIGRATIONS[0].version, 1);
        assert_eq!(MIGRATIONS[1].version, 2);
        assert_eq!(MIGRATIONS[2].version, 4);
        assert_eq!(MIGRATIONS[0].statements.len(), 3);
        assert_eq!(MIGRATIONS[1].statements.len(), 3);
        assert_eq!(MIGRATIONS[2].statements.len(), 1);
        assert!(REPOSITORY_SESSION_BY_ID_DDL.contains("auth_runtime.sessions_by_id"));
    }

    #[test]
    fn active_postgres_migrations_do_not_create_runtime_session_tables() {
        let active_initial_auth_sql = repo_file!("migrations/20260419000001_initial_auth.sql");
        let active_migrations_readme = repo_file!("migrations/README.md");

        assert!(!active_initial_auth_sql.contains("CREATE TABLE IF NOT EXISTS refresh_tokens"));
        assert!(!active_initial_auth_sql.contains("CREATE TABLE IF NOT EXISTS scoped_sessions"));
        assert!(
            active_migrations_readme.contains("Runtime auth state no longer belongs in Postgres")
        );
    }

    #[test]
    fn archived_postgres_runtime_ddl_is_preserved_for_cutover_audit() {
        let legacy_refresh_tokens_sql = repo_file!(
            "../../docs/architecture/legacy-migrations/identity-federation-service/20260419000001_refresh_tokens.sql",
        );
        let legacy_scoped_sessions_sql = repo_file!(
            "../../docs/architecture/legacy-migrations/identity-federation-service/20260425193000_scoped_sessions_security.sql",
        );

        assert!(legacy_refresh_tokens_sql.contains("CREATE TABLE IF NOT EXISTS refresh_tokens"));
        assert!(legacy_scoped_sessions_sql.contains("CREATE TABLE IF NOT EXISTS scoped_sessions"));
    }
}
