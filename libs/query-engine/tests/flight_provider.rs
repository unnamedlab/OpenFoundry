//! End-to-end test for [`query_engine::flight_provider::FlightSqlTableProvider`].
//!
//! Spins up an in-process tonic Flight SQL server (bound to an ephemeral
//! local TCP port), points the provider at it and runs `SELECT * FROM t`
//! through a DataFusion `SessionContext`.

#![cfg(feature = "flight-client")]

use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;

use arrow::array::{ArrayRef, Int32Array, RecordBatch};
use arrow::datatypes::{DataType, Field, Schema};
use arrow_flight::flight_service_server::{FlightService, FlightServiceServer};
use arrow_flight::sql::server::FlightSqlService;
use arrow_flight::sql::{
    CommandStatementQuery, ProstMessageExt, SqlInfo, TicketStatementQuery,
};
use arrow_flight::utils::batches_to_flight_data;
use arrow_flight::{FlightData, FlightDescriptor, FlightEndpoint, FlightInfo, Ticket};
use datafusion::prelude::SessionContext;
use futures::{stream, Stream};
use prost::bytes::Bytes;
use prost::Message;
use query_engine::flight_provider::FlightSqlTableProvider;
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::Server;
use tonic::{Request, Response, Status};

/// Schema returned by the in-process server.
fn test_schema() -> Arc<Schema> {
    Arc::new(Schema::new(vec![Field::new("id", DataType::Int32, false)]))
}

/// The single `RecordBatch` the test server returns.
fn test_batch() -> RecordBatch {
    let id: ArrayRef = Arc::new(Int32Array::from(vec![1, 2, 3]));
    RecordBatch::try_new(test_schema(), vec![id]).expect("valid batch")
}

/// Minimal `FlightSqlService` that ignores the SQL string and always returns
/// `test_batch()`. Implements only the two RPCs the provider needs:
/// `get_flight_info_statement` (planning) and `do_get_statement` (fetch).
#[derive(Clone, Default)]
struct TestFlightSqlService;

#[tonic::async_trait]
impl FlightSqlService for TestFlightSqlService {
    type FlightService = TestFlightSqlService;

    async fn get_flight_info_statement(
        &self,
        _query: CommandStatementQuery,
        _request: Request<FlightDescriptor>,
    ) -> Result<Response<FlightInfo>, Status> {
        let schema = test_schema();
        // The ticket payload must be a protobuf `Any` wrapping a Flight SQL
        // command so the generated `do_get` dispatcher routes it to
        // `do_get_statement` below.
        let ticket_cmd = TicketStatementQuery {
            statement_handle: Bytes::from_static(b"handle"),
        };
        let ticket = Ticket {
            ticket: ticket_cmd.as_any().encode_to_vec().into(),
        };
        let endpoint = FlightEndpoint::new().with_ticket(ticket);
        let info = FlightInfo::new()
            .try_with_schema(schema.as_ref())
            .map_err(|e| Status::internal(format!("encode schema: {e}")))?
            .with_endpoint(endpoint)
            .with_descriptor(FlightDescriptor::new_cmd(Vec::<u8>::new()));
        Ok(Response::new(info))
    }

    async fn do_get_statement(
        &self,
        _ticket: TicketStatementQuery,
        _request: Request<Ticket>,
    ) -> Result<Response<<Self as FlightService>::DoGetStream>, Status> {
        let batch = test_batch();
        let schema = batch.schema();
        let flight_data = batches_to_flight_data(schema.as_ref(), vec![batch])
            .map_err(|e| Status::internal(format!("encode batch: {e}")))?
            .into_iter()
            .map(Ok::<FlightData, Status>);
        let stream: Pin<Box<dyn Stream<Item = Result<FlightData, Status>> + Send>> =
            Box::pin(stream::iter(flight_data));
        Ok(Response::new(stream))
    }

    async fn register_sql_info(&self, _id: i32, _result: &SqlInfo) {}
}

/// Spin the server, return its endpoint URI plus a shutdown signal.
async fn spawn_server() -> (String, tokio::sync::oneshot::Sender<()>) {
    let listener = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("bind ephemeral port");
    let addr = listener.local_addr().expect("local_addr");
    let endpoint = format!("http://{addr}");

    let (tx, rx) = tokio::sync::oneshot::channel::<()>();
    let svc = FlightServiceServer::new(TestFlightSqlService);
    let incoming = TcpListenerStream::new(listener);

    tokio::spawn(async move {
        let _ = Server::builder()
            .add_service(svc)
            .serve_with_incoming_shutdown(incoming, async {
                let _ = rx.await;
            })
            .await;
    });

    // Tiny wait so the server is accepting before the client connects.
    tokio::time::sleep(Duration::from_millis(50)).await;
    (endpoint, tx)
}

#[tokio::test]
async fn select_star_through_flight_provider() {
    let (endpoint, _shutdown) = spawn_server().await;

    let provider =
        FlightSqlTableProvider::new(endpoint, "SELECT id FROM t", test_schema());
    let ctx = SessionContext::new();
    ctx.register_table("t", Arc::new(provider))
        .expect("register_table");

    let batches = ctx
        .sql("SELECT * FROM t")
        .await
        .expect("plan SQL")
        .collect()
        .await
        .expect("collect");

    let total_rows: usize = batches.iter().map(|b| b.num_rows()).sum();
    assert_eq!(total_rows, 3, "expected 3 rows from federated query");

    let first = &batches[0];
    let col = first
        .column(0)
        .as_any()
        .downcast_ref::<Int32Array>()
        .expect("Int32 column");
    let values: Vec<i32> = (0..col.len()).map(|i| col.value(i)).collect();
    assert_eq!(values, vec![1, 2, 3]);
}

#[tokio::test]
async fn limit_pushdown_truncates_results() {
    let (endpoint, _shutdown) = spawn_server().await;

    let provider =
        FlightSqlTableProvider::new(endpoint, "SELECT id FROM t", test_schema());
    let ctx = SessionContext::new();
    ctx.register_table("t", Arc::new(provider))
        .expect("register_table");

    let batches = ctx
        .sql("SELECT * FROM t LIMIT 2")
        .await
        .expect("plan SQL")
        .collect()
        .await
        .expect("collect");

    let total_rows: usize = batches.iter().map(|b| b.num_rows()).sum();
    assert_eq!(total_rows, 2, "limit should be pushed down to 2 rows");
}
