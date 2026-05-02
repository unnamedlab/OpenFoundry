//! Smoke test for the `ontology-actions-service` HTTP surface.
//!
//! Boots an ephemeral Postgres container via `testcontainers`, applies the
//! migrations owned by this crate, builds the production router via
//! [`ontology_actions_service::build_router`] and verifies the auth contract:
//!
//! * `GET /api/v1/ontology/actions` without an `Authorization` header → 401.
//! * Same request with a valid HS256 Bearer token → 200 plus a JSON
//!   envelope with an empty `data` list (the seeded database has no action
//!   types yet).
//!
//! Run with: `cargo test -p ontology-actions-service --test health -- --include-ignored`.
//! The `#[ignore]` attribute keeps `cargo test` light on developer machines
//! that lack a working Docker daemon.

use std::time::Duration;

use auth_middleware::jwt::{self, JwtConfig, build_access_claims, encode_token};
use http_body_util::BodyExt;
use ontology_actions_service::{build_router, jwt_config_from_secret};
use ontology_kernel::AppState;
use sqlx::{PgPool, postgres::PgPoolOptions};
use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use tower::ServiceExt;
use uuid::Uuid;

const POSTGRES_PORT: u16 = 5432;
const PG_PASSWORD: &str = "postgres";
const JWT_SECRET: &str = "ontology-actions-service-smoke-secret-do-not-use-in-prod";

async fn boot_postgres() -> (ContainerAsync<GenericImage>, PgPool) {
    let container = GenericImage::new("postgres", "16-alpine")
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_exposed_port(POSTGRES_PORT.tcp())
        .with_env_var("POSTGRES_PASSWORD", PG_PASSWORD)
        .with_env_var("POSTGRES_DB", "openfoundry")
        .start()
        .await
        .expect("postgres container failed to start");

    let host = container.get_host().await.expect("container host");
    let port = container
        .get_host_port_ipv4(POSTGRES_PORT)
        .await
        .expect("container port");

    let url = format!("postgres://postgres:{PG_PASSWORD}@{host}:{port}/openfoundry");

    let mut attempts = 0;
    let pool = loop {
        match PgPoolOptions::new()
            .max_connections(4)
            .connect(&url)
            .await
        {
            Ok(pool) => break pool,
            Err(error) if attempts < 30 => {
                attempts += 1;
                tokio::time::sleep(Duration::from_millis(500)).await;
                eprintln!("waiting for postgres ({attempts}): {error}");
            }
            Err(error) => panic!("postgres never became reachable: {error}"),
        }
    };

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply ontology-actions-service migrations");

    (container, pool)
}

fn build_state(pool: PgPool, jwt_config: JwtConfig) -> AppState {
    AppState {
        db: pool,
        http_client: reqwest::Client::new(),
        jwt_config,
        // Pointed at unreachable URLs on purpose: the smoke test never
        // exercises any code path that calls these collaborators.
        audit_service_url: "http://127.0.0.1:1".to_string(),
        dataset_service_url: "http://127.0.0.1:1".to_string(),
        ontology_service_url: "http://127.0.0.1:1".to_string(),
        pipeline_service_url: "http://127.0.0.1:1".to_string(),
        ai_service_url: "http://127.0.0.1:1".to_string(),
        notification_service_url: "http://127.0.0.1:1".to_string(),
        search_embedding_provider: "deterministic-hash".to_string(),
        node_runtime_command: "node".to_string(),
        connector_management_service_url: "http://127.0.0.1:1".to_string(),
    }
}

fn dev_token(jwt_config: &JwtConfig) -> String {
    let claims = build_access_claims(
        jwt_config,
        Uuid::new_v4(),
        "smoke@openfoundry.test",
        "Smoke Tester",
        vec!["ontology.editor".to_string()],
        vec!["ontology.actions.read".to_string()],
        None,
        serde_json::Value::Null,
        vec!["password".to_string()],
    );
    encode_token(jwt_config, &claims).expect("encode dev access token")
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
#[ignore = "requires Docker; run with `cargo test --test health -- --include-ignored`"]
async fn list_action_types_requires_bearer_token() {
    let (_container, pool) = boot_postgres().await;
    let jwt_config = jwt_config_from_secret(JWT_SECRET);
    let app = build_router(build_state(pool, jwt_config.clone()));

    // 1. Unauthenticated → 401.
    let response = app
        .clone()
        .oneshot(
            axum::http::Request::builder()
                .method("GET")
                .uri("/api/v1/ontology/actions")
                .body(axum::body::Body::empty())
                .unwrap(),
        )
        .await
        .expect("router responded");

    assert_eq!(
        response.status(),
        axum::http::StatusCode::UNAUTHORIZED,
        "missing Bearer token must yield 401"
    );

    // 2. Authenticated with a freshly-minted dev token → 200 + JSON envelope.
    let token = dev_token(&jwt_config);
    let response = app
        .oneshot(
            axum::http::Request::builder()
                .method("GET")
                .uri("/api/v1/ontology/actions")
                .header("authorization", format!("Bearer {token}"))
                .body(axum::body::Body::empty())
                .unwrap(),
        )
        .await
        .expect("router responded");

    assert_eq!(
        response.status(),
        axum::http::StatusCode::OK,
        "valid Bearer token must yield 200"
    );

    let body = response
        .into_body()
        .collect()
        .await
        .expect("read body")
        .to_bytes();
    let json: serde_json::Value = serde_json::from_slice(&body).expect("body is JSON");
    assert!(
        json.get("data").and_then(|v| v.as_array()).is_some(),
        "expected `data` array in list_action_types response, got {json}"
    );
    assert_eq!(json["total"].as_i64(), Some(0));
}

#[test]
fn jwt_round_trip_compiles_against_kernel_jwt_layer() {
    // Compile-time sanity: build a token with the same helper the smoke
    // test uses and ensure `decode_token` accepts it. This runs without
    // Docker so `cargo test -p ontology-actions-service` always exercises
    // the JWT wiring even when the integration test is skipped.
    let cfg = jwt_config_from_secret(JWT_SECRET);
    let token = dev_token(&cfg);
    let decoded = jwt::decode_token(&cfg, &token).expect("decode");
    assert_eq!(decoded.email, "smoke@openfoundry.test");
}
