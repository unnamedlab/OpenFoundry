//! Job lifecycle state machine + (under `runtime`) the Postgres
//! repository that backs it.
//!
//! The state machine is pure: [`JobStatus::can_transition_to`] is the
//! single source of truth and is unit-tested without a database.
//! The `runtime`-only [`JobRepo`] (see bottom of file) wraps `sqlx`
//! and persists transitions atomically inside a single UPDATE — the
//! state machine is consulted client-side for clear error messages,
//! and the SQL `CHECK` constraint enforces the same invariant
//! server-side as a defence in depth.

use std::fmt;

use thiserror::Error;

/// Lifecycle of a row in `reindex_coordinator.reindex_jobs`.
///
/// Mirrors the `Status` enum of the legacy Go workflow
/// (`workers-go/reindex/internal/contract/contract.go`):
/// `queued` and `running` are pre-terminal; `completed`, `failed`
/// and `cancelled` are terminal.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum JobStatus {
    /// Persisted by the consumer immediately on receiving the
    /// `requested.v1` event, before the first Cassandra page.
    Queued,
    /// First scan page started; transitions back to `Queued` are
    /// not allowed.
    Running,
    /// Scan reached `next_token == None`. Terminal.
    Completed,
    /// One of the page / publish / persist steps raised an error
    /// the retry envelope could not absorb. Terminal; `error`
    /// column is populated.
    Failed,
    /// External cancel signal (control-plane HTTP route). Terminal.
    Cancelled,
}

impl JobStatus {
    /// Wire-format string used in the SQL `CHECK` constraint, in
    /// the `completed.v1` event payload, and in tracing.
    pub fn as_str(self) -> &'static str {
        match self {
            JobStatus::Queued => "queued",
            JobStatus::Running => "running",
            JobStatus::Completed => "completed",
            JobStatus::Failed => "failed",
            JobStatus::Cancelled => "cancelled",
        }
    }

    /// Parse the wire-format string. Inverse of [`Self::as_str`].
    pub fn parse(value: &str) -> Result<Self, StateError> {
        match value {
            "queued" => Ok(JobStatus::Queued),
            "running" => Ok(JobStatus::Running),
            "completed" => Ok(JobStatus::Completed),
            "failed" => Ok(JobStatus::Failed),
            "cancelled" => Ok(JobStatus::Cancelled),
            other => Err(StateError::UnknownStatus(other.to_string())),
        }
    }

    /// `true` for `Completed`, `Failed`, `Cancelled`.
    pub fn is_terminal(self) -> bool {
        matches!(
            self,
            JobStatus::Completed | JobStatus::Failed | JobStatus::Cancelled
        )
    }

    /// Validate a proposed transition from `self` → `next`.
    ///
    /// Allowed moves:
    ///
    /// ```text
    ///   Queued  → Running | Cancelled | Failed
    ///   Running → Completed | Failed | Cancelled
    ///   * → *  (idempotent self-loop on terminal states is allowed
    ///          to keep `INSERT … ON CONFLICT DO UPDATE` writers safe)
    /// ```
    ///
    /// Terminal → non-terminal is forbidden. A new `requested.v1`
    /// for a terminal `(tenant, type)` job is the producer's
    /// responsibility (delete the row first, or pick a different
    /// `type_id`); the coordinator returns
    /// [`StateError::IllegalTransition`] rather than silently
    /// resurrecting the row.
    pub fn can_transition_to(self, next: JobStatus) -> bool {
        if self == next {
            return true;
        }
        matches!(
            (self, next),
            (JobStatus::Queued, JobStatus::Running)
                | (JobStatus::Queued, JobStatus::Failed)
                | (JobStatus::Queued, JobStatus::Cancelled)
                | (JobStatus::Running, JobStatus::Completed)
                | (JobStatus::Running, JobStatus::Failed)
                | (JobStatus::Running, JobStatus::Cancelled)
        )
    }

    /// Same as [`Self::can_transition_to`] but returns a typed error
    /// instead of a bool. Use at the boundary where you want the
    /// error to bubble up; use the bool form inside conditionals.
    pub fn validate_transition(self, next: JobStatus) -> Result<(), StateError> {
        if self.can_transition_to(next) {
            Ok(())
        } else {
            Err(StateError::IllegalTransition {
                from: self,
                to: next,
            })
        }
    }
}

