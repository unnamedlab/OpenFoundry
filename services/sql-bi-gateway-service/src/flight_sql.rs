//! Apache Arrow Flight SQL server for the edge BI gateway.
//!
//! Implements the subset of Flight SQL verbs that JDBC/ODBC clients
//! (Tableau, Superset, the Apache Arrow Flight SQL JDBC driver) call
//! during normal operation:
//!
//! * `do_handshake` — accept the client.
//! * `get_flight_info_catalogs` / `get_flight_info_schemas` /
//!   `get_flight_info_tables` — return the catalog tree so BI clients can
//!   render a sensible navigator panel even though the data lives across
//!   multiple backends (Iceberg, ClickHouse, Vespa, Postgres).
//! * `get_flight_info_statement` — plan a SQL query and return a ticket.
//! * `do_get_statement` — stream Arrow record batches for the planned
//!   ticket.
//! * `get_flight_info_prepared_statement` — minimal prepared-statement
//!   support: the prepared handle is the SQL bytes themselves.
//! * `do_put_statement_update` — execute non-returning DDL/DML.
//! * `register_sql_info` — no-op (the trait requires it).
//!
//! Every request is **authenticated** via a JWT in the `authorization`
//! gRPC metadata header (see [`crate::auth`]), every executed statement
//! is **routed** to one of the configured backends (see
//! [`crate::routing`]), and every execution is **audited** through the
//! `sql_bi_gateway.audit` tracing target (see [`crate::audit`]).
//!
//! Cross-backend federation is delegated to DataFusion: Iceberg /
//! lakehouse statements run against the local
//! `query_engine::QueryContext` (or are forwarded to
//! `sql-warehousing-service` over Flight SQL when configured), while
//! statements that target ClickHouse / Vespa / Postgres are forwarded to
//! their respective Flight SQL endpoints.

use std::pin::Pin;
use std::sync::Arc;
use std::time::Instant;

use arrow::array::{RecordBatch, StringArray};
use arrow::datatypes::{DataType, Field, Schema};
use arrow_flight::encode::FlightDataEncoderBuilder;
use arrow_flight::error::FlightError;
use arrow_flight::flight_service_server::FlightService;
use arrow_flight::sql::client::FlightSqlServiceClient;
use arrow_flight::sql::server::{FlightSqlService, PeekableFlightDataStream};
use arrow_flight::sql::{
    CommandGetCatalogs, CommandGetDbSchemas, CommandGetTables, CommandPreparedStatementQuery,
    CommandStatementQuery, CommandStatementUpdate, ProstMessageExt, SqlInfo, TicketStatementQuery,
};
use arrow_flight::{
    FlightDescriptor, FlightEndpoint, FlightInfo, HandshakeRequest, HandshakeResponse, Ticket,
};
use bytes::Bytes;
use futures::{Stream, TryStreamExt};
use prost::Message;
use query_engine::QueryContext;
use tonic::transport::{Channel, Endpoint};
use tonic::{Request, Response, Status, Streaming};

use crate::audit::{AuditOutcome, SqlAuditEvent, sql_fingerprint};
use crate::auth::{AuthenticatedRequest, Authenticator, EnforcedQuotas};
use crate::routing::{Backend, BackendRouter, RoutingDecision, RoutingError};

/// Fixed catalog name advertised to BI clients. The schemas under it map
/// 1:1 onto [`Backend`] variants, so a connected Tableau/Superset session
/// sees exactly one catalog (`openfoundry`) and four schemas
/// (`iceberg`, `clickhouse`, `vespa`, `postgres`).
pub const GATEWAY_CATALOG: &str = "openfoundry";

/// Stream type used by Flight SQL `do_get_*` methods.
pub type DoGetStream =
    Pin<Box<dyn Stream<Item = Result<arrow_flight::FlightData, Status>> + Send + 'static>>;

/// Flight SQL server backed by DataFusion + a [`BackendRouter`].
#[derive(Clone)]
pub struct FlightSqlServiceImpl {
    ctx: Arc<QueryContext>,
    router: Arc<BackendRouter>,
    auth: Arc<Authenticator>,
}

