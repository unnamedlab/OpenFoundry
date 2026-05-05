//! Shared test harness for `iceberg-catalog-service` integration tests.
//!
//! Boots a Postgres testcontainer, applies the iceberg migrations,
//! builds the same router that production uses, and exposes helpers
//! for issuing JWT bearers with the iceberg scope shape.

#![allow(dead_code)]

use std::time::Duration;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use iceberg_catalog_service::{AppState, IcebergState, authz, build_router, testing as test_helpers};
use sqlx::PgPool;
use testcontainers::{ContainerAsync, GenericImage};
use testing::containers::boot_postgres;
use wiremock::MockServer;

const ICEBERG_TEST_SECRET: &str = "iceberg-catalog-test-secret-please-change";

pub struct Harness {
    pub _container: ContainerAsync<GenericImage>,
    pub pool: PgPool,
    pub router: Router,
    pub mock: MockServer,
}

impl Harness {
    pub fn read_token(&self) -> String {
        scoped_token(&[
            "api:iceberg-read",
            "role:viewer",
            "iceberg-clearance:public",
            "iceberg-clearance:confidential",
            "iceberg-clearance:pii",
            "iceberg-clearance:restricted",
            "iceberg-clearance:secret",
        ])
    }

    pub fn write_token(&self) -> String {
        scoped_token(&[
            "api:iceberg-read",
            "api:iceberg-write",
            "role:admin",
            "iceberg-clearance:*",
        ])
    }
}

pub async fn spawn() -> Harness {
    // Inject the test secret BEFORE the lazy-initialized JWT key is read.
    unsafe {
        std::env::set_var("OPENFOUNDRY_JWT_SECRET", ICEBERG_TEST_SECRET);
    }

    let (container, pool, _url) = boot_postgres().await;

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply iceberg-catalog-service migrations");

    let mock = MockServer::start().await;
    let oauth_url = mock.uri();

    let jwt_config = JwtConfig::new(ICEBERG_TEST_SECRET)
        .with_issuer("foundry-iceberg")
        .with_audience("iceberg-catalog");

    let http = reqwest::Client::builder()
        .timeout(Duration::from_secs(5))
        .build()
        .expect("reqwest client");

    let authz_engine = authz::bootstrap_engine().await;

    let state = AppState::new(IcebergState {
        db: pool.clone(),
        jwt_config,
        warehouse_uri: "s3://foundry-iceberg-test".to_string(),
        identity_federation_url: mock.uri(),
        oauth_integration_url: oauth_url,
        default_token_ttl_secs: 3600,
        long_lived_token_ttl_secs: 86_400,
        jwt_issuer: "foundry-iceberg".to_string(),
        jwt_audience: "iceberg-catalog".to_string(),
        http,
        authz: authz_engine,
        default_tenant: "test-tenant".to_string(),
    });

    let router = build_router(state);

    Harness {
        _container: container,
        pool,
        router,
        mock,
    }
}

pub fn scoped_token(scopes: &[&str]) -> String {
    let cfg = JwtConfig::new(ICEBERG_TEST_SECRET)
        .with_issuer("foundry-iceberg")
        .with_audience("iceberg-catalog");
    let scope_strings: Vec<String> = scopes.iter().map(|s| s.to_string()).collect();
    test_helpers::issue_internal_jwt(
        &cfg,
        "00000000-0000-0000-0000-000000000001",
        "foundry-iceberg",
        "iceberg-catalog",
        &scope_strings,
        3600,
    )
    .expect("issue test token")
}
