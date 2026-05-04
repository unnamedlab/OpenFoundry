//! Consumer-side deduplication helper for ADR-0038.
//!
//! Every OpenFoundry consumer of the data plane (Kafka topics
//! delivered at-least-once) must dedupe by `event_id` so a redelivery
//! does not produce side effects twice. This crate gives them a
//! single, atomic primitive instead of every team rolling its own.
//!
//! ## Trait
//!
//! ```ignore
//! #[async_trait]
//! pub trait IdempotencyStore: Send + Sync {
//!     async fn check_and_record(&self, event_id: Uuid)
//!         -> Result<Outcome, IdempotencyError>;
//! }
//! ```
//!
//! `check_and_record` is **atomic** — for any given `event_id`, at
//! most one caller across the whole cluster ever sees
//! [`Outcome::FirstSeen`]; everyone else sees
//! [`Outcome::AlreadyProcessed`]. Backends:
//!
//! * [`postgres::PgIdempotencyStore`] — `INSERT … ON CONFLICT DO
//!   NOTHING RETURNING event_id`. One round-trip, no race window.
//! * [`cassandra::CassandraIdempotencyStore`] — `INSERT … IF NOT
//!   EXISTS` (Lightweight Transaction at `LOCAL_SERIAL`). 4× the
//!   cost of a regular write but the only Cassandra primitive that
//!   gives this guarantee.
//! * [`MemoryIdempotencyStore`] — in-process, for unit tests.
//!
//! ## Wrapper
//!
//! [`idempotent`] composes the store with a closure: it returns
//! `Ok(None)` when the event has already been processed and
//! `Ok(Some(T))` when the closure ran on a first delivery.
//!
//! ## Semantics — record-before-process
//!
//! `check_and_record` records the `event_id` **before** the consumer
//! has finished processing. This is the only ordering that's safe
//! when two consumers race on the same event id, but the trade-off
//! is: a closure that fails after `FirstSeen` does NOT un-record the
//! row, so the next redelivery sees `AlreadyProcessed` and skips.
//! Wrap the side effects of your consumer in their own transactional
//! outbox / saga so that "we recorded the event_id, then crashed" is
//! not a silent data loss.

#![forbid(unsafe_code)]

use std::collections::HashSet;
use std::future::Future;
use std::sync::Mutex;

use async_trait::async_trait;
use thiserror::Error;
use uuid::Uuid;

#[cfg(feature = "cassandra")]
pub mod cassandra;
#[cfg(feature = "postgres")]
pub mod postgres;

// ─── Public types ────────────────────────────────────────────────────────

/// Result of a [`IdempotencyStore::check_and_record`] call.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Outcome {
    /// This `event_id` was not previously known to the store. The
    /// caller is the unique owner of "first delivery" and should
    /// proceed with side effects.
    FirstSeen,
    /// This `event_id` was already recorded — by an earlier delivery
    /// of this consumer or by a sibling instance. The caller MUST
    /// skip side effects.
    AlreadyProcessed,
}

impl Outcome {
    /// Convenience: `true` when the caller should run the work.
    pub fn is_first_seen(self) -> bool {
        matches!(self, Outcome::FirstSeen)
    }

    /// Convenience: `true` when the caller should skip the work.
    pub fn is_already_processed(self) -> bool {
        matches!(self, Outcome::AlreadyProcessed)
    }
}

/// Errors raised by an [`IdempotencyStore`].
#[derive(Debug, Error)]
pub enum IdempotencyError {
    /// Underlying storage error (Postgres, Cassandra, …).
    #[error("storage backend error: {0}")]
    Backend(String),
}

impl IdempotencyError {
    /// Construct from any error type by stringifying it. Backends
    /// use this so the public API stays driver-agnostic.
    pub fn backend<E: std::fmt::Display>(err: E) -> Self {
        Self::Backend(err.to_string())
    }
}

