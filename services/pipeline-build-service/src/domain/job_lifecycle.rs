//! Job state-machine. Mirrors Foundry "Builds.md § Job states" verbatim.
//!
//! ```text
//!   WAITING ──┬─→ RUN_PENDING ─→ RUNNING ─┬─→ COMPLETED
//!             │                            ├─→ FAILED
//!             │                            └─→ ABORT_PENDING ─→ ABORTED
//!             └────────────────────────────→ ABORTED       (cascading abort)
//! ```
//!
//! Any transition not listed above is rejected by
//! [`is_valid_transition`] and propagated as
//! [`JobLifecycleError::InvalidTransition`]. The high-level
//! [`transition_job`] helper performs the row update and the
//! audit-trail insert in a single transaction so the
//! `job_state_transitions` table is always in sync with `jobs.state`.

use chrono::Utc;
use sqlx::{PgPool, Postgres, Transaction};
use uuid::Uuid;

use crate::models::job::{JobState, UnknownJobState};

#[derive(Debug, thiserror::Error)]
pub enum JobLifecycleError {
    #[error("invalid job state transition {from} → {to}")]
    InvalidTransition { from: JobState, to: JobState },
    #[error("unknown job state in db: {0}")]
    UnknownState(#[from] UnknownJobState),
    #[error("job {0} not found")]
    NotFound(Uuid),
    #[error(transparent)]
    Db(#[from] sqlx::Error),
}

/// Returns true when `from → to` is one of the explicit edges in the
/// lifecycle diagram.
pub fn is_valid_transition(from: JobState, to: JobState) -> bool {
    use JobState::*;
    matches!(
        (from, to),
        (Waiting, RunPending)
            | (Waiting, Aborted)
            | (Waiting, AbortPending)
            | (RunPending, Running)
            | (RunPending, AbortPending)
            | (RunPending, Failed)
            | (Running, Completed)
            | (Running, Failed)
            | (Running, AbortPending)
            | (AbortPending, Aborted)
    )
}

/// Apply a transition + audit insert atomically against an open
/// transaction. Caller is responsible for committing.
pub async fn transition_job_in_tx<'c>(
    tx: &mut Transaction<'c, Postgres>,
    job_id: Uuid,
    expected_from: Option<JobState>,
    to: JobState,
    reason: Option<&str>,
) -> Result<JobState, JobLifecycleError> {
    let row: Option<(String,)> =
        sqlx::query_as("SELECT state FROM jobs WHERE id = $1 FOR UPDATE")
            .bind(job_id)
            .fetch_optional(&mut **tx)
            .await?;

    let from = match row {
        Some((state_str,)) => state_str.parse::<JobState>()?,
        None => return Err(JobLifecycleError::NotFound(job_id)),
    };

    if let Some(expected) = expected_from {
        if expected != from {
            return Err(JobLifecycleError::InvalidTransition { from, to });
        }
    }

    if from == to {
        // No-op transitions are tolerated by callers that re-enter the
        // same state (idempotent retries) but never recorded twice in
        // the audit log.
        return Ok(from);
    }

    if !is_valid_transition(from, to) {
        return Err(JobLifecycleError::InvalidTransition { from, to });
    }

    sqlx::query("UPDATE jobs SET state = $1, state_changed_at = $2 WHERE id = $3")
        .bind(to.as_str())
        .bind(Utc::now())
        .bind(job_id)
        .execute(&mut **tx)
        .await?;

    sqlx::query(
        r#"INSERT INTO job_state_transitions (job_id, from_state, to_state, reason)
           VALUES ($1, $2, $3, $4)"#,
    )
    .bind(job_id)
    .bind(from.as_str())
    .bind(to.as_str())
    .bind(reason)
    .execute(&mut **tx)
    .await?;

    Ok(from)
}

/// Convenience wrapper over [`transition_job_in_tx`] that opens its own
/// transaction. Use this when no other DB writes need to be atomic with
/// the state change.
pub async fn transition_job(
    pool: &PgPool,
    job_id: Uuid,
    expected_from: Option<JobState>,
    to: JobState,
    reason: Option<&str>,
) -> Result<JobState, JobLifecycleError> {
    let mut tx = pool.begin().await?;
    let from = transition_job_in_tx(&mut tx, job_id, expected_from, to, reason).await?;
    tx.commit().await?;
    Ok(from)
}

#[cfg(test)]
mod tests {
    use super::*;
    use JobState::*;

    #[test]
    fn happy_path_transitions_are_valid() {
        assert!(is_valid_transition(Waiting, RunPending));
        assert!(is_valid_transition(RunPending, Running));
        assert!(is_valid_transition(Running, Completed));
        assert!(is_valid_transition(Running, Failed));
    }

    #[test]
    fn abort_paths_are_valid() {
        assert!(is_valid_transition(Running, AbortPending));
        assert!(is_valid_transition(RunPending, AbortPending));
        assert!(is_valid_transition(AbortPending, Aborted));
        assert!(is_valid_transition(Waiting, Aborted));
        assert!(is_valid_transition(Waiting, AbortPending));
    }

    #[test]
    fn skipping_states_is_rejected() {
        // WAITING cannot jump to RUNNING.
        assert!(!is_valid_transition(Waiting, Running));
        // COMPLETED is terminal — no transitions out.
        assert!(!is_valid_transition(Completed, Failed));
        assert!(!is_valid_transition(Completed, Running));
        // FAILED is terminal.
        assert!(!is_valid_transition(Failed, Running));
        // Direct RUNNING → ABORTED bypasses ABORT_PENDING.
        assert!(!is_valid_transition(Running, Aborted));
    }

    #[test]
    fn terminal_states_have_no_outgoing_edges() {
        for &terminal in &[Completed, Failed, Aborted] {
            for &target in JobState::ALL {
                assert!(
                    !is_valid_transition(terminal, target),
                    "{terminal:?} → {target:?} must be rejected"
                );
            }
        }
    }
}