impl fmt::Display for JobStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

#[derive(Debug, Error)]
pub enum StateError {
    #[error("unknown job status {0:?}")]
    UnknownStatus(String),
    #[error("illegal job-status transition {from} → {to}")]
    IllegalTransition { from: JobStatus, to: JobStatus },
    #[cfg(feature = "runtime")]
    #[error("postgres error: {0}")]
    Sqlx(#[from] sqlx::Error),
}

// ───────────────────────── Runtime repo ─────────────────────────

#[cfg(feature = "runtime")]
pub use repo::{JobRecord, JobRepo};

#[cfg(feature = "runtime")]
mod repo {
    use chrono::{DateTime, Utc};
    use sqlx::{PgPool, Row};
    use uuid::Uuid;

    use super::{JobStatus, StateError};

    /// Snapshot of one row in `reindex_coordinator.reindex_jobs`.
    #[derive(Debug, Clone)]
    pub struct JobRecord {
        pub id: Uuid,
        pub tenant_id: String,
        /// Stored as empty string in Postgres for "all-types"
        /// jobs; surfaced here as `None` to make the `Option`-
        /// shaped event schema the boundary type.
        pub type_id: Option<String>,
        pub status: JobStatus,
        pub resume_token: Option<String>,
        pub page_size: i32,
        pub scanned: i64,
        pub published: i64,
        pub error: Option<String>,
        pub started_at: DateTime<Utc>,
        pub updated_at: DateTime<Utc>,
        pub completed_at: Option<DateTime<Utc>>,
    }

    /// Postgres-backed repository for the `reindex_jobs` table.
    ///
    /// All write methods funnel through SQL that includes the
    /// previous status in the `WHERE` clause, so two coordinator
    /// replicas racing on the same row produce at most one
    /// successful UPDATE; the loser sees `rows_affected == 0` and
    /// surfaces a typed error.
    #[derive(Debug, Clone)]
    pub struct JobRepo {
        pool: PgPool,
    }

    impl JobRepo {
        pub fn new(pool: PgPool) -> Self {
            Self { pool }
        }

        pub fn pool(&self) -> &PgPool {
            &self.pool
        }

        /// Idempotent claim. Inserts the row at status `queued`
        /// with `id = uuid_v5(tenant||type)`; if the row already
        /// exists, returns the existing record unchanged. The
        /// caller decides what to do with non-`queued` returns
        /// (e.g. ignore a duplicate `requested.v1` for a still-
        /// running job).
        pub async fn upsert_queued(
            &self,
            id: Uuid,
            tenant_id: &str,
            type_id: Option<&str>,
            page_size: i32,
        ) -> Result<JobRecord, StateError> {
            let type_id_db = type_id.unwrap_or("");
            sqlx::query(
                r#"
                INSERT INTO reindex_coordinator.reindex_jobs
                    (id, tenant_id, type_id, status, page_size)
                VALUES ($1, $2, $3, 'queued', $4)
                ON CONFLICT (id) DO NOTHING
                "#,
            )
            .bind(id)
            .bind(tenant_id)
            .bind(type_id_db)
            .bind(page_size)
            .execute(&self.pool)
            .await?;
            self.load(id).await
        }

        /// Load by job id. Returns `StateError::Sqlx(RowNotFound)`
        /// if the row does not exist.
        pub async fn load(&self, id: Uuid) -> Result<JobRecord, StateError> {
            let row = sqlx::query(
                r#"
                SELECT id, tenant_id, type_id, status, resume_token, page_size,
                       scanned, published, error, started_at, updated_at, completed_at
                FROM reindex_coordinator.reindex_jobs
                WHERE id = $1
                "#,
            )
            .bind(id)
            .fetch_one(&self.pool)
            .await?;
            row_to_record(row)
        }

