//! Integration test: round-trip `SELECT 1` through the embedded Flight
//! SQL server backed by DataFusion, with `allow_anonymous = true` so no
//! JWT is required.
//!
//! Verifies the same end-to-end path that Tableau / Superset use: open a
//! Flight SQL channel, call `execute`, fetch the resulting Arrow stream,
//! and assert a single row with value `1`.

use std::time::Duration;

use arrow::array::Int64Array;
use arrow_flight::flight_service_server::FlightServiceServer;
use arrow_flight::sql::client::FlightSqlServiceClient;
use futures::TryStreamExt;
use sql_bi_gateway_service::auth::Authenticator;
use sql_bi_gateway_service::flight_sql::FlightSqlServiceImpl;
use sql_bi_gateway_service::routing::BackendRouter;
use tokio::net::TcpListener;
use tonic::transport::{Channel, Endpoint, Server};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn flight_sql_select_one_round_trip() {
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let local_addr = listener.local_addr().expect("local_addr");
    let incoming = tokio_stream::wrappers::TcpListenerStream::new(listener);

    let config = sql_bi_gateway_service::config::AppConfig {
        host: "127.0.0.1".to_string(),
        port: 0,
        healthz_port: 0,
        database_url: "postgres://test".to_string(),
        jwt_secret: "test-secret".to_string(),
        warehousing_flight_sql_url: None,
        clickhouse_flight_sql_url: None,
        vespa_flight_sql_url: None,
        postgres_flight_sql_url: None,
        allow_anonymous: true,
    };
    let router = BackendRouter::from_config(&config);
    let auth = Authenticator::new(&config.jwt_secret, config.allow_anonymous);
    let service = FlightSqlServiceImpl::new(router, auth);

    let server = tokio::spawn(async move {
        Server::builder()
            .add_service(FlightServiceServer::new(service))
            .serve_with_incoming(incoming)
            .await
            .expect("flight server");
    });

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

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn flight_sql_unconfigured_backend_is_rejected_with_clear_error() {
    // No JWT and no remote endpoints configured: a query that targets
    // `clickhouse.*` must be rejected at planning time.
    let listener = TcpListener::bind("127.0.0.1:0").await.expect("bind");
    let local_addr = listener.local_addr().expect("local_addr");
    let incoming = tokio_stream::wrappers::TcpListenerStream::new(listener);

    let config = sql_bi_gateway_service::config::AppConfig {
        host: "127.0.0.1".to_string(),
        port: 0,
        healthz_port: 0,
        database_url: "postgres://test".to_string(),
        jwt_secret: "test-secret".to_string(),
        warehousing_flight_sql_url: None,
        clickhouse_flight_sql_url: None,
        vespa_flight_sql_url: None,
        postgres_flight_sql_url: None,
        allow_anonymous: true,
    };
    let router = BackendRouter::from_config(&config);
    let auth = Authenticator::new(&config.jwt_secret, config.allow_anonymous);
    let service = FlightSqlServiceImpl::new(router, auth);

    let server = tokio::spawn(async move {
        Server::builder()
            .add_service(FlightServiceServer::new(service))
            .serve_with_incoming(incoming)
            .await
            .expect("flight server");
    });

    let endpoint =
        Endpoint::from_shared(format!("http://{local_addr}")).expect("endpoint from addr");
    let channel: Channel = {
        let mut connected = None;
        for _ in 0..50 {
            if let Ok(channel) = endpoint.connect().await {
                connected = Some(channel);
                break;
            }
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
        connected.expect("connect")
    };
    let mut client = FlightSqlServiceClient::new(channel);

    let result = client
        .execute(
            "SELECT * FROM clickhouse.events_dist".to_string(),
            None,
        )
        .await;

    let err = result.expect_err("must fail when clickhouse backend is unconfigured");
    let msg = format!("{err:?}");
    assert!(
        msg.contains("not configured") || msg.to_lowercase().contains("failedprecondition"),
        "expected `backend not configured` error, got: {msg}"
    );

    server.abort();
}
