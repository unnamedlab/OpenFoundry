//! S3.2 — Cassandra-backed session, refresh-token and OAuth-state
//! storage for `identity-federation-service`.
//!
//! Three tables in keyspace `auth_runtime` (bootstrapped by
//! [`infra/k8s/cassandra/keyspaces-job.yaml`](../../../../infra/k8s/cassandra/keyspaces-job.yaml)):
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
//!
//! All three tables use the
//! `cassandra_kernel::Migration` ledger so the schema lands
//! idempotently on every startup.
//!
//! The adapter [`SessionsAdapter`] is the typed surface handlers use
//! once the bin is wired up. `Arc<scylla::Session>` is injected at
//! construction time.

use std::sync::Arc;

use cassandra_kernel::Migration;
use cassandra_kernel::scylla::Session;
use uuid::Uuid;

/// Cassandra keyspace name. Matches the post-install Job
/// (`infra/k8s/cassandra/keyspaces-job.yaml`).
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

/// Versioned migration slice consumed by `cassandra_kernel::migrate::apply`.
pub const MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "auth_runtime_session_tables",
    statements: &[USER_SESSION_DDL, REFRESH_TOKEN_DDL, OAUTH_STATE_DDL],
}];

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

/// Adapter the handlers will use. Methods are still TODO at this
/// substrate cut: each method documents its CQL and the audit
/// invariants. The `Arc<Session>` is held so the adapter can be
/// cheaply cloned and shared across request tasks.
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

    /// Future write path for `user_session`. Will execute
    /// `INSERT … USING TTL 1800` against the prepared statement
    /// cache. Returns the session id.
    ///
    /// Substrate-only signature — body is filled in by the per-
    /// handler PR that migrates `handlers::login`.
    pub async fn record_session(
        &self,
        _user_id: &str,
        _session_id: Uuid,
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
        assert_eq!(MIGRATIONS.len(), 1);
        assert_eq!(MIGRATIONS[0].version, 1);
        assert_eq!(MIGRATIONS[0].statements.len(), 3);
    }
}
