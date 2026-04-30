//! Apache Arrow Flight SQL [`TableProvider`] for DataFusion.
//!
//! [`FlightSqlTableProvider`] turns a remote Flight SQL endpoint into a
//! regular DataFusion table. Any data-plane service can therefore federate
//! queries against another Flight SQL service exactly as if its result set
//! were a local table:
//!
//! ```ignore
//! use std::sync::Arc;
//! use datafusion::prelude::SessionContext;
//! use query_engine::flight_provider::FlightSqlTableProvider;
//!
//! # async fn run() -> datafusion::error::Result<()> {
//! let provider = FlightSqlTableProvider::try_new(
//!     "http://flight-sql.internal:50051",
//!     "SELECT id, name FROM customers",
//! )
//! .await
//! .unwrap();
//!
//! let ctx = SessionContext::new();
//! ctx.register_table("customers", Arc::new(provider))?;
//! let df = ctx.sql("SELECT * FROM customers LIMIT 10").await?;
//! df.show().await?;
//! # Ok(()) }
//! ```
//!
//! ## Push-down support
//!
//! * **Projection** is pushed down trivially: only the requested columns
//!   are kept once the batches arrive from the remote endpoint.
//! * **Limit** is pushed down trivially: the local stream stops as soon as
//!   `limit` rows have been emitted.
//! * **Filter** push-down is intentionally **out of scope**. Translating
//!   arbitrary DataFusion `Expr`s into the SQL dialect of an unknown remote
//!   engine cannot be done safely in a generic provider. Callers that need
//!   filter push-down should embed the predicate directly in the `query`
//!   they pass to [`FlightSqlTableProvider::try_new`].

use std::any::Any;
use std::fmt;
use std::sync::Arc;

use arrow::datatypes::SchemaRef;
use arrow::record_batch::RecordBatch;
use arrow_flight::sql::client::FlightSqlServiceClient;
use async_trait::async_trait;
use datafusion::catalog::Session;
use datafusion::common::{DataFusionError, project_schema};
use datafusion::datasource::{TableProvider, TableType};
use datafusion::error::Result as DfResult;
use datafusion::execution::{SendableRecordBatchStream, TaskContext};
use datafusion::logical_expr::{Expr, TableProviderFilterPushDown};
use datafusion::physical_expr::EquivalenceProperties;
use datafusion::physical_plan::execution_plan::{Boundedness, EmissionType};
use datafusion::physical_plan::stream::RecordBatchStreamAdapter;
use datafusion::physical_plan::{
    DisplayAs, DisplayFormatType, ExecutionPlan, Partitioning, PlanProperties,
};
use futures::{StreamExt, TryStreamExt, stream};
use thiserror::Error;
use tonic::transport::{Channel, Endpoint};

/// Errors raised while building or driving a [`FlightSqlTableProvider`].
#[derive(Debug, Error)]
pub enum FlightProviderError {
    /// The supplied endpoint URL could not be parsed or reached.
    #[error("invalid Flight SQL endpoint `{endpoint}`: {source}")]
    InvalidEndpoint {
        endpoint: String,
        #[source]
        source: tonic::transport::Error,
    },
    /// The TCP/HTTP-2 connection to the remote Flight server failed.
    #[error("failed to connect to Flight SQL endpoint `{endpoint}`: {source}")]
    Connect {
        endpoint: String,
        #[source]
        source: tonic::transport::Error,
    },
    /// The remote Flight SQL server returned an Arrow-level error.
    #[error("Flight SQL error: {0}")]
    Flight(String),
    /// The Flight server replied with a [`FlightInfo`] that did not contain
    /// any endpoint (and therefore no ticket) for the query.
    #[error("Flight SQL endpoint returned no ticket for query")]
    MissingTicket,
}

impl From<FlightProviderError> for DataFusionError {
    fn from(value: FlightProviderError) -> Self {
        DataFusionError::External(Box::new(value))
    }
}

/// A [`TableProvider`] that resolves its data by issuing a SQL query against
/// a remote Apache Arrow Flight SQL endpoint.
///
/// The schema is supplied up-front by the caller because DataFusion needs it
/// at planning time, before any I/O takes place. The remote service must
/// produce a result-set that is compatible with this schema.
#[derive(Debug, Clone)]
pub struct FlightSqlTableProvider {
    endpoint: String,
    query: String,
    schema: SchemaRef,
}

impl FlightSqlTableProvider {
    /// Connect to `endpoint`, execute `query` once to retrieve the Arrow
    /// schema and return a provider that can be registered into a
    /// [`SessionContext`](datafusion::prelude::SessionContext).
    pub async fn try_new(
        endpoint: impl Into<String>,
        query: impl Into<String>,
    ) -> Result<Self, FlightProviderError> {
        let endpoint = endpoint.into();
        let query = query.into();

        let mut client = connect(&endpoint).await?;
        let info = client
            .execute(query.clone(), None)
            .await
            .map_err(|e| FlightProviderError::Flight(e.to_string()))?;
        let schema = info
            .try_decode_schema()
            .map_err(|e| FlightProviderError::Flight(e.to_string()))?;

        Ok(Self {
            endpoint,
            query,
            schema: Arc::new(schema),
        })
    }

