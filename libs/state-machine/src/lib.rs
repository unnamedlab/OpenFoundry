//! Postgres-backed state machine helper for OpenFoundry's
//! Foundry-pattern orchestration substrate (ADR-0037).
//!
//! ## What this crate is
//!
//! A small, dependency-light helper that lets a service own a
//! state-machine table in its own Postgres schema while keeping the
//! mechanical parts — atomic transitions with optimistic concurrency,
//! JSON serialisation, timeout sweeps, and bounded retry — out of the
//! domain code.
//!
//! The intended consumers are `approvals-service` and
//! `workflow-automation-service` (per task 1.1 of
//! `docs/architecture/migration-plan-foundry-pattern-orchestration.md`),
//! plus any future flow that persists "where the data lives".
//!
//! ## Contract
//!
//! Each consumer implements [`StateMachine`] for its aggregate type and
//! creates a backing table whose shape matches
//! `migrations/0001_state_machine_template.sql`. The standard columns
//! are:
//!
//! | column       | purpose                                                       |
//! | ------------ | ------------------------------------------------------------- |
//! | `id`         | aggregate identifier, returned by `aggregate_id()`            |
//! | `state`      | textual rendering of `current_state()` (queryable)            |
//! | `state_data` | full machine serialised as JSON                               |
//! | `version`    | optimistic concurrency token — bumped on every `apply`        |
//! | `expires_at` | optional timeout deadline used by `timeout_sweep`             |
//! | `created_at` | inserted by `insert`                                          |
//! | `updated_at` | bumped by every `apply`                                       |
//!
//! Atomic transition is one statement:
//!
//! ```sql
//! UPDATE <table>
//!    SET state = $1, state_data = $2, version = version + 1,
//!        expires_at = $3, updated_at = now()
//!  WHERE id = $4 AND version = $5
//!  RETURNING version
//! ```
//!
//! A zero-row return raises [`StoreError::Stale`] so the caller can
//! reload and retry — see [`with_retry`] for a backoff helper.
//!
//! ## Example
//!
//! ```ignore
//! use chrono::{DateTime, Duration, Utc};
//! use serde::{Deserialize, Serialize};
//! use state_machine::{PgStore, StateMachine, TransitionError};
//! use uuid::Uuid;
//!
//! #[derive(Copy, Clone, Eq, PartialEq, Debug, Serialize, Deserialize)]
//! #[serde(rename_all = "snake_case")]
//! enum ApprovalState { Pending, AwaitingApproval, Approved, Rejected, TimedOut }
//!
//! enum ApprovalEvent { Submit, Approve, Reject, Timeout }
//!
//! #[derive(Clone, Debug, Serialize, Deserialize)]
//! struct ApprovalRequest {
//!     id: Uuid,
//!     state: ApprovalState,
//!     deadline: Option<DateTime<Utc>>,
//! }
//!
//! impl StateMachine for ApprovalRequest {
//!     type State = ApprovalState;
//!     type Event = ApprovalEvent;
//!
//!     fn aggregate_id(&self) -> Uuid { self.id }
//!     fn current_state(&self) -> Self::State { self.state }
//!     fn expires_at(&self) -> Option<DateTime<Utc>> { self.deadline }
//!
//!     fn state_str(state: Self::State) -> String {
//!         match state {
//!             ApprovalState::Pending => "pending",
//!             ApprovalState::AwaitingApproval => "awaiting_approval",
//!             ApprovalState::Approved => "approved",
//!             ApprovalState::Rejected => "rejected",
//!             ApprovalState::TimedOut => "timed_out",
//!         }
//!         .to_string()
//!     }
//!
//!     fn transition(mut self, event: Self::Event) -> Result<Self, TransitionError> {
//!         use ApprovalEvent::*;
//!         use ApprovalState::*;
//!         self.state = match (self.state, event) {
//!             (Pending, Submit) => {
//!                 self.deadline = Some(Utc::now() + Duration::days(7));
//!                 AwaitingApproval
//!             }
//!             (AwaitingApproval, Approve) => Approved,
//!             (AwaitingApproval, Reject)  => Rejected,
//!             (AwaitingApproval, Timeout) => TimedOut,
//!             (s, _) => return Err(TransitionError::invalid(format!("no transition from {s:?}"))),
//!         };
//!         Ok(self)
//!     }
//! }
//! ```

use std::fmt::Debug;
use std::marker::PhantomData;
use std::time::Duration;

use chrono::{DateTime, Utc};
use serde::Serialize;
use serde::de::DeserializeOwned;
use sqlx::{PgPool, Row};
use thiserror::Error;
use uuid::Uuid;

/// Error returned by [`StateMachine::transition`] when an event is not
/// applicable in the current state.
#[derive(Debug, Error)]
#[error("invalid transition: {message}")]
pub struct TransitionError {
    pub message: String,
}