impl FlightSqlServiceImpl {
    pub fn new(router: BackendRouter, auth: Authenticator) -> Self {
        Self {
            ctx: Arc::new(QueryContext::new()),
            router: Arc::new(router),
            auth: Arc::new(auth),
        }
    }

    pub fn with_context(
        ctx: Arc<QueryContext>,
        router: BackendRouter,
        auth: Authenticator,
    ) -> Self {
        Self {
            ctx,
            router: Arc::new(router),
            auth: Arc::new(auth),
        }
    }

    /// Borrow the underlying query context (useful for registering tables in tests).
    pub fn context(&self) -> &Arc<QueryContext> {
        &self.ctx
    }

    /// Borrow the configured router (useful for tests and metrics).
    pub fn router(&self) -> &Arc<BackendRouter> {
        &self.router
    }

    /// Authenticate a request and resolve the effective quotas, falling
    /// back to the anonymous defaults when `allow_anonymous` is enabled.
    fn authenticate(
        &self,
        metadata: &tonic::metadata::MetadataMap,
    ) -> Result<(Option<AuthenticatedRequest>, EnforcedQuotas), Status> {
        match self.auth.authenticate(metadata)? {
            Some(auth_req) => {
                let quotas = EnforcedQuotas::for_tenant(&auth_req.tenant);
                Ok((Some(auth_req), quotas))
            }
            None => Ok((None, EnforcedQuotas::anonymous_default())),
        }
    }

    async fn execute(
        &self,
        sql: &str,
        auth: Option<&AuthenticatedRequest>,
        quotas: EnforcedQuotas,
    ) -> Result<Response<DoGetStream>, Status> {
        let started = Instant::now();
        let decision = self.router.route(sql).map_err(routing_status)?;

        let (batches, schema, outcome): (Vec<RecordBatch>, _, AuditOutcome) =
            match self.collect_batches(sql, &decision, quotas).await {
                Ok((batches, schema)) => (batches, schema, AuditOutcome::Ok),
                Err(status) => {
                    self.audit(sql, auth, &decision, 0, started.elapsed(), AuditOutcome::Error);
                    return Err(status);
                }
            };

        let row_count: usize = batches.iter().map(|b| b.num_rows()).sum();
        self.audit(sql, auth, &decision, row_count, started.elapsed(), outcome);

        let batch_stream = futures::stream::iter(batches.into_iter().map(Ok::<_, FlightError>));
        let flight_data_stream = FlightDataEncoderBuilder::new()
            .with_schema(schema)
            .build(batch_stream)
            .map_err(|err| Status::internal(format!("Flight encoding failed: {err}")));

        Ok(Response::new(Box::pin(flight_data_stream)))
    }

    async fn collect_batches(
        &self,
        sql: &str,
        decision: &RoutingDecision,
        quotas: EnforcedQuotas,
    ) -> Result<(Vec<RecordBatch>, arrow::datatypes::SchemaRef), Status> {
        let batches = match decision.remote_endpoint.as_deref() {
            None => self
                .ctx
                .execute_sql(sql)
                .await
                .map_err(|err| Status::internal(format!("DataFusion execution failed: {err}")))?,
            Some(endpoint) => delegate_to_remote(endpoint, sql).await?,
        };

        let schema = batches
            .first()
            .map(|batch| batch.schema())
            .unwrap_or_else(|| Arc::new(Schema::empty()));

        // Apply the tenant row-quota by truncating the result set. DataFusion
        // does not expose a generic `LIMIT` rewrite that is safe across all
        // dialects, so we cap on the way out — which also caps remote results.
        let mut remaining = quotas.max_rows;
        let mut clamped = Vec::with_capacity(batches.len());
        for batch in batches {
            if remaining == 0 {
                break;
            }
            if batch.num_rows() <= remaining {
                remaining -= batch.num_rows();
                clamped.push(batch);
            } else {
                clamped.push(batch.slice(0, remaining));
                remaining = 0;
            }
        }

        Ok((clamped, schema))
    }