    /// Build a provider with a known schema, skipping the upfront network
    /// round-trip used by [`Self::try_new`]. Useful when the schema is part
    /// of a service contract or has already been negotiated out-of-band.
    ///
    /// * `endpoint` – a `tonic`-compatible URI (e.g. `http://host:port`).
    /// * `query`    – the SQL statement to issue against the remote service.
    /// * `schema`   – the Arrow schema of the result-set.
    pub fn new(
        endpoint: impl Into<String>,
        query: impl Into<String>,
        schema: SchemaRef,
    ) -> Self {
        Self {
            endpoint: endpoint.into(),
            query: query.into(),
            schema,
        }
    }

    /// Returns the configured Flight SQL endpoint URL.
    /// The remote endpoint URI this provider talks to.
    pub fn endpoint(&self) -> &str {
        &self.endpoint
    }

    /// Returns the SQL statement that will be sent to the remote endpoint.
    pub fn query(&self) -> &str {
        &self.query
    }
}

#[async_trait]
impl TableProvider for FlightSqlTableProvider {
    fn as_any(&self) -> &dyn Any {
        self
    }

    fn schema(&self) -> SchemaRef {
        Arc::clone(&self.schema)
    }

    fn table_type(&self) -> TableType {
        TableType::Base
    }

    async fn scan(
        &self,
        _state: &dyn Session,
        projection: Option<&Vec<usize>>,
        _filters: &[Expr],
        limit: Option<usize>,
    ) -> DfResult<Arc<dyn ExecutionPlan>> {
        let exec = FlightSqlExec::try_new(
            self.endpoint.clone(),
            self.query.clone(),
            Arc::clone(&self.schema),
            projection.cloned(),
            limit,
        )?;
        Ok(Arc::new(exec))
    }

    /// Filter push-down is intentionally not implemented: the caller is
    /// expected to bake any predicates into the SQL `query` they hand to
    /// [`FlightSqlTableProvider::new`]. We report `Unsupported` so DataFusion
    /// re-evaluates filters locally on the batches we return.
    fn supports_filters_pushdown(
        &self,
        filters: &[&Expr],
    ) -> DfResult<Vec<TableProviderFilterPushDown>> {
        Ok(vec![TableProviderFilterPushDown::Unsupported; filters.len()])
    }
}

/// [`ExecutionPlan`] that streams batches out of a remote Flight SQL endpoint.
///
/// The plan owns the connection parameters and produces a single, bounded
/// partition. Projection and `LIMIT` are applied locally on the incoming
/// stream because the Flight SQL spec doesn't define a portable way to
/// rewrite the original statement in the remote dialect.
pub(crate) struct FlightSqlExec {
    endpoint: String,
    query: String,
    /// Schema visible to the rest of the plan after projection.
    projected_schema: SchemaRef,
    projection: Option<Vec<usize>>,
    limit: Option<usize>,
    properties: PlanProperties,
}

impl FlightSqlExec {
    fn try_new(
        endpoint: String,
        query: String,
        schema: SchemaRef,
        projection: Option<Vec<usize>>,
        limit: Option<usize>,
    ) -> DfResult<Self> {
        let projected_schema = project_schema(&schema, projection.as_ref())?;
        let properties = PlanProperties::new(
            EquivalenceProperties::new(Arc::clone(&projected_schema)),
            Partitioning::UnknownPartitioning(1),
            EmissionType::Incremental,
            Boundedness::Bounded,
        );
        Ok(Self {
            endpoint,
            query,
            projected_schema,
            projection,
            limit,
            properties,
        })
    }
}

impl fmt::Debug for FlightSqlExec {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.debug_struct("FlightSqlExec")
            .field("endpoint", &self.endpoint)
            .field("query", &self.query)
            .field("projection", &self.projection)
            .field("limit", &self.limit)
            .finish()
    }
}

impl DisplayAs for FlightSqlExec {
    fn fmt_as(&self, _t: DisplayFormatType, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "FlightSqlExec: endpoint={}, query={:?}, projection={:?}, limit={:?}",
            self.endpoint, self.query, self.projection, self.limit
        )
    }
}

impl ExecutionPlan for FlightSqlExec {
    fn name(&self) -> &str {
        "FlightSqlExec"
    }

    fn as_any(&self) -> &dyn Any {
        self
    }

    fn properties(&self) -> &PlanProperties {
        &self.properties
    }

    fn children(&self) -> Vec<&Arc<dyn ExecutionPlan>> {
        vec![]
    }

