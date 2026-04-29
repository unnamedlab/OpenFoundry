//! Integration test: round-trip `SELECT 1` through the embedded Arrow
//! Flight SQL server backed by DataFusion.
//!
//! Spawns the server on an ephemeral local port, connects with
//! `FlightSqlServiceClient`, retrieves a `FlightInfo` via
//! `get_flight_info_statement`, fetches the resulting Arrow stream via
//! `do_get`, and asserts the returned record batch matches `SELECT 1`.

use std::time::Duration;

use arrow::array::Int64Array;
use arrow_flight::flight_service_server::FlightServiceServer;
use arrow_flight::sql::client::FlightSqlServiceClient;
use futures::TryStreamExt;
use sql_warehousing_service::flight_sql::FlightSqlServiceImpl;
use tokio::net::TcpListener;
use tonic::transport::{Channel, Endpoint, Server};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn flight_sql_select_one_round_trip() {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let local_addr = listener.local_addr().expect("local_addr");
    let incoming = tokio_stream::wrappers::TcpListenerStream::new(listener);

    let service = FlightSqlServiceImpl::new();

    let server = tokio::spawn(async move {
        Server::builder()
            .add_service(FlightServiceServer::new(service))
            .serve_with_incoming(incoming)
            .await
            .expect("flight server");
    });

    // Wait for the server to be ready by retrying the connection a few times,
    // rather than relying on a fixed sleep that can be flaky on slow CI.
    let endpoint =
        Endpoint::from_shared(format!("http://{local_addr}")).expect("endpoint from addr");
    let channel: Channel = {
        let mut last_err = None;
        let mut connected = None;
        for _ in 0..50 {
            match endpoint.connect().await {
                Ok(channel) => {
                    connected = Some(channel);
                    break;
                }
                Err(err) => {
                    last_err = Some(err);
                    tokio::time::sleep(Duration::from_millis(50)).await;
                }
            }
        }
        connected.unwrap_or_else(|| {
            panic!(
                "failed to connect to in-process Flight SQL server: {:?}",
                last_err
            )
        })
    };
    let mut client = FlightSqlServiceClient::new(channel);

    let info = client
        .execute("SELECT 1".to_string(), None)
        .await
        .expect("execute SELECT 1");

    assert!(
        !info.endpoint.is_empty(),
        "FlightInfo must contain at least one endpoint"
    );

    let mut total_rows = 0usize;
    let mut saw_value_one = false;

    for endpoint in info.endpoint.iter() {
        let ticket = endpoint
            .ticket
            .as_ref()
            .expect("endpoint must carry a ticket")
            .clone();
        let mut batches = client.do_get(ticket).await.expect("do_get");

        while let Some(batch) = batches.try_next().await.expect("decode batch") {
            total_rows += batch.num_rows();
            if let Some(column) = batch.columns().first() {
                if let Some(int_array) = column.as_any().downcast_ref::<Int64Array>() {
                    if int_array.iter().flatten().any(|v| v == 1) {
                        saw_value_one = true;
                    }
                }
            }
        }
    }

    assert_eq!(total_rows, 1, "SELECT 1 must produce exactly one row");
    assert!(saw_value_one, "the single row must contain the value 1");

    server.abort();
}