    fn audit(
        &self,
        sql: &str,
        auth: Option<&AuthenticatedRequest>,
        decision: &RoutingDecision,
        row_count: usize,
        duration: std::time::Duration,
        outcome: AuditOutcome,
    ) {
        let fingerprint = sql_fingerprint(sql);
        let (tenant_id, tenant_tier, user_email) = match auth {
            Some(req) => (
                Some(req.tenant.scope_id.as_str()),
                req.tenant.tier.as_str(),
                Some(req.claims.email.as_str()),
            ),
            None => (None, "anonymous", None),
        };
        SqlAuditEvent {
            tenant_id,
            tenant_tier,
            user_email,
            backend: decision.backend,
            remote: decision.remote_endpoint.is_some(),
            sql_hash: &fingerprint,
            row_count,
            duration,
            outcome,
        }
        .emit();
    }
}

fn routing_status(error: RoutingError) -> Status {
    match error {
        RoutingError::BackendUnavailable(_) => Status::failed_precondition(error.to_string()),
    }
}

/// Delegate a SQL statement to a remote Flight SQL endpoint and collect
/// every Arrow record batch returned by it. Used to fan out queries to
/// `sql-warehousing-service` (Iceberg shared compute pool) and to the
/// ClickHouse / Vespa / Postgres Flight SQL fronts.
async fn delegate_to_remote(endpoint: &str, sql: &str) -> Result<Vec<RecordBatch>, Status> {
    let channel: Channel = Endpoint::from_shared(endpoint.to_string())
        .map_err(|err| Status::internal(format!("invalid backend endpoint `{endpoint}`: {err}")))?
        .connect()
        .await
        .map_err(|err| Status::unavailable(format!("backend `{endpoint}` unreachable: {err}")))?;
    let mut client = FlightSqlServiceClient::new(channel);
    let info = client
        .execute(sql.to_string(), None)
        .await
        .map_err(|err| Status::internal(format!("remote execute failed: {err}")))?;

    let mut batches = Vec::new();
    for endpoint_info in info.endpoint.iter() {
        let ticket = endpoint_info
            .ticket
            .as_ref()
            .ok_or_else(|| Status::internal("remote FlightInfo missing ticket"))?
            .clone();
        let mut stream = client
            .do_get(ticket)
            .await
            .map_err(|err| Status::internal(format!("remote do_get failed: {err}")))?;
        while let Some(batch) = stream
            .try_next()
            .await
            .map_err(|err| Status::internal(format!("remote stream decode failed: {err}")))?
        {
            batches.push(batch);
        }
    }
    Ok(batches)
}

fn build_catalog_batch() -> Result<(RecordBatch, arrow::datatypes::SchemaRef), Status> {
    let schema = Arc::new(Schema::new(vec![Field::new(
        "catalog_name",
        DataType::Utf8,
        false,
    )]));
    let array = StringArray::from(vec![GATEWAY_CATALOG]);
    let batch = RecordBatch::try_new(schema.clone(), vec![Arc::new(array)])
        .map_err(|err| Status::internal(format!("build catalog batch failed: {err}")))?;
    Ok((batch, schema))
}

fn build_schemas_batch() -> Result<(RecordBatch, arrow::datatypes::SchemaRef), Status> {
    let schema = Arc::new(Schema::new(vec![
        Field::new("catalog_name", DataType::Utf8, false),
        Field::new("db_schema_name", DataType::Utf8, false),
    ]));
    let backends = Backend::all();
    let catalogs = StringArray::from(vec![GATEWAY_CATALOG; backends.len()]);
    let schemas = StringArray::from(backends.iter().map(|b| b.as_str()).collect::<Vec<_>>());
    let batch = RecordBatch::try_new(
        schema.clone(),
        vec![Arc::new(catalogs), Arc::new(schemas)],
    )
    .map_err(|err| Status::internal(format!("build schemas batch failed: {err}")))?;
    Ok((batch, schema))
}

