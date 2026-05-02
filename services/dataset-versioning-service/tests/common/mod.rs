//! Shared harness for `dataset-versioning-service` integration tests.

#![allow(dead_code)]

use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use axum::Router;
use dataset_versioning_service::{
    AppState, DatasetWriter, build_router, storage::LegacyDatasetWriter,
};
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
        .expect("apply dataset-versioning-service migrations");

    let dir = tempfile::tempdir().expect("temp storage root");
    let backend: Arc<dyn StorageBackend> =
        Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).expect("local storage"));
    let writer: Arc<dyn DatasetWriter> =
        Arc::new(LegacyDatasetWriter::new(backend, "tests"));

    let jwt_config = fixtures::jwt_config();
    let token =
        fixtures::dev_token(&jwt_config, vec!["dataset.read".into(), "dataset.write".into()]);

    let mock = wiremock::MockServer::start().await;
    let neighbour = mock.uri();

    let state = AppState {
        db: pool.clone(),
        jwt_config: Arc::new(jwt_config.clone()),
        writer,
        http: reqwest::Client::new(),
        retention_policy_url: Some(neighbour.clone()),
        data_asset_catalog_url: Some(neighbour),
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

/// Insert a minimal `datasets` row in the versioning DB and a default
/// `master` branch so transaction handlers find a parent. Returns the
/// dataset id.
pub async fn seed_dataset_with_master(pool: &PgPool, rid: &str) -> uuid::Uuid {
    let id = fixtures::seed_dataset(pool, rid, rid, "parquet").await;
    let branch_id = uuid::Uuid::now_v7();
    sqlx::query(
        r#"INSERT INTO dataset_branches (id, dataset_id, name, parent_branch_id, head_transaction_id, is_default)
           VALUES ($1, $2, 'master', NULL, NULL, TRUE)"#,
    )
    .bind(branch_id)
    .bind(id)
    .execute(pool)
    .await
    .expect("seed master branch");
    id
}