        /// Every job whose status is `queued` or `running`.
        /// Used at startup to resume in-flight jobs after a crash
        /// before draining the Kafka request topic.
        pub async fn list_resumable(&self) -> Result<Vec<JobRecord>, StateError> {
            let rows = sqlx::query(
                r#"
                SELECT id, tenant_id, type_id, status, resume_token, page_size,
                       scanned, published, error, started_at, updated_at, completed_at
                FROM reindex_coordinator.reindex_jobs
                WHERE status IN ('queued', 'running')
                ORDER BY started_at ASC
                "#,
            )
            .fetch_all(&self.pool)
            .await?;
            rows.into_iter().map(row_to_record).collect()
        }

        /// Mark the job as `running`. Also clears any leftover
        /// `error` from a previous failed run that's been re-queued
        /// (today this is unreachable but the DDL allows it).
        pub async fn mark_running(&self, id: Uuid) -> Result<(), StateError> {
            let current = self.load(id).await?;
            current.status.validate_transition(JobStatus::Running)?;
            let result = sqlx::query(
                r#"
                UPDATE reindex_coordinator.reindex_jobs
                   SET status = 'running',
                       error = NULL,
                       updated_at = now()
                 WHERE id = $1
                   AND status IN ('queued', 'running')
                "#,
            )
            .bind(id)
            .execute(&self.pool)
            .await?;
            if result.rows_affected() == 0 {
                // Lost the race; reload and surface a typed error.
                let now = self.load(id).await?;
                return Err(StateError::IllegalTransition {
                    from: now.status,
                    to: JobStatus::Running,
                });
            }
            Ok(())
        }

        /// Persist a successful page: bump cumulative counters and
        /// move the cursor. Idempotent at the SQL layer because
        /// the event-id dedup happens above (per-batch
        /// `processed_events` row), but we still gate on
        /// `status = 'running'` so a concurrent cancel wins.
        pub async fn advance(
            &self,
            id: Uuid,
            next_resume_token: Option<&str>,
            scanned_delta: i64,
            published_delta: i64,
        ) -> Result<(), StateError> {
            let result = sqlx::query(
                r#"
                UPDATE reindex_coordinator.reindex_jobs
                   SET resume_token = $2,
                       scanned   = scanned   + $3,
                       published = published + $4,
                       updated_at = now()
                 WHERE id = $1
                   AND status = 'running'
                "#,
            )
            .bind(id)
            .bind(next_resume_token)
            .bind(scanned_delta)
            .bind(published_delta)
            .execute(&self.pool)
            .await?;
            if result.rows_affected() == 0 {
                let now = self.load(id).await?;
                return Err(StateError::IllegalTransition {
                    from: now.status,
                    to: JobStatus::Running,
                });
            }
            Ok(())
        }

        /// Terminal transition. `error` is required iff
        /// `next == Failed`. Validated client-side; the SQL CHECK
        /// constraint validates the status string server-side as a
        /// belt-and-braces.
        pub async fn mark_terminal(
            &self,
            id: Uuid,
            next: JobStatus,
            error: Option<&str>,
        ) -> Result<JobRecord, StateError> {
            if !next.is_terminal() {
                return Err(StateError::IllegalTransition {
                    from: JobStatus::Running,
                    to: next,
                });
            }
            let current = self.load(id).await?;
            // Idempotent self-loop: re-marking an already-terminal
            // row with the same status is a no-op so a redelivery
            // does not raise.
            if current.status == next {
                return Ok(current);
            }
            current.status.validate_transition(next)?;
            let result = sqlx::query(
                r#"
                UPDATE reindex_coordinator.reindex_jobs
                   SET status = $2,
                       error = $3,
                       updated_at = now(),
                       completed_at = now()
                 WHERE id = $1
                   AND status IN ('queued', 'running')
                "#,
            )
            .bind(id)
            .bind(next.as_str())
            .bind(error)
            .execute(&self.pool)
            .await?;
            if result.rows_affected() == 0 {
                let now = self.load(id).await?;
                return Err(StateError::IllegalTransition {
                    from: now.status,
                    to: next,
                });
            }
            self.load(id).await
        }
    }

