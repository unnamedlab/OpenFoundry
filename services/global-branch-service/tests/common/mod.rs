//! Shared harness for global-branch-service integration tests.

#![allow(dead_code)]

use axum::Router;
use global_branch_service::{AppState, build_router};
use sqlx::PgPool;
use testcontainers::{ContainerAsync, GenericImage};
use testing::containers::boot_postgres;

pub struct Harness {
    pub container: ContainerAsync<GenericImage>,
    pub pool: PgPool,
    pub router: Router,
}

pub async fn spawn() -> Harness {
    let (container, pool, _url) = boot_postgres().await;
    sqlx::migrate!("./migrations")
        .run(&pool)
        .await
        .expect("global-branch-service migrations");
    let router = build_router(AppState::for_tests(pool.clone()));
    Harness {
        container,
        pool,
        router,
    }
}
