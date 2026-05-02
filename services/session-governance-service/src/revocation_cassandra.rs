//! S3.3 — Cassandra-backed session revocation list.
//!
//! Keyspace `auth_runtime` (shared with
//! [`identity-federation-service::sessions_cassandra`](../../../identity-federation-service/src/sessions_cassandra.rs)).
//!
//! Two tables:
//!
//! * [`SESSION_REVOCATION_DDL`] — direct lookup by `session_id`.
//!   PK `((session_id_prefix), session_id)`. The 2-byte hex prefix
//!   yields 256 partitions of ~equal size. TTL 1800 s (the access-
//!   token / session lifetime); after that the underlying session
//!   is gone and the revocation row is meaningless.
//! * [`USER_REVOCATION_DDL`] — fan-out by user ("revoke all of my
//!   sessions"). PK `((user_id, day_bucket), revoked_at, session_id)`,
//!   clustering ordered by `revoked_at DESC` so the most recent
//!   revocation events come first. TTL 86 400 s (24 h) — long enough
//!   to cover any legitimate session that may still be cached.
//!
//! Auth path on `identity-federation-service`:
//!
//! 1. Validate JWT → recover `session_id`.
//! 2. Single-partition `SELECT … FROM session_revocation
//!    WHERE session_id_prefix = ? AND session_id = ?`.
//! 3. If hit → 401.
//!
//! Step 2 is what we mean by "fast revocation list" (P99 ≤ 5 ms in
//! steady state).

use std::sync::Arc;

use cassandra_kernel::Migration;
use cassandra_kernel::scylla::{
    Session,
    frame::value::{CqlDate, CqlTimestamp},
};
use uuid::Uuid;

/// Cassandra keyspace shared with `identity-federation-service`.
pub const KEYSPACE: &str = "auth_runtime";

/// TTL for `session_revocation` rows. Matches
/// [`identity_federation_service::sessions_cassandra::USER_SESSION_TTL_SECS`].
pub const SESSION_REVOCATION_TTL_SECS: i32 = 1800;

/// TTL for `user_revocation` rows (24 h).
pub const USER_REVOCATION_TTL_SECS: i32 = 86_400;

/// `session_revocation` DDL — direct hash-prefixed lookup. Reads
/// are single-partition and bounded.
pub const SESSION_REVOCATION_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.session_revocation ( \
    session_id_prefix text, \
    session_id        uuid, \
    user_id           text, \
    revoked_at        timestamp, \
    reason            text, \
    PRIMARY KEY ((session_id_prefix), session_id) \
) WITH default_time_to_live = 1800 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'MINUTES', \
                    'compaction_window_size': '30'}";

/// `user_revocation` DDL — fan-out by user for "revoke all of my
/// sessions" UX. Clustering DESC so the most recent event is at the
/// top of the partition.
pub const USER_REVOCATION_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.user_revocation ( \
    user_id     text, \
    day_bucket  date, \
    revoked_at  timestamp, \
    session_id  uuid, \
    reason      text, \
    PRIMARY KEY ((user_id, day_bucket), revoked_at, session_id) \
) WITH CLUSTERING ORDER BY (revoked_at DESC, session_id ASC) \
  AND default_time_to_live = 86400 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'HOURS', \
                    'compaction_window_size': '6'}";

/// Versioned migration slice consumed by `cassandra_kernel::migrate::apply`.
pub const MIGRATIONS: &[Migration] = &[Migration {
    version: 1,
    name: "auth_runtime_revocation_tables",
    statements: &[SESSION_REVOCATION_DDL, USER_REVOCATION_DDL],
}];

/// 2-byte hex prefix derived from the session UUID. Mirrors the
/// shape used by `refresh_token` so the bucket distribution is well-
/// understood.
pub fn session_id_prefix(session_id: Uuid) -> String {
    let bytes = session_id.as_bytes();
    let mut out = String::with_capacity(4);
    for b in &bytes[..2] {
        out.push_str(&format!("{b:02x}"));
    }
    out
}

/// Day bucket (UTC midnight, unix seconds) for the `user_revocation`
/// partition. Crossing midnight just creates a new partition; old
/// rows TTL out after 24 h.
pub fn day_bucket(unix_secs: i64) -> i64 {
    unix_secs - unix_secs.rem_euclid(86_400)
}

fn cql_ts(unix_secs: i64) -> CqlTimestamp {
    CqlTimestamp(unix_secs.saturating_mul(1_000))
}

fn cql_day(unix_secs: i64) -> CqlDate {
    let days_since_epoch = unix_secs.div_euclid(86_400);
    CqlDate(((1_i64 << 31) + days_since_epoch) as u32)
}

/// Revocation reason. The set is deliberately small so audit
/// pipelines can pin an enum without ad-hoc strings drifting in.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RevocationReason {
    UserLogout,
    AdminAction,
    SuspectedCompromise,
    RefreshTokenReplay,
    PolicyViolation,
}

