//! Apache Arrow Flight SQL server implementation backed by the workspace
//! [`query_engine::QueryContext`] (which wraps DataFusion's `SessionContext`).
//!
//! This module purposefully implements only the small subset of Flight SQL
//! verbs requested by the migration: `do_handshake`,
//! `get_flight_info_statement`, `do_get_statement`,
//! `do_put_statement_update` and `get_flight_info_prepared_statement`.
//! Everything else falls back to the default `Status::unimplemented`
//! responses provided by the `arrow-flight` trait.

use std::pin::Pin;
use std::sync::Arc;

use arrow_flight::encode::FlightDataEncoderBuilder;
use arrow_flight::error::FlightError;
use arrow_flight::flight_service_server::FlightService;
use arrow_flight::sql::server::{FlightSqlService, PeekableFlightDataStream};
use arrow_flight::sql::{
    CommandPreparedStatementQuery, CommandStatementQuery, CommandStatementUpdate, ProstMessageExt,
    SqlInfo, TicketStatementQuery,
};
use arrow_flight::{
    FlightDescriptor, FlightEndpoint, FlightInfo, HandshakeRequest, HandshakeResponse, Ticket,
};
use bytes::Bytes;
use futures::{Stream, TryStreamExt};
use prost::Message;
use query_engine::QueryContext;
use tonic::{Request, Response, Status, Streaming};

/// Stream type used by the Flight SQL `do_get_*` methods.
pub type DoGetStream =
    Pin<Box<dyn Stream<Item = Result<arrow_flight::FlightData, Status>> + Send + 'static>>;

/// Flight SQL server backed by an embedded DataFusion `SessionContext`.
#[derive(Clone)]
pub struct FlightSqlServiceImpl {
    ctx: Arc<QueryContext>,
}

impl FlightSqlServiceImpl {
    /// Build a new server with a fresh DataFusion session.
    pub fn new() -> Self {
        Self {
            ctx: Arc::new(QueryContext::new()),
        }
    }

    /// Build a new server reusing an existing [`QueryContext`].
    pub fn with_context(ctx: Arc<QueryContext>) -> Self {
        Self { ctx }
    }

    /// Borrow the underlying query context (useful for registering tables in tests).
    pub fn context(&self) -> &Arc<QueryContext> {
        &self.ctx
    }

    async fn execute_to_stream(&self, sql: &str) -> Result<Response<DoGetStream>, Status> {
        let batches = self
            .ctx
            .execute_sql(sql)
            .await
            .map_err(|err| Status::internal(format!("DataFusion execution failed: {err}")))?;

        let schema = batches
            .first()
            .map(|batch| batch.schema())
            .unwrap_or_else(|| Arc::new(arrow::datatypes::Schema::empty()));

        let batch_stream = futures::stream::iter(
            batches
                .into_iter()
                .map(Ok::<_, FlightError>)
                .collect::<Vec<_>>(),
        );

        let flight_data_stream = FlightDataEncoderBuilder::new()
            .with_schema(schema)
            .build(batch_stream)
            .map_err(|err| Status::internal(format!("Flight encoding failed: {err}")));

        Ok(Response::new(Box::pin(flight_data_stream)))
    }
}

impl Default for FlightSqlServiceImpl {
    fn default() -> Self {
        Self::new()
    }
}

#[tonic::async_trait]
impl FlightSqlService for FlightSqlServiceImpl {
    type FlightService = FlightSqlServiceImpl;

    /// Minimal handshake: accept any client and return an empty payload.
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

    /// Return a `FlightInfo` whose single endpoint contains a ticket
    /// referencing the original SQL text.
    async fn get_flight_info_statement(
        &self,
        query: CommandStatementQuery,
        request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        let flight_descriptor = request.into_inner();

        let ticket_payload = TicketStatementQuery {
            statement_handle: Bytes::from(query.query.into_bytes()),
        };
        let ticket = Ticket::new(ticket_payload.as_any().encode_to_vec());
        let endpoint = FlightEndpoint::new().with_ticket(ticket);

        let info = FlightInfo::new()
            .with_descriptor(flight_descriptor)
            .with_endpoint(endpoint)
            .with_total_records(-1)
            .with_total_bytes(-1);

        Ok(Response::new(info))
    }

    /// Treat a prepared statement handle as the raw SQL bytes.
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

    /// Execute the SQL recovered from the ticket and stream Arrow record batches back.
    async fn do_get_statement(
        &self,
        ticket: TicketStatementQuery,
        _request: Request<Ticket>,
    ) -> Result<Response<<Self as FlightService>::DoGetStream>, Status> {
        let sql = std::str::from_utf8(&ticket.statement_handle)
            .map_err(|err| Status::invalid_argument(format!("statement_handle is not utf-8: {err}")))?;
        self.execute_to_stream(sql).await
    }

    /// Execute a non-returning SQL statement (DDL/DML) and report rows affected.
    async fn do_put_statement_update(
        &self,
        ticket: CommandStatementUpdate,
        _request: Request<PeekableFlightDataStream>,
    ) -> Result<i64, Status> {
        // DataFusion does not currently report a row count for arbitrary
        // updates, so we run the statement to completion and report 0.
        let _ = self
            .ctx
            .execute_sql(&ticket.query)
            .await
            .map_err(|err| Status::internal(format!("DataFusion execution failed: {err}")))?;
        Ok(0)
    }

    async fn register_sql_info(&self, _id: i32, _result: &SqlInfo) {}
}
