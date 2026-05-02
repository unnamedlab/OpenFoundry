//! Shared harness for `data-asset-catalog-service` integration tests.
//!
//! Each test file calls [`spawn`] to obtain a fresh Postgres container,
//! a router built from the production [`build_router`], and a JWT it
//! can use to authenticate. The container handle in [`Harness`] must be
//! kept alive for the duration of the test (drop ⇒ teardown).

#![allow(dead_code)]

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use data_asset_catalog_service::{AppState, build_router, config::LakehousePrefixes};
use sqlx::PgPool;
use storage_abstraction::backend::StorageBackend;
use storage_abstraction::local::LocalStorage;
use tempfile::TempDir;
use testcontainers::{ContainerAsync, GenericImage};
use testing::{containers::boot_postgres, fixtures};

pub struct Harness {
    pub container: ContainerAsync<GenericImage>,
    pub pool: PgPool,
    pub router: Router,
    pub jwt_config: JwtConfig,
    pub token: String,
    pub mock: wiremock::MockServer,
    pub _storage_dir: TempDir,
}

pub async fn spawn() -> Harness {
    let (container, pool, _url) = boot_postgres().await;

    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("apply data-asset-catalog-service migrations");

    let dir = tempfile::tempdir().expect("temp storage root");
    let storage: Arc<dyn StorageBackend> =
        Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).expect("local storage"));

    let jwt_config = fixtures::jwt_config();
    let token = fixtures::dev_token(&jwt_config, vec!["dataset.read".into(), "dataset.write".into()]);

    let mock = wiremock::MockServer::start().await;
    let quality_url = mock.uri();

    let state = AppState {
        db: pool.clone(),
        storage,
        lakehouse_prefixes: LakehousePrefixes::default(),
        dataset_quality_service_url: quality_url,
        jwt_config: Arc::new(jwt_config.clone()),
        http_client: reqwest::Client::new(),
        marking_resolver: None,
    };
    let router = build_router(state);

    Harness {
        container,
        pool,
        router,
        jwt_config,
        token,
        mock,
        _storage_dir: dir,
    }
}