impl RevocationReason {
    pub const fn as_str(self) -> &'static str {
        match self {
            Self::UserLogout => "user_logout",
            Self::AdminAction => "admin_action",
            Self::SuspectedCompromise => "suspected_compromise",
            Self::RefreshTokenReplay => "refresh_token_replay",
            Self::PolicyViolation => "policy_violation",
        }
    }
}

/// Adapter the handlers will use once the bin is wired up.
#[derive(Clone)]
pub struct RevocationAdapter {
    session: Arc<Session>,
}

impl RevocationAdapter {
    pub fn new(session: Arc<Session>) -> Self {
        Self { session }
    }

    /// Apply the `auth_runtime` revocation migrations. Idempotent.
    pub async fn migrate(&self) -> cassandra_kernel::KernelResult<()> {
        cassandra_kernel::migrate::apply(&self.session, KEYSPACE, MIGRATIONS).await?;
        Ok(())
    }

    /// Write the direct lookup row plus the user-scoped audit/fan-out
    /// row. Both writes are idempotent on their primary keys and TTL
    /// out automatically after the affected session can no longer be
    /// valid.
    pub async fn revoke_session(
        &self,
        user_id: &str,
        session_id: Uuid,
        reason: RevocationReason,
        revoked_at: i64,
    ) -> cassandra_kernel::KernelResult<()> {
        let prefix = session_id_prefix(session_id);
        let revoked_at_ts = cql_ts(revoked_at);
        self.session
            .query(
                "INSERT INTO auth_runtime.session_revocation \
                 (session_id_prefix, session_id, user_id, revoked_at, reason) \
                 VALUES (?, ?, ?, ?, ?) USING TTL ?",
                (
                    prefix.as_str(),
                    session_id,
                    user_id,
                    revoked_at_ts,
                    reason.as_str(),
                    SESSION_REVOCATION_TTL_SECS,
                ),
            )
            .await?;

        self.session
            .query(
                "INSERT INTO auth_runtime.user_revocation \
                 (user_id, day_bucket, revoked_at, session_id, reason) \
                 VALUES (?, ?, ?, ?, ?) USING TTL ?",
                (
                    user_id,
                    cql_day(day_bucket(revoked_at)),
                    revoked_at_ts,
                    session_id,
                    reason.as_str(),
                    USER_REVOCATION_TTL_SECS,
                ),
            )
            .await?;
        Ok(())
    }

    /// Fast auth-path check: single-partition lookup by prefixed
    /// session id.
    pub async fn is_session_revoked(
        &self,
        session_id: Uuid,
    ) -> cassandra_kernel::KernelResult<bool> {
        let prefix = session_id_prefix(session_id);
        let result = self
            .session
            .query(
                "SELECT revoked_at FROM auth_runtime.session_revocation \
                 WHERE session_id_prefix = ? AND session_id = ?",
                (prefix.as_str(), session_id),
            )
            .await?;
        Ok(result
            .rows
            .as_ref()
            .map(|rows| !rows.is_empty())
            .unwrap_or(false))
    }

    /// Recent revocation events for a user/day, used by admin and
    /// security-review surfaces.
    pub async fn list_user_revocations(
        &self,
        user_id: &str,
        day_unix_secs: i64,
    ) -> cassandra_kernel::KernelResult<Vec<(Uuid, String, i64)>> {
        let result = self
            .session
            .query(
                "SELECT session_id, reason, revoked_at FROM auth_runtime.user_revocation \
                 WHERE user_id = ? AND day_bucket = ?",
                (user_id, cql_day(day_bucket(day_unix_secs))),
            )
            .await?;
        let mut items = Vec::new();
        for row in result.rows_typed_or_empty::<(Uuid, String, CqlTimestamp)>() {
            let (session_id, reason, revoked_at) = row.map_err(|error| {
                cassandra_kernel::KernelError::ModellingRule(format!(
                    "auth_runtime.user_revocation row decode failed: {error}"
                ))
            })?;
            items.push((session_id, reason, revoked_at.0 / 1_000));
        }
        Ok(items)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn session_id_prefix_is_two_bytes_hex() {
        let id = Uuid::from_bytes([0xde, 0xad, 0xbe, 0xef, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
        assert_eq!(session_id_prefix(id), "dead");
    }

    #[test]
    fn day_bucket_rounds_down_to_midnight() {
        let t = 1_714_650_896_i64; // some Tue 12:34:56 UTC.
        assert_eq!(day_bucket(t) % 86_400, 0);
        assert!(day_bucket(t) <= t);
        assert!(t - day_bucket(t) < 86_400);
    }

    #[test]
    fn revocation_reasons_are_pinned() {
        assert_eq!(RevocationReason::UserLogout.as_str(), "user_logout");
        assert_eq!(
            RevocationReason::RefreshTokenReplay.as_str(),
            "refresh_token_replay"
        );
    }

    #[test]
    fn migrations_have_pinned_versions() {
        assert_eq!(MIGRATIONS.len(), 1);
        assert_eq!(MIGRATIONS[0].version, 1);
        assert_eq!(MIGRATIONS[0].statements.len(), 2);
    }
}