impl TransitionError {
    pub fn invalid(message: impl Into<String>) -> Self {
        Self {
            message: message.into(),
        }
    }
}

/// Errors raised by [`PgStore`] when persisting / loading machines.
#[derive(Debug, Error)]
pub enum StoreError {
    /// Underlying Postgres error.
    #[error("database error: {0}")]
    Db(#[from] sqlx::Error),

    /// JSON encode/decode of `state_data` failed.
    #[error("serialize state: {0}")]
    Serialize(#[from] serde_json::Error),

    /// Domain-level transition refused by the machine.
    #[error("transition: {0}")]
    Transition(#[from] TransitionError),

    /// Optimistic concurrency conflict — the row was modified between
    /// `load` and `apply`. The caller should reload and retry.
    #[error("stale write: row id={id} expected version={expected}")]
    Stale { id: Uuid, expected: i64 },

    /// `load` was called for an id that does not exist.
    #[error("not found: id={0}")]
    NotFound(Uuid),

    /// The `state` column held a value the implementor's
    /// deserialiser rejected. Indicates schema drift between code and
    /// data.
    #[error("invalid persisted state for id={id}: {message}")]
    InvalidState { id: Uuid, message: String },
}

/// Convenience alias.
pub type StoreResult<T> = Result<T, StoreError>;

/// State-machine contract implemented by the aggregate type.
///
/// The machine is responsible for the pure transition logic; the
/// persistence boilerplate lives in [`PgStore`]. Implementors are
/// expected to be `Serialize + DeserializeOwned` so the helper can
/// store the machine in the `state_data` column without each consumer
/// inventing its own encoding.
pub trait StateMachine: Sized + Send + Sync + Serialize + DeserializeOwned {
    /// Discriminator type for the current state. Should be small,
    /// `Copy` and string-renderable via [`StateMachine::state_str`].
    type State: Copy + Eq + Debug + Send + Sync;

    /// Domain event applied by [`StateMachine::transition`].
    type Event: Send;

    /// Compute the next machine, or refuse the event with
    /// [`TransitionError`].
    fn transition(self, event: Self::Event) -> Result<Self, TransitionError>;

    /// Current state — written verbatim into the `state` column.
    fn current_state(&self) -> Self::State;

    /// Aggregate identifier, used as the primary key.
    fn aggregate_id(&self) -> Uuid;

    /// Optional timeout deadline. Returning `None` means the row will
    /// not be picked up by [`PgStore::timeout_sweep`].
    fn expires_at(&self) -> Option<DateTime<Utc>> {
        None
    }

    /// Render `state` for the queryable `state` column. The default
    /// implementation uses the [`Debug`] formatter; consumers that
    /// want a stable wire format (recommended for operators and
    /// dashboards) should override this with an explicit mapping.
    fn state_str(state: Self::State) -> String {
        format!("{state:?}")
    }
}

/// A `StateMachine` plus the optimistic-concurrency `version` it was
/// loaded with. Returned by every read/write entry point of
/// [`PgStore`]. The caller MUST round-trip the same `Loaded` (or a
/// freshly reloaded one) into [`PgStore::apply`] for the version
/// guard to do its job.
#[derive(Debug, Clone)]
pub struct Loaded<T> {
    pub machine: T,
    pub version: i64,
}

/// Postgres-backed store for a single state-machine type.
///
/// Stateless beyond the connection pool and the table name — clone or
/// share freely across tasks.
pub struct PgStore<T: StateMachine> {
    pool: PgPool,
    table: &'static str,
    _phantom: PhantomData<T>,
}

impl<T: StateMachine> Clone for PgStore<T> {
    fn clone(&self) -> Self {
        Self {
            pool: self.pool.clone(),
            table: self.table,
            _phantom: PhantomData,
        }
    }
}

impl<T: StateMachine> PgStore<T> {
    /// Build a store bound to `table`. The table name is interpolated
    /// directly into the SQL — pass a constant or a value validated at
    /// service boot time. SQL identifier rules apply (letters, digits,
    /// underscore, optional schema prefix).
    pub fn new(pool: PgPool, table: &'static str) -> Self {
        Self {
            pool,
            table,
            _phantom: PhantomData,
        }
    }

    /// Insert a fresh machine. Fails if a row with the same id already
    /// exists (use [`PgStore::load`] + [`PgStore::apply`] to update).
    pub async fn insert(&self, machine: T) -> StoreResult<Loaded<T>> {
        let state = T::state_str(machine.current_state());
        let payload = serde_json::to_value(&machine)?;
        let expires_at = machine.expires_at();
        let id = machine.aggregate_id();

        let sql = format!(
            "INSERT INTO {table} \
             (id, state, state_data, version, expires_at, created_at, updated_at) \
             VALUES ($1, $2, $3, 1, $4, now(), now()) \
             RETURNING version",
            table = self.table,
        );

        let row = sqlx::query(&sql)
            .bind(id)
            .bind(&state)
            .bind(&payload)
            .bind(expires_at)
            .fetch_one(&self.pool)
            .await?;

        let version: i64 = row.try_get("version")?;
        Ok(Loaded { machine, version })
    }

    /// Load the machine + version for `id`. Returns
    /// [`StoreError::NotFound`] if no row matches.
    pub async fn load(&self, id: Uuid) -> StoreResult<Loaded<T>> {
        let sql = format!(
            "SELECT state_data, version FROM {table} WHERE id = $1",
            table = self.table,
        );

        let maybe_row = sqlx::query(&sql)
            .bind(id)
            .fetch_optional(&self.pool)
            .await?;
        let row = maybe_row.ok_or(StoreError::NotFound(id))?;
        let payload: serde_json::Value = row.try_get("state_data")?;
        let version: i64 = row.try_get("version")?;
        let machine: T =
            serde_json::from_value(payload).map_err(|err| StoreError::InvalidState {
                id,
                message: err.to_string(),
            })?;
        Ok(Loaded { machine, version })
    }

    /// Atomically apply `event` to `loaded` and persist the result.
    ///
    /// Runs the implementor's pure [`StateMachine::transition`], then
    /// writes the new machine via
    ///
    /// ```sql
    /// UPDATE <table>
    ///    SET state = $1, state_data = $2, version = version + 1,
    ///        expires_at = $3, updated_at = now()
    ///  WHERE id = $4 AND version = $5
    ///  RETURNING version
    /// ```
    ///
    /// If the `WHERE` clause finds no row, another writer beat us to
    /// it — the call returns [`StoreError::Stale`] and the caller
    /// should reload and retry (see [`with_retry`]).
    pub async fn apply(&self, loaded: Loaded<T>, event: T::Event) -> StoreResult<Loaded<T>> {
        let Loaded { machine, version } = loaded;
        let id = machine.aggregate_id();
        let next = machine.transition(event)?;
        let state = T::state_str(next.current_state());
        let payload = serde_json::to_value(&next)?;
        let expires_at = next.expires_at();

        let sql = format!(
            "UPDATE {table} \
             SET state = $1, state_data = $2, version = version + 1, \
                 expires_at = $3, updated_at = now() \
             WHERE id = $4 AND version = $5 \
             RETURNING version",
            table = self.table,
        );

        let maybe_row = sqlx::query(&sql)
            .bind(&state)
            .bind(&payload)
            .bind(expires_at)
            .bind(id)
            .bind(version)
            .fetch_optional(&self.pool)
            .await?;

        let row = maybe_row.ok_or(StoreError::Stale {
            id,
            expected: version,
        })?;
        let new_version: i64 = row.try_get("version")?;
        Ok(Loaded {
            machine: next,
            version: new_version,
        })
    }

    /// Return every row whose `expires_at` is `<= now`. The caller is
    /// responsible for applying the appropriate timeout event via
    /// [`PgStore::apply`] (so the optimistic-lock guard still fires
    /// against concurrent writers).
    pub async fn timeout_sweep(&self, now: DateTime<Utc>) -> StoreResult<Vec<Loaded<T>>> {
        let sql = format!(
            "SELECT id, state_data, version FROM {table} \
             WHERE expires_at IS NOT NULL AND expires_at <= $1 \
             ORDER BY expires_at",
            table = self.table,
        );

        let rows = sqlx::query(&sql).bind(now).fetch_all(&self.pool).await?;
        let mut out = Vec::with_capacity(rows.len());
        for row in rows {
            let id: Uuid = row.try_get("id")?;
            let payload: serde_json::Value = row.try_get("state_data")?;
            let version: i64 = row.try_get("version")?;
            let machine: T =
                serde_json::from_value(payload).map_err(|err| StoreError::InvalidState {
                    id,
                    message: err.to_string(),
                })?;
            out.push(Loaded { machine, version });
        }
        Ok(out)
    }
}

/// Run `op` up to `max_attempts` times, retrying only on
/// [`StoreError::Stale`] with capped exponential backoff (initial
/// `base_delay`, doubled on every retry, max `1s`). Useful when the
/// caller prefers to swallow contention rather than surface it.
///
/// The closure receives the attempt number (1-based) so it can
/// `load()` afresh every time, since the optimistic-lock token is
/// invalidated on every conflict.
pub async fn with_retry<F, Fut, T>(
    max_attempts: u32,
    base_delay: Duration,
    mut op: F,
) -> StoreResult<T>
where
    F: FnMut(u32) -> Fut,
    Fut: std::future::Future<Output = StoreResult<T>>,
{
    let mut attempt: u32 = 0;
    let mut delay = base_delay;
    let cap = Duration::from_secs(1);
    loop {
        attempt += 1;
        match op(attempt).await {
            Ok(value) => return Ok(value),
            Err(StoreError::Stale { id, expected }) if attempt < max_attempts => {
                tracing::debug!(
                    %id,
                    expected,
                    attempt,
                    backoff_ms = delay.as_millis() as u64,
                    "state_machine: stale write, retrying"
                );
                tokio::time::sleep(delay).await;
                delay = (delay * 2).min(cap);
            }
            Err(err) => return Err(err),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde::{Deserialize, Serialize};

    #[derive(Copy, Clone, Eq, PartialEq, Debug, Serialize, Deserialize)]
    #[serde(rename_all = "snake_case")]
    enum DemoState {
        Pending,
        Done,
    }

    enum DemoEvent {
        Finish,
    }

    #[derive(Clone, Debug, Serialize, Deserialize)]
    struct DemoMachine {
        id: Uuid,
        state: DemoState,
    }

    impl StateMachine for DemoMachine {
        type State = DemoState;
        type Event = DemoEvent;

        fn transition(mut self, event: Self::Event) -> Result<Self, TransitionError> {
            match (self.state, event) {
                (DemoState::Pending, DemoEvent::Finish) => {
                    self.state = DemoState::Done;
                    Ok(self)
                }
                (s, _) => Err(TransitionError::invalid(format!(
                    "no transition from {s:?}"
                ))),
            }
        }

        fn current_state(&self) -> Self::State {
            self.state
        }

        fn aggregate_id(&self) -> Uuid {
            self.id
        }

        fn state_str(state: Self::State) -> String {
            match state {
                DemoState::Pending => "pending".to_string(),
                DemoState::Done => "done".to_string(),
            }
        }
    }

    #[test]
    fn transition_happy_path() {
        let m = DemoMachine {
            id: Uuid::nil(),
            state: DemoState::Pending,
        };
        let next = m.transition(DemoEvent::Finish).expect("transition ok");
        assert_eq!(next.current_state(), DemoState::Done);
    }

    #[test]
    fn transition_rejects_invalid_event() {
        let m = DemoMachine {
            id: Uuid::nil(),
            state: DemoState::Done,
        };
        let err = m.transition(DemoEvent::Finish).expect_err("must reject");
        assert!(err.message.contains("no transition"), "msg: {err}");
    }

    #[test]
    fn state_str_default_uses_debug() {
        // Sanity check that the default `state_str` does not panic for
        // well-formed `Debug` impls. Real consumers override it; the
        // default exists only as a fallback.
        #[derive(Copy, Clone, Eq, PartialEq, Debug, Serialize, Deserialize)]
        struct OpaqueState;

        #[derive(Clone, Debug, Serialize, Deserialize)]
        struct Inert {
            id: Uuid,
        }

        impl StateMachine for Inert {
            type State = OpaqueState;
            type Event = ();

            fn transition(self, _event: Self::Event) -> Result<Self, TransitionError> {
                Err(TransitionError::invalid("inert"))
            }
            fn current_state(&self) -> Self::State {
                OpaqueState
            }
            fn aggregate_id(&self) -> Uuid {
                self.id
            }
        }

        let rendered = <Inert as StateMachine>::state_str(OpaqueState);
        assert_eq!(rendered, "OpaqueState");
    }

    #[tokio::test]
    async fn with_retry_returns_immediately_on_success() {
        let result: StoreResult<u32> = with_retry(
            3,
            Duration::from_millis(1),
            |attempt| async move { Ok(attempt) },
        )
        .await;
        assert_eq!(result.unwrap(), 1);
    }

    #[tokio::test]
    async fn with_retry_gives_up_after_max_attempts() {
        let result: StoreResult<u32> = with_retry(3, Duration::from_millis(1), |_attempt| async {
            Err(StoreError::Stale {
                id: Uuid::nil(),
                expected: 0,
            })
        })
        .await;
        assert!(matches!(result, Err(StoreError::Stale { .. })));
    }

    #[tokio::test]
    async fn with_retry_stops_on_non_stale_error() {
        use std::sync::atomic::{AtomicU32, Ordering};
        let calls = AtomicU32::new(0);
        let result: StoreResult<u32> = with_retry(5, Duration::from_millis(1), |_attempt| {
            calls.fetch_add(1, Ordering::SeqCst);
            async {
                Err(StoreError::Transition(TransitionError::invalid(
                    "permanent",
                )))
            }
        })
        .await;
        assert!(matches!(result, Err(StoreError::Transition(_))));
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }
}
