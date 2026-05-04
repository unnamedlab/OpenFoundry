//! Integration test against a real Cassandra container.
//!
//! Marked `#[ignore]` because CI runners without docker would
//! otherwise fail. Run locally with:
//!
//! ```text
//! cargo test -p cassandra-kernel --test integration -- --ignored --test-threads=1
//! ```

mod support;

use cassandra_kernel::{
    cql_migrate,
    migrate::{MigrationOutcome, apply},
};

#[tokio::test(flavor = "multi_thread")]
#[ignore = "requires docker"]
async fn migrations_are_idempotent() {
    let _ = tracing_subscriber::fmt::try_init();

    let cassandra = support::start_cassandra().await;
    let session = cassandra.session.clone();

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

    let first = apply(session.as_ref(), "kernel_it", migrations)
        .await
        .expect("first apply");
    assert!(first.iter().all(|(_, o)| *o == MigrationOutcome::Applied));

    let second = apply(session.as_ref(), "kernel_it", migrations)
        .await
        .expect("second apply");
    assert!(
        second
            .iter()
            .all(|(_, o)| *o == MigrationOutcome::AlreadyApplied)
    );
}
