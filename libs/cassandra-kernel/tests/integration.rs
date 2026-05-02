//! Integration test against a real Cassandra container.
//!
//! Marked `#[ignore]` because CI runners without docker would
//! otherwise fail. Run locally with:
//!
//! ```text
//! cargo test -p cassandra-kernel -- --ignored
//! ```

use std::time::Duration;

use cassandra_kernel::{
    ClusterConfig, SessionBuilder, cql_migrate,
    migrate::{MigrationOutcome, apply},
};
use testcontainers::{
    GenericImage,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};

#[tokio::test(flavor = "multi_thread")]
#[ignore = "requires docker"]
async fn migrations_are_idempotent() {
    let _ = tracing_subscriber::fmt::try_init();

    let image = GenericImage::new("cassandra", "5.0.2")
        .with_exposed_port(9042.tcp())
        .with_wait_for(WaitFor::message_on_stdout(
            "Starting listening for CQL clients",
        ));
    let container = image.start().await.expect("starting cassandra container");
    let host = container
        .get_host()
        .await
        .expect("container host")
        .to_string();
    let port = container
        .get_host_port_ipv4(9042)
        .await
        .expect("mapped port");

    // Cassandra needs a beat after the listening message before
    // accepting connections cleanly.
    tokio::time::sleep(Duration::from_secs(5)).await;

    let cfg = ClusterConfig {
        contact_points: vec![format!("{host}:{port}")],
        local_datacenter: "datacenter1".to_string(),
        ..ClusterConfig::dev_local()
    };
    let session = SessionBuilder::new(cfg).build().await.expect("session");

    session
        .query(
            "CREATE KEYSPACE IF NOT EXISTS kernel_it WITH replication = \
             {'class': 'SimpleStrategy', 'replication_factor': 1}",
            &[],
        )
        .await
        .expect("create keyspace");

    let migrations = cql_migrate![
        1, "create_t" => &[
            "CREATE TABLE IF NOT EXISTS kernel_it.t (id int PRIMARY KEY, v text)",
        ],
        2, "add_aux" => &[
            "CREATE TABLE IF NOT EXISTS kernel_it.aux (id int PRIMARY KEY)",
        ],
    ];

    let first = apply(&session, "kernel_it", migrations)
        .await
        .expect("first apply");
    assert!(first.iter().all(|(_, o)| *o == MigrationOutcome::Applied));

    let second = apply(&session, "kernel_it", migrations)
        .await
        .expect("second apply");
    assert!(
        second
            .iter()
            .all(|(_, o)| *o == MigrationOutcome::AlreadyApplied)
    );
}
