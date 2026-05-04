//! FASE 3 / Tarea 3.4 — kube fake-client integration test for
//! [`pipeline_build_service::spark::submit_pipeline_run`].
//!
//! Mirrors `services/ingestion-replication-service/tests/control_plane_kube_stub.rs`:
//! drives the function under test against a `tower_test::mock` →
//! `kube::Client` and asserts the POST URI / body without requiring
//! a real Kubernetes API server.

use http::{Request, Response};
use http_body_util::BodyExt;
use kube::Client;
use kube::client::Body;
use serde_json::Value as JsonValue;
use tower_test::mock;

use pipeline_build_service::spark::{
    self, PipelineRunInput, SparkApplicationType, SparkResourceOverrides, SPARK_GROUP,
    SPARK_VERSION, SPARK_PLURAL,
};

fn sample_input() -> PipelineRunInput {
    PipelineRunInput {
        pipeline_id: "p-7c1a".into(),
        run_id: "r-stub".into(),
        namespace: "openfoundry-spark".into(),
        application_type: SparkApplicationType::Scala,
        pipeline_runner_image: "localhost:5001/pipeline-runner:0.1.0".into(),
        input_dataset_rid: "ri.dataset.main.in".into(),
        output_dataset_rid: "ri.dataset.main.out".into(),
        resources: SparkResourceOverrides::default(),
    }
}

#[tokio::test]
async fn submit_pipeline_run_posts_sparkapplication_to_correct_endpoint() {
    let (mock_service, mut handle) = mock::pair::<Request<Body>, Response<Body>>();
    let client = Client::new(mock_service, "openfoundry-spark");
    let input = sample_input();

    // Drive the kube call and the mock side concurrently.
    let submit_task = tokio::spawn({
        let client = client.clone();
        let input = input.clone();
        async move { spark::submit_pipeline_run(client, &input).await }
    });

    let (req, send) = handle
        .next_request()
        .await
        .expect("submit_pipeline_run must issue exactly one HTTP request");

    // Method + URI assertions — the request MUST hit the SparkOperator
    // CRD endpoint scoped to the correct namespace.
    assert_eq!(req.method().as_str(), "POST");
    let uri = req.uri().to_string();
    let expected_path = format!(
        "/apis/{group}/{version}/namespaces/{ns}/{plural}",
        group = SPARK_GROUP,
        version = SPARK_VERSION,
        ns = input.namespace,
        plural = SPARK_PLURAL,
    );
    assert!(
        uri.contains(&expected_path),
        "request URI {uri} does not contain expected SparkApplication path {expected_path}"
    );

    // Body assertions — the rendered manifest must carry the canonical
    // composite name and the dataset RIDs we passed in.
    let body_bytes = req
        .into_body()
        .collect()
        .await
        .expect("collect body")
        .to_bytes();
    let body_json: JsonValue = serde_json::from_slice(&body_bytes).expect("body must be JSON");
    assert_eq!(
        body_json["kind"], "SparkApplication",
        "wrong kind in submitted manifest: {body_json}"
    );
    assert_eq!(
        body_json["metadata"]["name"], "pipeline-run-p-7c1a-r-stub",
        "metadata.name must follow the canonical pipeline-run-<pid>-<rid> format"
    );
    assert_eq!(body_json["metadata"]["namespace"], "openfoundry-spark");
    let args = body_json["spec"]["arguments"]
        .as_array()
        .expect("spec.arguments must be a list");
    assert!(args.iter().any(|a| a == "ri.dataset.main.in"));
    assert!(args.iter().any(|a| a == "ri.dataset.main.out"));

    // Reply with the same object (the API server echoes the created
    // resource — that is what `kube::Api::create` deserializes).
    let response_body = serde_json::to_vec(&body_json).expect("re-encode body");
    send.send_response(
        Response::builder()
            .status(http::StatusCode::CREATED)
            .header(http::header::CONTENT_TYPE, "application/json")
            .body(Body::from(response_body))
            .expect("build response"),
    );

    let returned_name = submit_task
        .await
        .expect("join")
        .expect("submit_pipeline_run must succeed");
    assert_eq!(returned_name, "pipeline-run-p-7c1a-r-stub");
}

#[tokio::test]
async fn submit_pipeline_run_surfaces_invalid_input_without_calling_kube() {
    // No mock service required — the call must short-circuit before
    // reaching the transport. Build a no-op client just to satisfy the
    // type signature; the test asserts we never use it.
    let (mock_service, _handle) = mock::pair::<Request<Body>, Response<Body>>();
    let client = Client::new(mock_service, "openfoundry-spark");

    let mut bad = sample_input();
    bad.pipeline_runner_image = String::new();

    let err = spark::submit_pipeline_run(client, &bad)
        .await
        .expect_err("blank image must be rejected");
    match err {
        spark::SparkSubmitError::InvalidInput(msg) => {
            assert!(msg.contains("pipeline_runner_image"), "{msg}");
        }
        other => panic!("expected InvalidInput, got {other:?}"),
    }
}
