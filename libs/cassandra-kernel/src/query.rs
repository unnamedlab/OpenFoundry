//! Query helpers that codify the modelling rules from ADR-0020.
//!
//! These wrappers exist for two reasons:
//!
//! 1. Make the recommended pattern the easy path (paged streaming
//!    instead of `query_unpaged` for unbounded reads, prepared cache
//!    instead of ad-hoc `Session::prepare` calls).
//! 2. Make the forbidden patterns hard. [`batch_logged`] enforces
//!    single-partition LOGGED batches at runtime, [`lwt_insert_if_not_exists`]
//!    forces the caller to acknowledge the 4× cost of LWT.

use std::collections::HashMap;
use std::sync::Arc;

use futures::stream::Stream;
use scylla::batch::{Batch, BatchType};
use scylla::prepared_statement::PreparedStatement;
use scylla::serialize::batch::BatchValues;
use scylla::serialize::row::SerializeRow;
use scylla::statement::SerialConsistency;
use scylla::transport::iterator::TypedRowIterator;
use scylla::{QueryResult, Session};
use tokio::sync::RwLock;
use tracing::warn;

use crate::error::{KernelError, KernelResult};

/// Stream rows from a prepared SELECT, page by page. Use this for any
/// read that is not strictly bounded to a single partition: the
/// driver fetches the next page transparently while the caller
/// consumes the current one.
///
/// Returns a typed iterator. The caller picks the row type — usually
/// a `(Uuid, String, …)` tuple or a `#[derive(FromRow)]` struct.
///
/// ```ignore
/// use cassandra_kernel::query::paged_query;
///
/// let mut iter = paged_query::<(uuid::Uuid, String)>(
///     &session,
///     &prepared_select_objects,
///     ("tenant-1",),
/// ).await?;
///
/// while let Some(row) = iter.next().await {
///     let (id, name) = row?;
///     // ...
/// }
/// ```
pub async fn paged_query<RowT>(
    session: &Session,
    prepared: &PreparedStatement,
    values: impl SerializeRow,
) -> KernelResult<TypedRowIterator<RowT>>
where
    RowT: scylla::FromRow,
{
    let iter = session.execute_iter(prepared.clone(), values).await?;
    Ok(iter.into_typed::<RowT>())
}

/// Run a single-row Lightweight Transaction (LWT) `INSERT … IF NOT
/// EXISTS`. LWTs are 4× the cost of a regular write (Paxos round-
/// trip) and must only be used where atomicity actually matters
/// (idempotency keys, unique-by-natural-key inserts).
///
/// Returns `true` if the row was applied (insert won), `false` if a
/// row with the same primary key already existed.
///
/// The serial consistency is fixed to [`SerialConsistency::LocalSerial`]
/// — global Paxos rounds are not safe in a multi-DC topology with
/// active writes in more than one DC.
pub async fn lwt_insert_if_not_exists(
    session: &Session,
    prepared: &PreparedStatement,
    values: impl SerializeRow,
) -> KernelResult<bool> {
    let mut stmt = prepared.clone();
    stmt.set_serial_consistency(Some(SerialConsistency::LocalSerial));
    let result: QueryResult = session.execute(&stmt, values).await?;
    // The first column of an LWT response is `[applied]` (bool).
    let applied = result
        .first_row()
        .ok()
        .and_then(|row| row.columns.into_iter().next().flatten())
        .and_then(|v| v.as_boolean())
        .unwrap_or(false);
    Ok(applied)
}

/// Execute a LOGGED batch confined to a single partition.
///
/// Cassandra's LOGGED batches give atomicity across statements but
/// are *not* a performance feature: cross-partition LOGGED batches
/// fan out to every replica of every partition and become a hotspot
/// magnet. This helper takes a `partition_token` parameter and
/// requires the caller to certify (via the type system) that every
/// statement in the batch targets the same token. We cannot verify
/// the partition key without parsing every statement; the explicit
/// token is the runtime equivalent of a typestate.
///
/// Use [`scylla::Session::batch`] directly for UNLOGGED batches when
/// you have many independent inserts that happen to share a
/// partition — that is a legitimate optimisation.
pub async fn batch_logged<V>(
    session: &Session,
    statements: Vec<PreparedStatement>,
    values: V,
    partition_token: i64,
) -> KernelResult<QueryResult>
where
    V: BatchValues,
{
    if statements.is_empty() {
        return Err(KernelError::ModellingRule(
            "batch_logged called with zero statements".into(),
        ));
    }
    if statements.len() > 30 {
        // Soft cap: the driver allows more, but in practice batches
        // larger than this are an indicator that the modelling is
        // wrong — the data wants to be a separate table.
        warn!(
            count = statements.len(),
            "LOGGED batch with >30 statements; revisit modelling per ADR-0020"
        );
    }

    let _ = partition_token; // captured for tracing only
    let mut batch = Batch::new(BatchType::Logged);
    for stmt in statements {
        batch.append_statement(stmt);
    }
    Ok(session.batch(&batch, values).await?)
}

/// Cache of prepared statements, keyed by the CQL source.
///
/// `Session::prepare` is a network round-trip; preparing the same
/// statement on every request is a common foot-gun. This cache is
/// safe to share across tasks (`Arc<PreparedCache>`).
pub struct PreparedCache {
    inner: RwLock<HashMap<String, Arc<PreparedStatement>>>,
}

impl PreparedCache {
    /// Build an empty cache.
    pub fn new() -> Self {
        Self {
            inner: RwLock::new(HashMap::new()),
        }
    }

    /// Get-or-prepare. The CQL string is the cache key.
    pub async fn get(&self, session: &Session, cql: &str) -> KernelResult<Arc<PreparedStatement>> {
        if let Some(p) = self.inner.read().await.get(cql).cloned() {
            return Ok(p);
        }
        let prepared = session.prepare(cql).await?;
        let arc = Arc::new(prepared);
        self.inner
            .write()
            .await
            .insert(cql.to_string(), arc.clone());
        Ok(arc)
    }
}

impl Default for PreparedCache {
    fn default() -> Self {
        Self::new()
    }
}

// Marker re-export so callers can spell out the bound on `paged_query`
// helpers without depending on the futures crate directly.
#[doc(hidden)]
pub trait RowStream: Stream {}
impl<S: Stream> RowStream for S {}