/// Atomic "is this event new?" primitive. Implementations MUST make
/// the check-and-record sequence atomic across concurrent callers.
#[async_trait]
pub trait IdempotencyStore: Send + Sync {
    /// Atomically record `event_id` if it is not already present and
    /// report whether the caller is the first to claim it.
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError>;
}

// Allow `&dyn IdempotencyStore`, `Arc<dyn …>`, etc. to be passed to
// the [`idempotent`] wrapper without an extra layer of `&`.
#[async_trait]
impl<S: IdempotencyStore + ?Sized> IdempotencyStore for std::sync::Arc<S> {
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError> {
        (**self).check_and_record(event_id).await
    }
}

#[async_trait]
impl<S: IdempotencyStore + ?Sized> IdempotencyStore for &S {
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError> {
        (**self).check_and_record(event_id).await
    }
}

// ─── Wrapper ─────────────────────────────────────────────────────────────

/// Errors raised by the [`idempotent`] wrapper.
#[derive(Debug, Error)]
pub enum IdempotentError<E: std::error::Error + Send + Sync + 'static> {
    /// Could not record / look up the event id.
    #[error(transparent)]
    Idempotency(#[from] IdempotencyError),
    /// The user closure ran (this was the first delivery) but
    /// returned an error. The `event_id` IS already recorded —
    /// see the module docs for the record-before-process rationale.
    #[error(transparent)]
    Process(E),
}

/// Run `f` exactly once per `event_id`.
///
/// On a first delivery: records the `event_id`, runs `f`, returns
/// `Ok(Some(T))`. If `f` returns an error, the `event_id` remains
/// recorded — see [the module documentation](self) for why.
///
/// On a redelivery: skips `f` and returns `Ok(None)`.
pub async fn idempotent<S, F, Fut, T, E>(
    store: &S,
    event_id: Uuid,
    f: F,
) -> Result<Option<T>, IdempotentError<E>>
where
    S: IdempotencyStore + ?Sized,
    F: FnOnce() -> Fut,
    Fut: Future<Output = Result<T, E>>,
    E: std::error::Error + Send + Sync + 'static,
{
    match store.check_and_record(event_id).await? {
        Outcome::AlreadyProcessed => {
            tracing::debug!(%event_id, "skipped duplicate delivery");
            Ok(None)
        }
        Outcome::FirstSeen => {
            let value = f().await.map_err(IdempotentError::Process)?;
            Ok(Some(value))
        }
    }
}

// ─── In-memory store (unit tests + dev) ──────────────────────────────────

/// Process-local [`IdempotencyStore`] backed by a `HashSet`.
///
/// **NOT** for production: dedup state evaporates on restart and
/// does not cross processes. Useful for unit tests and for
/// consumers that only need single-process dedup of recent events.
#[derive(Debug, Default)]
pub struct MemoryIdempotencyStore {
    seen: Mutex<HashSet<Uuid>>,
}

impl MemoryIdempotencyStore {
    /// Build an empty store.
    pub fn new() -> Self {
        Self::default()
    }

    /// Number of recorded `event_id`s. Mostly useful for tests.
    pub fn len(&self) -> usize {
        self.seen.lock().expect("memory store mutex poisoned").len()
    }

    /// `true` when no `event_id` has been recorded.
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
}

#[async_trait]
impl IdempotencyStore for MemoryIdempotencyStore {
    async fn check_and_record(&self, event_id: Uuid) -> Result<Outcome, IdempotencyError> {
        let mut guard = self.seen.lock().expect("memory store mutex poisoned");
        if guard.insert(event_id) {
            Ok(Outcome::FirstSeen)
        } else {
            Ok(Outcome::AlreadyProcessed)
        }
    }
}

