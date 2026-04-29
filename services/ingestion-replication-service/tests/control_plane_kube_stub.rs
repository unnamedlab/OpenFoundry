//! Integration-style test that exercises [`apply_resources`] against a
//! `kube::Client` constructed from a `tower_test::mock` stub. This validates
//! that the control plane sends the right server-side-apply requests for the
//! Strimzi `KafkaConnector` and Flink `FlinkDeployment` CRDs without
//! requiring a real Kubernetes API server.

use http::{Request, Response};
use http_body_util::BodyExt;
use kube::client::Body;
use kube::Client;
use tower_test::mock;

use ingestion_replication_service::control_plane::{apply_resources, render_resources};
use ingestion_replication_service::proto::{IcebergSink, IngestJobSpec, PostgresSource};

fn sample_spec() -> IngestJobSpec {
    IngestJobSpec {
        name: "users".into(),
        namespace: "ingest".into(),
        source: "postgres".into(),
        kafka_connect_cluster: "main-connect".into(),
        postgres: Some(PostgresSource {
            hostname: "pg.local".into(),
            port: 5432,
            database: "app".into(),
            user: "debezium".into(),
            password_secret: "pg-secret".into(),
            slot_name: "users_slot".into(),
            publication_name: "users_pub".into(),
            tables: vec!["public.users".into()],
            topic_prefix: "users".into(),
        }),
        iceberg_sink: Some(IcebergSink {
            warehouse: "s3://lake/warehouse".into(),
            catalog_name: "lake".into(),
            database: "ops".into(),
            table: "users".into(),
            flink_image: String::new(),
            flink_version: String::new(),
        }),
    }
}

#[tokio::test]
async fn apply_resources_sends_ssa_patches_for_both_crds() {
    let (mock_service, mut handle) =
        mock::pair::<Request<Body>, Response<Body>>();
    // The mock service has a finite buffer; we discard the readiness checks.
    let client = Client::new(mock_service, "default");

    let rendered = render_resources(&sample_spec()).expect("render");

    // Drive the kube client and the mock side concurrently.
    let apply_task = tokio::spawn({
        let client = client.clone();
        async move { apply_resources(&client, &rendered).await }
    });

    // First request: KafkaConnector PATCH (server-side apply).
    let (kc_req, kc_resp) = handle
        .next_request()
        .await
        .expect("first request should be sent");
    assert_eq!(kc_req.method().as_str(), "PATCH");
    let kc_uri = kc_req.uri().to_string();
    assert!(
        kc_uri.contains("/apis/kafka.strimzi.io/v1beta2/namespaces/ingest/kafkaconnectors/users-debezium-pg"),
        "unexpected kafka connector uri: {kc_uri}",
    );
    assert!(
        kc_uri.contains("fieldManager=ingestion-replication-service"),
        "missing field manager in {kc_uri}",
    );
    let kc_ct = kc_req
        .headers()
        .get("content-type")
        .map(|v| v.to_str().unwrap().to_string())
        .unwrap_or_default();
    assert_eq!(kc_ct, "application/apply-patch+yaml");

    let body_bytes = kc_req
        .into_body()
        .collect()
        .await
        .expect("collect body")
        .to_bytes();
    let body_value: serde_json::Value =
        serde_json::from_slice(&body_bytes).expect("body is JSON");
    assert_eq!(body_value["kind"], "KafkaConnector");
    assert_eq!(
        body_value["spec"]["class"],
        "io.debezium.connector.postgresql.PostgresConnector"
    );

    // Reply with a 200 so the kube client unblocks.
    let echo = serde_json::json!({
        "apiVersion": "kafka.strimzi.io/v1beta2",
        "kind": "KafkaConnector",
        "metadata": {"name": "users-debezium-pg", "namespace": "ingest"},
        "spec": body_value["spec"],
    });
    kc_resp.send_response(
        Response::builder()
            .status(200)
            .header("content-type", "application/json")
            .body(Body::from(echo.to_string().into_bytes()))
            .unwrap(),
    );

    // Second request: FlinkDeployment PATCH.
    let (fl_req, fl_resp) = handle
        .next_request()
        .await
        .expect("second request should be sent");
    assert_eq!(fl_req.method().as_str(), "PATCH");
    let fl_uri = fl_req.uri().to_string();
    assert!(
        fl_uri.contains(
            "/apis/flink.apache.org/v1beta1/namespaces/ingest/flinkdeployments/users-iceberg-sink"
        ),
        "unexpected flink uri: {fl_uri}",
    );
    let fl_body = fl_req.into_body().collect().await.unwrap().to_bytes();
    let fl_json: serde_json::Value = serde_json::from_slice(&fl_body).unwrap();
    assert_eq!(fl_json["kind"], "FlinkDeployment");
    assert_eq!(fl_json["spec"]["image"], "apache/flink:1.18-scala_2.12-java11");
    let echo_flink = serde_json::json!({
        "apiVersion": "flink.apache.org/v1beta1",
        "kind": "FlinkDeployment",
        "metadata": {"name": "users-iceberg-sink", "namespace": "ingest"},
        "spec": fl_json["spec"],
    });
    fl_resp.send_response(
        Response::builder()
            .status(200)
            .header("content-type", "application/json")
            .body(Body::from(echo_flink.to_string().into_bytes()))
            .unwrap(),
    );

    apply_task
        .await
        .expect("apply task joined")
        .expect("apply_resources succeeded");
}

#[tokio::test]
async fn apply_resources_skips_flink_when_no_iceberg_sink() {
    let (mock_service, mut handle) =
        mock::pair::<Request<Body>, Response<Body>>();
    let client = Client::new(mock_service, "default");

    let mut spec = sample_spec();
    spec.iceberg_sink = None;
    let rendered = render_resources(&spec).expect("render");

    let apply_task = tokio::spawn({
        let client = client.clone();
        async move { apply_resources(&client, &rendered).await }
    });

    let (req, resp) = handle.next_request().await.expect("kc request");
    assert!(req.uri().path().contains("/kafkaconnectors/"));
    let body = req.into_body().collect().await.unwrap().to_bytes();
    resp.send_response(
        Response::builder()
            .status(200)
            .header("content-type", "application/json")
            .body(Body::from(body))
            .unwrap(),
    );

    apply_task.await.unwrap().unwrap();

    // No further requests should be issued — drop the handle and verify the
    // apply task already returned (which it did, otherwise the await above
    // would have blocked).
    drop(handle);
}