    fn row_to_record(row: sqlx::postgres::PgRow) -> Result<JobRecord, StateError> {
        let type_id_db: String = row.try_get("type_id")?;
        let type_id = if type_id_db.is_empty() {
            None
        } else {
            Some(type_id_db)
        };
        let status: String = row.try_get("status")?;
        Ok(JobRecord {
            id: row.try_get("id")?,
            tenant_id: row.try_get("tenant_id")?,
            type_id,
            status: JobStatus::parse(&status)?,
            resume_token: row.try_get("resume_token")?,
            page_size: row.try_get("page_size")?,
            scanned: row.try_get("scanned")?,
            published: row.try_get("published")?,
            error: row.try_get("error")?,
            started_at: row.try_get("started_at")?,
            updated_at: row.try_get("updated_at")?,
            completed_at: row.try_get("completed_at")?,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_round_trips() {
        for s in [
            JobStatus::Queued,
            JobStatus::Running,
            JobStatus::Completed,
            JobStatus::Failed,
            JobStatus::Cancelled,
        ] {
            assert_eq!(JobStatus::parse(s.as_str()).unwrap(), s);
        }
    }

    #[test]
    fn parse_rejects_unknown() {
        let err = JobStatus::parse("explosion").unwrap_err();
        assert!(matches!(err, StateError::UnknownStatus(s) if s == "explosion"));
    }

    #[test]
    fn terminal_classification() {
        assert!(!JobStatus::Queued.is_terminal());
        assert!(!JobStatus::Running.is_terminal());
        assert!(JobStatus::Completed.is_terminal());
        assert!(JobStatus::Failed.is_terminal());
        assert!(JobStatus::Cancelled.is_terminal());
    }

    #[test]
    fn legal_transitions() {
        // Queued → {Running, Failed, Cancelled}
        assert!(JobStatus::Queued.can_transition_to(JobStatus::Running));
        assert!(JobStatus::Queued.can_transition_to(JobStatus::Failed));
        assert!(JobStatus::Queued.can_transition_to(JobStatus::Cancelled));
        // Queued → Completed is illegal (must run at least once).
        assert!(!JobStatus::Queued.can_transition_to(JobStatus::Completed));

        // Running → {Completed, Failed, Cancelled}
        assert!(JobStatus::Running.can_transition_to(JobStatus::Completed));
        assert!(JobStatus::Running.can_transition_to(JobStatus::Failed));
        assert!(JobStatus::Running.can_transition_to(JobStatus::Cancelled));
        // Running → Queued is illegal (no resurrection).
        assert!(!JobStatus::Running.can_transition_to(JobStatus::Queued));
    }

    #[test]
    fn terminal_states_do_not_resurrect() {
        for terminal in [
            JobStatus::Completed,
            JobStatus::Failed,
            JobStatus::Cancelled,
        ] {
            for next in [JobStatus::Queued, JobStatus::Running] {
                assert!(
                    !terminal.can_transition_to(next),
                    "{terminal} → {next} must be forbidden",
                );
            }
            // Self-loop is allowed (idempotent re-mark).
            assert!(terminal.can_transition_to(terminal));
            // Cross-terminal is forbidden.
            for other in [
                JobStatus::Completed,
                JobStatus::Failed,
                JobStatus::Cancelled,
            ] {
                if other != terminal {
                    assert!(
                        !terminal.can_transition_to(other),
                        "{terminal} → {other} must be forbidden",
                    );
                }
            }
        }
    }

    #[test]
    fn validate_transition_returns_typed_error() {
        let err = JobStatus::Queued
            .validate_transition(JobStatus::Completed)
            .unwrap_err();
        match err {
            StateError::IllegalTransition { from, to } => {
                assert_eq!(from, JobStatus::Queued);
                assert_eq!(to, JobStatus::Completed);
            }
            other => panic!("unexpected error variant: {other:?}"),
        }
    }
}