fn build_tables_batch() -> Result<(RecordBatch, arrow::datatypes::SchemaRef), Status> {
    // The `GetTables` Flight SQL response is intentionally minimal here:
    // BI clients show one row per known schema/backend with a placeholder
    // table name `_meta` so the navigator tree is non-empty even before
    // any catalog metadata is wired in. Real tables are still discoverable
    // via standard `SHOW TABLES` and DataFusion's `information_schema`.
    let schema = Arc::new(Schema::new(vec![
        Field::new("catalog_name", DataType::Utf8, false),
        Field::new("db_schema_name", DataType::Utf8, false),
        Field::new("table_name", DataType::Utf8, false),
        Field::new("table_type", DataType::Utf8, false),
    ]));
    let backends = Backend::all();
    let catalogs = StringArray::from(vec![GATEWAY_CATALOG; backends.len()]);
    let schemas = StringArray::from(backends.iter().map(|b| b.as_str()).collect::<Vec<_>>());
    let names = StringArray::from(vec!["_meta"; backends.len()]);
    let kinds = StringArray::from(vec!["TABLE"; backends.len()]);
    let batch = RecordBatch::try_new(
        schema.clone(),
        vec![
            Arc::new(catalogs),
            Arc::new(schemas),
            Arc::new(names),
            Arc::new(kinds),
        ],
    )
    .map_err(|err| Status::internal(format!("build tables batch failed: {err}")))?;
    Ok((batch, schema))
}

fn batches_to_response(
    batch: RecordBatch,
    schema: arrow::datatypes::SchemaRef,
) -> Response<DoGetStream> {
    let stream = futures::stream::iter(vec![Ok::<_, FlightError>(batch)]);
    let flight_data_stream = FlightDataEncoderBuilder::new()
        .with_schema(schema)
        .build(stream)
        .map_err(|err| Status::internal(format!("Flight encoding failed: {err}")));
    Response::new(Box::pin(flight_data_stream))
}

#[tonic::async_trait]
impl FlightSqlService for FlightSqlServiceImpl {
    type FlightService = FlightSqlServiceImpl;

    async fn do_handshake(
        &self,
        _request: Request<Streaming<HandshakeRequest>>,
    ) -> Result<
        Response<Pin<Box<dyn Stream<Item = Result<HandshakeResponse, Status>> + Send>>>,
        Status,
    > {
        let response = HandshakeResponse {
            protocol_version: 0,
            payload: Bytes::new(),
        };
        let stream = futures::stream::once(async move { Ok(response) });
        Ok(Response::new(Box::pin(stream)))
    }

    async fn get_flight_info_catalogs(
        &self,
        _query: CommandGetCatalogs,
        request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        self.authenticate(request.metadata())?;
        let descriptor = request.into_inner();
        let ticket_payload = TicketStatementQuery {
            statement_handle: Bytes::from_static(b"__catalogs__"),
        };
        let ticket = Ticket::new(ticket_payload.as_any().encode_to_vec());
        let endpoint = FlightEndpoint::new().with_ticket(ticket);
        let info = FlightInfo::new()
            .with_descriptor(descriptor)
            .with_endpoint(endpoint)
            .with_total_records(1)
            .with_total_bytes(-1);
        Ok(Response::new(info))
    }

    async fn get_flight_info_schemas(
        &self,
        _query: CommandGetDbSchemas,
        request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        self.authenticate(request.metadata())?;
        let descriptor = request.into_inner();
        let ticket_payload = TicketStatementQuery {
            statement_handle: Bytes::from_static(b"__schemas__"),
        };
        let ticket = Ticket::new(ticket_payload.as_any().encode_to_vec());
        let endpoint = FlightEndpoint::new().with_ticket(ticket);
        let info = FlightInfo::new()
            .with_descriptor(descriptor)
            .with_endpoint(endpoint)
            .with_total_records(Backend::all().len() as i64)
            .with_total_bytes(-1);
        Ok(Response::new(info))
    }

