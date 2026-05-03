//! Ephemeral Postgres harness for integration tests.
//!
//! [`boot_postgres`] returns a started container plus a connected
//! `sqlx::PgPool`. The container handle must be kept alive for the
//! duration of the test (drop ⇒ teardown). The caller is responsible
//! for applying its own migrations via `sqlx::migrate!("./migrations")`.
//!
//! Boot is hardened against transient connection refusals during
//! container startup with up to 30 retries / 500 ms each.

use std::time::Duration;

use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;
use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};

const POSTGRES_PORT: u16 = 5432;
const PG_PASSWORD: &str = "postgres";
const PG_DB: &str = "openfoundry";

/// Boot a `postgres:16-alpine` container and return a connected pool.
///
/// Keep the returned `ContainerAsync` alive for the test lifetime; the
/// container is removed when it is dropped.
pub async fn boot_postgres() -> (ContainerAsync<GenericImage>, PgPool, String) {
    let container = GenericImage::new("postgres", "16-alpine")
        .with_wait_for(WaitFor::message_on_stderr(
            "database system is ready to accept connections",
        ))
        .with_exposed_port(POSTGRES_PORT.tcp())
        .with_env_var("POSTGRES_PASSWORD", PG_PASSWORD)
        .with_env_var("POSTGRES_DB", PG_DB)
        .start()
        .await
        .expect("postgres container failed to start");

    let host = container.get_host().await.expect("container host");
    let port = container
        .get_host_port_ipv4(POSTGRES_PORT)
        .await
        .expect("container port");

    let url = format!("postgres://postgres:{PG_PASSWORD}@{host}:{port}/{PG_DB}");

    let mut attempts = 0;
    let pool = loop {
        match PgPoolOptions::new().max_connections(8).connect(&url).await {
            Ok(pool) => break pool,
            Err(error) if attempts < 30 => {
                attempts += 1;
                tokio::time::sleep(Duration::from_millis(500)).await;
                eprintln!("waiting for postgres ({attempts}): {error}");
            }
            Err(error) => panic!("postgres never became reachable: {error}"),
        }
    };

    (container, pool, url)
}
