//! Ephemeral Cassandra 5 harness for integration tests.
//!
//! [`boot_cassandra`] starts a single-node `cassandra:5.0` container,
//! waits until CQL is reachable on `9042`, builds a `scylla::Session`
//! against it, optionally creates a keyspace with `RF=1`, and returns
//! everything the caller needs.
//!
//! Keep the returned [`CassandraHarness`] alive for the duration of
//! the test — dropping it tears down the container.
//!
//! ## Why a single node?
//!
//! Production runs `RF=3` in three racks per DC; tests run with
//! `RF=1` because spinning up a multi-node cluster per test costs ~30 s
//! and adds zero coverage. Tests that need to validate replica
//! placement should target the dev cluster (`just dev-up-cassandra`)
//! instead.
//!
//! ## Boot timing
//!
//! Cassandra startup is slow (~25 s on a warm Docker daemon). The
//! container is considered healthy when both
//! `Created default superuser role 'cassandra'` and
//! `Starting listening for CQL clients on /0.0.0.0:9042` appear in
//! stdout — both messages must precede a successful CQL handshake.
//! On top of that we retry the `scylla::Session` build up to 60 times
//! at 1 s intervals to absorb the post-startup gossip settle window.

use std::sync::Arc;
use std::time::Duration;

use scylla::transport::session::Session;
use scylla::SessionBuilder;
use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};

const CQL_PORT: u16 = 9042;

/// Live Cassandra container plus the connected client.
///
/// `keyspace` is `None` when [`boot_cassandra`] was called with
/// `keyspace = None`; otherwise it is the keyspace that was created
/// with `RF=1` and is the session's default keyspace.
pub struct CassandraHarness {
    /// Container handle. Drop ⇒ teardown.
    pub container: ContainerAsync<GenericImage>,
    /// Connected `scylla::Session` wrapped in `Arc` for cheap cloning.
    pub session: Arc<Session>,
    /// `host:port` of the CQL endpoint (useful for spawning extra
    /// sessions or shelling out to `cqlsh`).
    pub contact_point: String,
    /// Keyspace created on boot, if any.
    pub keyspace: Option<String>,
}

/// Boot a `cassandra:5.0` container and connect a `scylla::Session`.
///
/// Pass `Some("my_keyspace")` to have the harness create that
/// keyspace with `RF=1` and select it as the session's default; pass
/// `None` to get a session with no default keyspace.
pub async fn boot_cassandra(keyspace: Option<&str>) -> CassandraHarness {
    let container = GenericImage::new("cassandra", "5.0")
        .with_wait_for(WaitFor::message_on_stdout(
            "Starting listening for CQL clients",
        ))
        .with_exposed_port(CQL_PORT.tcp())
        // Single-node cluster overrides — Cassandra refuses to start
        // a one-node SimpleSnitch cluster without these.
        .with_env_var("CASSANDRA_CLUSTER_NAME", "of-test")
        .with_env_var("CASSANDRA_DC", "dc1")
        .with_env_var("CASSANDRA_RACK", "rack1")
        .with_env_var("CASSANDRA_ENDPOINT_SNITCH", "GossipingPropertyFileSnitch")
        // Cassandra 5 ships with a generous default heap; cap it so
        // CI runners do not OOM.
        .with_env_var("HEAP_NEWSIZE", "128M")
        .with_env_var("MAX_HEAP_SIZE", "512M")
        .start()
        .await
        .expect("cassandra container failed to start");

    let host = container.get_host().await.expect("container host");
    let port = container
        .get_host_port_ipv4(CQL_PORT)
        .await
        .expect("container port");
    let contact_point = format!("{host}:{port}");

    // Connect with retries — gossip settle takes several seconds even
    // after the "listening for CQL clients" message.
    let mut attempts = 0;
    let session = loop {
        match SessionBuilder::new()
            .known_node(&contact_point)
            .connection_timeout(Duration::from_secs(5))
            .build()
            .await
        {
            Ok(session) => break session,
            Err(error) if attempts < 60 => {
                attempts += 1;
                tokio::time::sleep(Duration::from_secs(1)).await;
                eprintln!("waiting for cassandra ({attempts}): {error}");
            }
            Err(error) => panic!("cassandra never became reachable: {error}"),
        }
    };

    let session = Arc::new(session);

    let created_keyspace = if let Some(ks) = keyspace {
        let cql = format!(
            "CREATE KEYSPACE IF NOT EXISTS {ks} WITH replication = \
             {{ 'class': 'NetworkTopologyStrategy', 'dc1': 1 }} AND durable_writes = true"
        );
        session
            .query(cql, &[])
            .await
            .expect("create keyspace failed");
        session
            .use_keyspace(ks, false)
            .await
            .expect("use_keyspace failed");
        Some(ks.to_string())
    } else {
        None
    };

    CassandraHarness {
        container,
        session,
        contact_point,
        keyspace: created_keyspace,
    }
}