    fn with_new_children(
        self: Arc<Self>,
        _children: Vec<Arc<dyn ExecutionPlan>>,
    ) -> DfResult<Arc<dyn ExecutionPlan>> {
        Ok(self)
    }

    fn execute(
        &self,
        _partition: usize,
        _context: Arc<TaskContext>,
    ) -> DfResult<SendableRecordBatchStream> {
        let endpoint = self.endpoint.clone();
        let query = self.query.clone();
        let projection = self.projection.clone();
        let limit = self.limit;
        let projected_schema = Arc::clone(&self.projected_schema);
        let projected_schema_for_stream = Arc::clone(&projected_schema);

        // The Flight SQL client API is `async`, but `ExecutionPlan::execute`
        // is synchronous. We materialise the connect / execute / do_get
        // dance lazily inside an async stream so DataFusion can drive it on
        // its own runtime.
        let batch_stream = stream::once(async move {
            run_remote_query(endpoint, query)
                .await
                .map_err(DataFusionError::from)
        })
        .try_flatten()
        .map(move |batch_res| {
            batch_res.and_then(|batch| project_batch(&batch, projection.as_deref()))
        });

        let limited = apply_limit(batch_stream, limit);

        Ok(Box::pin(RecordBatchStreamAdapter::new(
            projected_schema_for_stream,
            limited,
        )))
    }
}

/// Connects to a remote Flight SQL endpoint and returns a ready-to-use client.
async fn connect(endpoint: &str) -> Result<FlightSqlServiceClient<Channel>, FlightProviderError> {
    let ep = Endpoint::from_shared(endpoint.to_owned()).map_err(|source| {
        FlightProviderError::InvalidEndpoint {
            endpoint: endpoint.to_owned(),
            source,
        }
    })?;
    let channel = ep
        .connect()
        .await
        .map_err(|source| FlightProviderError::Connect {
            endpoint: endpoint.to_owned(),
            source,
        })?;
    Ok(FlightSqlServiceClient::new(channel))
}

/// Opens a fresh Flight SQL connection, executes `query` and returns a
/// `RecordBatch` stream that yields the union of every endpoint reported by
/// the remote server.
async fn run_remote_query(
    endpoint: String,
    query: String,
) -> Result<
    impl futures::Stream<Item = DfResult<RecordBatch>> + Send + 'static,
    FlightProviderError,
> {
    let mut client = connect(&endpoint).await?;
    let info = client
        .execute(query, None)
        .await
        .map_err(|e| FlightProviderError::Flight(e.to_string()))?;

    if info.endpoint.is_empty() {
        return Err(FlightProviderError::MissingTicket);
    }

    // Walk every endpoint's ticket and concatenate the resulting batch
    // streams. For most servers there will only be one endpoint.
    let mut streams = Vec::with_capacity(info.endpoint.len());
    for ep in info.endpoint {
        let ticket = ep.ticket.ok_or(FlightProviderError::MissingTicket)?;
        let stream = client
            .do_get(ticket)
            .await
            .map_err(|e| FlightProviderError::Flight(e.to_string()))?;
        streams.push(stream);
    }

    let merged = stream::iter(streams)
        .flatten()
        .map(|batch_res| batch_res.map_err(|e| DataFusionError::External(Box::new(e))));

    Ok(merged)
}

/// Apply the requested projection (a column-index list) to a batch.
fn project_batch(batch: &RecordBatch, projection: Option<&[usize]>) -> DfResult<RecordBatch> {
    match projection {
        Some(cols) => batch
            .project(cols)
            .map_err(|e| DataFusionError::ArrowError(e, None)),
        None => Ok(batch.clone()),
    }
}

/// Wrap the underlying batch stream so it stops once `limit` rows have been
/// emitted, slicing the boundary batch as needed.
fn apply_limit<S>(
    stream: S,
    limit: Option<usize>,
) -> impl futures::Stream<Item = DfResult<RecordBatch>> + Send
where
    S: futures::Stream<Item = DfResult<RecordBatch>> + Send + 'static,
{
    let enforce = limit.is_some();
    let budget = limit.unwrap_or(usize::MAX);
    futures::stream::unfold(
        (stream.boxed(), budget, enforce),
        |(mut s, mut budget, enforce)| async move {
            if enforce && budget == 0 {
                return None;
            }
            match s.next().await {
                None => None,
                Some(Err(e)) => Some((Err(e), (s, budget, enforce))),
                Some(Ok(batch)) => {
                    if !enforce {
                        return Some((Ok(batch), (s, budget, enforce)));
                    }
                    let rows = batch.num_rows();
                    if rows <= budget {
                        budget -= rows;
                        Some((Ok(batch), (s, budget, enforce)))
                    } else {
                        let sliced = batch.slice(0, budget);
                        Some((Ok(sliced), (s, 0, enforce)))
                    }
                }
            }
        },
    )
}

