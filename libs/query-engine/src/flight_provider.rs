//! DataFusion [`TableProvider`] backed by a remote Apache Arrow Flight SQL
//! endpoint.
//!
//! This module lets any data-plane service federate a SQL query against
//! another Flight SQL service as if its result-set were a regular DataFusion
//! table:
//!
//! ```no_run
//! # use std::sync::Arc;
//! # use arrow::datatypes::{DataType, Field, Schema};
//! # use datafusion::prelude::SessionContext;
//! # use query_engine::flight_provider::FlightSqlTableProvider;
//! # async fn run() -> datafusion::error::Result<()> {
//! let schema = Arc::new(Schema::new(vec![Field::new("id", DataType::Int64, false)]));
//! let provider = FlightSqlTableProvider::new(
//!     "http://127.0.0.1:50051",
//!     "SELECT id FROM remote_table",
//!     schema,
//! );
//! let ctx = SessionContext::new();
//! ctx.register_table("t", Arc::new(provider))?;
//! let _ = ctx.sql("SELECT * FROM t").await?.collect().await?;
//! # Ok(()) }
//! ```
//!
//! ### Push-down
//!
//! * `projection` and `limit` are pushed down trivially: `projection` is
//!   forwarded to the in-memory execution plan, and `limit` truncates the
//!   collected batches before they are wrapped.
//! * `filter` push-down is **out of scope** for this provider. Filters are
//!   left to be evaluated locally by DataFusion on the materialised batches.
//!   The remote query string itself is the authoritative way to push
//!   predicates today.

use std::any::Any;
use std::sync::Arc;

use arrow::array::RecordBatch;
use arrow::datatypes::SchemaRef;
use arrow_flight::sql::client::FlightSqlServiceClient;
use async_trait::async_trait;
use datafusion::catalog::Session;
use datafusion::datasource::{TableProvider, TableType};
use datafusion::error::{DataFusionError, Result as DfResult};
use datafusion::logical_expr::{Expr, TableProviderFilterPushDown};
use datafusion::physical_plan::ExecutionPlan;
use datafusion::physical_plan::memory::MemoryExec;
use futures::TryStreamExt;
use tonic::transport::Endpoint;
use tracing::debug;

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
    /// Create a new provider.
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

    /// The remote endpoint URI this provider talks to.
    pub fn endpoint(&self) -> &str {
        &self.endpoint
    }

    /// The SQL query that will be executed against the remote service.
    pub fn query(&self) -> &str {
        &self.query
    }

    /// Open a connection, execute the query and collect every record batch
    /// returned by the remote service across all Flight endpoints.
    async fn fetch_batches(&self) -> DfResult<Vec<RecordBatch>> {
        let endpoint = Endpoint::from_shared(self.endpoint.clone())
            .map_err(|e| DataFusionError::External(Box::new(e)))?;
        let channel = endpoint
            .connect()
            .await
            .map_err(|e| DataFusionError::External(Box::new(e)))?;

        let mut client = FlightSqlServiceClient::new(channel);

        debug!(endpoint = %self.endpoint, query = %self.query, "executing Flight SQL query");
        let info = client
            .execute(self.query.clone(), None)
            .await
            .map_err(|e| DataFusionError::External(Box::new(e)))?;

        let mut batches: Vec<RecordBatch> = Vec::new();
        for ep in info.endpoint {
            let ticket = ep.ticket.ok_or_else(|| {
                DataFusionError::Plan(
                    "Flight SQL endpoint did not include a ticket".to_string(),
                )
            })?;
            let stream = client
                .do_get(ticket)
                .await
                .map_err(|e| DataFusionError::External(Box::new(e)))?;
            let mut collected: Vec<RecordBatch> = stream
                .try_collect()
                .await
                .map_err(|e| DataFusionError::External(Box::new(e)))?;
            batches.append(&mut collected);
        }
        Ok(batches)
    }
}

#[async_trait]
impl TableProvider for FlightSqlTableProvider {
    fn as_any(&self) -> &dyn Any {
        self
    }

    fn schema(&self) -> SchemaRef {
        self.schema.clone()
    }

    fn table_type(&self) -> TableType {
        TableType::Base
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

    async fn scan(
        &self,
        _state: &dyn Session,
        projection: Option<&Vec<usize>>,
        _filters: &[Expr],
        limit: Option<usize>,
    ) -> DfResult<Arc<dyn ExecutionPlan>> {
        let mut batches = self.fetch_batches().await?;

        // Trivial limit push-down: slice the collected batches.
        if let Some(limit) = limit {
            let mut remaining = limit;
            let mut limited: Vec<RecordBatch> = Vec::with_capacity(batches.len());
            for batch in batches.into_iter() {
                if remaining == 0 {
                    break;
                }
                if batch.num_rows() <= remaining {
                    remaining -= batch.num_rows();
                    limited.push(batch);
                } else {
                    limited.push(batch.slice(0, remaining));
                    remaining = 0;
                }
            }
            batches = limited;
        }

        // Trivial projection push-down: handed off to MemoryExec, which only
        // emits the selected columns from each batch.
        let exec = MemoryExec::try_new(&[batches], self.schema.clone(), projection.cloned())?;
        Ok(Arc::new(exec))
    }
}