// ─── Unit tests ─────────────────────────────────────────────────────────

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Arc;
    use std::sync::atomic::{AtomicUsize, Ordering};

    #[derive(Debug, Error)]
    #[error("boom")]
    struct Boom;

    #[tokio::test]
    async fn memory_store_first_then_duplicate() {
        let store = MemoryIdempotencyStore::new();
        let id = Uuid::now_v7();
        assert_eq!(
            store.check_and_record(id).await.unwrap(),
            Outcome::FirstSeen
        );
        assert_eq!(
            store.check_and_record(id).await.unwrap(),
            Outcome::AlreadyProcessed
        );
        assert_eq!(store.len(), 1);
    }

    #[tokio::test]
    async fn memory_store_distinct_ids_are_independent() {
        let store = MemoryIdempotencyStore::new();
        let a = Uuid::now_v7();
        let b = Uuid::now_v7();
        assert_eq!(store.check_and_record(a).await.unwrap(), Outcome::FirstSeen);
        assert_eq!(store.check_and_record(b).await.unwrap(), Outcome::FirstSeen);
        assert_eq!(store.len(), 2);
    }

    #[tokio::test]
    async fn idempotent_wrapper_runs_closure_on_first_seen() {
        let store = MemoryIdempotencyStore::new();
        let calls = AtomicUsize::new(0);
        let id = Uuid::now_v7();
        let r = idempotent::<_, _, _, _, Boom>(&store, id, || async {
            calls.fetch_add(1, Ordering::SeqCst);
            Ok(42)
        })
        .await
        .unwrap();
        assert_eq!(r, Some(42));
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn idempotent_wrapper_skips_closure_on_replay() {
        let store = MemoryIdempotencyStore::new();
        let id = Uuid::now_v7();
        // First delivery records and runs.
        let _ = idempotent::<_, _, _, _, Boom>(&store, id, || async { Ok::<_, Boom>(()) })
            .await
            .unwrap();

        // Second delivery must NOT run the closure even though the
        // closure would error if invoked.
        let calls = AtomicUsize::new(0);
        let r = idempotent::<_, _, _, _, Boom>(&store, id, || async {
            calls.fetch_add(1, Ordering::SeqCst);
            Err::<(), _>(Boom)
        })
        .await
        .unwrap();
        assert!(r.is_none());
        assert_eq!(
            calls.load(Ordering::SeqCst),
            0,
            "closure must be skipped on duplicate"
        );
    }

    #[tokio::test]
    async fn idempotent_wrapper_record_before_process_keeps_id_on_failure() {
        // ADR-0038 record-before-process invariant: a failing closure
        // must NOT un-record the event id, so the next redelivery
        // skips the side effects.
        let store = MemoryIdempotencyStore::new();
        let id = Uuid::now_v7();

        let err = idempotent::<_, _, _, (), _>(&store, id, || async { Err::<(), _>(Boom) })
            .await
            .unwrap_err();
        assert!(matches!(err, IdempotentError::Process(_)));

        let again = idempotent::<_, _, _, _, Boom>(&store, id, || async { Ok::<_, Boom>(7) })
            .await
            .unwrap();
        assert!(
            again.is_none(),
            "redelivery after a failed first attempt must be deduped"
        );
    }

    #[tokio::test]
    async fn arc_dyn_store_satisfies_trait() {
        // Compile-time check: Arc<dyn IdempotencyStore> works with
        // the wrapper. This is the shape consumers use to share a
        // store across tasks.
        let store: Arc<dyn IdempotencyStore> = Arc::new(MemoryIdempotencyStore::new());
        let id = Uuid::now_v7();
        let r = idempotent::<_, _, _, _, Boom>(&*store, id, || async { Ok::<_, Boom>(()) })
            .await
            .unwrap();
        assert!(r.is_some());
    }

    #[test]
    fn outcome_helpers() {
        assert!(Outcome::FirstSeen.is_first_seen());
        assert!(!Outcome::FirstSeen.is_already_processed());
        assert!(Outcome::AlreadyProcessed.is_already_processed());
        assert!(!Outcome::AlreadyProcessed.is_first_seen());
    }

    #[test]
    fn idempotency_error_backend_constructor() {
        let e = IdempotencyError::backend("connection refused");
        let s = format!("{e}");
        assert!(s.contains("connection refused"));
    }
}