    async fn get_flight_info_tables(
        &self,
        _query: CommandGetTables,
        request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        self.authenticate(request.metadata())?;
        let descriptor = request.into_inner();
        let ticket_payload = TicketStatementQuery {
            statement_handle: Bytes::from_static(b"__tables__"),
        };
        let ticket = Ticket::new(ticket_payload.as_any().encode_to_vec());
        let endpoint = FlightEndpoint::new().with_ticket(ticket);
        let info = FlightInfo::new()
            .with_descriptor(descriptor)
            .with_endpoint(endpoint)
            .with_total_records(Backend::all().len() as i64)
            .with_total_bytes(-1);
        Ok(Response::new(info))
    }

    async fn get_flight_info_statement(
        &self,
        query: CommandStatementQuery,
        request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        self.authenticate(request.metadata())?;
        // Validate the routing decision **before** returning a ticket so
        // the BI client gets a clear error at planning time rather than at
        // streaming time.
        self.router.route(&query.query).map_err(routing_status)?;

        let descriptor = request.into_inner();
        let ticket_payload = TicketStatementQuery {
            statement_handle: Bytes::from(query.query.into_bytes()),
        };
        let ticket = Ticket::new(ticket_payload.as_any().encode_to_vec());
        let endpoint = FlightEndpoint::new().with_ticket(ticket);
        let info = FlightInfo::new()
            .with_descriptor(descriptor)
            .with_endpoint(endpoint)
            .with_total_records(-1)
            .with_total_bytes(-1);
        Ok(Response::new(info))
    }

    async fn get_flight_info_prepared_statement(
        &self,
        query: CommandPreparedStatementQuery,
        request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        let sql = String::from_utf8(query.prepared_statement_handle.to_vec()).map_err(|err| {
            Status::invalid_argument(format!("prepared_statement_handle is not utf-8: {err}"))
        })?;
        let cmd = CommandStatementQuery {
            query: sql,
            transaction_id: None,
        };
        self.get_flight_info_statement(cmd, request).await
    }

    async fn do_get_statement(
        &self,
        ticket: TicketStatementQuery,
        request: Request<Ticket>,
    ) -> Result<Response<<Self as FlightService>::DoGetStream>, Status> {
        let (auth, quotas) = self.authenticate(request.metadata())?;

        // Catalog-tree probes use sentinel handles so we can serve them
        // synchronously without going through the router.
        let handle = ticket.statement_handle.as_ref();
        if handle == b"__catalogs__" {
            let (batch, schema) = build_catalog_batch()?;
            return Ok(batches_to_response(batch, schema));
        }
        if handle == b"__schemas__" {
            let (batch, schema) = build_schemas_batch()?;
            return Ok(batches_to_response(batch, schema));
        }
        if handle == b"__tables__" {
            let (batch, schema) = build_tables_batch()?;
            return Ok(batches_to_response(batch, schema));
        }

        let sql = std::str::from_utf8(handle).map_err(|err| {
            Status::invalid_argument(format!("statement_handle is not utf-8: {err}"))
        })?;
        self.execute(sql, auth.as_ref(), quotas).await
    }

    async fn do_put_statement_update(
        &self,
        ticket: CommandStatementUpdate,
        request: Request<PeekableFlightDataStream>,
    ) -> Result<i64, Status> {
        let (_auth, _quotas) = self.authenticate(request.metadata())?;
        let decision = self.router.route(&ticket.query).map_err(routing_status)?;
        match decision.remote_endpoint.as_deref() {
            None => {
                // DataFusion does not surface a meaningful affected-row count
                // for arbitrary DDL/DML; we run the statement to completion
                // and report 0 (treated by Flight SQL clients as "unknown").
                let _ = self.ctx.execute_sql(&ticket.query).await.map_err(|err| {
                    Status::internal(format!("DataFusion execution failed: {err}"))
                })?;
                Ok(0)
            }
            Some(endpoint) => {
                // Forward the DDL/DML to the owning backend; we stream the
                // result so any rows produced by the remote are exhausted
                // and only then we acknowledge the update.
                let _ = delegate_to_remote(endpoint, &ticket.query).await?;
                Ok(0)
            }
        }
    }

    async fn register_sql_info(&self, _id: i32, _result: &SqlInfo) {}
}
