//! Ephemeral Temporal harness for integration tests.
//!
//! [`boot_temporal`] starts a `temporalio/temporal` dev-server
//! container (`temporal server start-dev`). The CLI image includes the
//! Temporal server, Web UI, the default namespace bootstrapper, and
//! embedded SQLite persistence for local tests.
//!
//! Why `start-dev` instead of the production combo
//! (server + cassandra)? Tests do not need to validate Cassandra
//! schema migrations of Temporal itself (that is covered by the
//! upstream Temporal release suite); they need a working Temporal
//! frontend to push workflows at. The CLI dev server ships a default
//! namespace and is ready quickly on a warm Docker daemon.
//!
//! Per [ADR-0021](../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md),
//! production Temporal pins to v1.24. The dev-server image tracks the
//! Temporal CLI release line; it is for tests only and must not be used
//! as a production deployment reference.
//!
//! ## What this module does *not* do
//!
//! Build a Temporal SDK client. The Rust workspace does not yet
//! depend on the Temporal Rust SDK; tests that need a typed client
//! should add `temporal-sdk-core = "..."` to their own
//! `dev-dependencies` and connect to [`TemporalHarness::frontend`].

use std::time::Duration;

use testcontainers::{
    ContainerAsync, Image,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};
use tokio::net::TcpStream;

const FRONTEND_PORT: u16 = 7233;
const UI_PORT: u16 = 8233;
const TEMPORAL_IMAGE: &str = "temporalio/temporal";
const TEMPORAL_TAG: &str = "1.7.0";

#[derive(Debug, Clone)]
pub struct TemporalDevServerImage {
    ports: Vec<testcontainers::core::ContainerPort>,
}

impl Default for TemporalDevServerImage {
    fn default() -> Self {
        Self {
            ports: vec![FRONTEND_PORT.tcp(), UI_PORT.tcp()],
        }
    }
}

impl Image for TemporalDevServerImage {
    fn name(&self) -> &str {
        TEMPORAL_IMAGE
    }

    fn tag(&self) -> &str {
        TEMPORAL_TAG
    }

    fn ready_conditions(&self) -> Vec<WaitFor> {
        vec![WaitFor::seconds(1)]
    }

    fn cmd(&self) -> impl IntoIterator<Item = impl Into<std::borrow::Cow<'_, str>>> {
        [
            "server",
            "start-dev",
            "--ip",
            "0.0.0.0",
            "--ui-ip",
            "0.0.0.0",
            "--search-attribute",
            "audit_correlation_id=Keyword",
            "--log-level",
            "warn",
        ]
    }

    fn expose_ports(&self) -> &[testcontainers::core::ContainerPort] {
        &self.ports
    }
}

/// Live Temporal container.
///
/// Drop ⇒ teardown.
pub struct TemporalHarness {
    /// Container handle.
    pub container: ContainerAsync<TemporalDevServerImage>,
    /// `host:port` of the Temporal gRPC frontend (`temporal://…/`).
    pub frontend: String,
    /// `host:port` of the Temporal Web UI (handy for manual
    /// inspection from a debugger session).
    pub web_ui: String,
    /// Default namespace created by the dev server.
    pub namespace: String,
}

/// Boot a `temporalio/temporal` dev-server container.
pub async fn boot_temporal() -> TemporalHarness {
    let container = TemporalDevServerImage::default()
        .start()
        .await
        .expect("temporal container failed to start");

    let host = container.get_host().await.expect("container host");
    let frontend_port = container
        .get_host_port_ipv4(FRONTEND_PORT)
        .await
        .expect("frontend port");
    let ui_port = container
        .get_host_port_ipv4(UI_PORT)
        .await
        .expect("ui port");

    let frontend = format!("{host}:{frontend_port}");
    let web_ui = format!("http://{host}:{ui_port}");

    let deadline = tokio::time::Instant::now() + Duration::from_secs(30);
    loop {
        match TcpStream::connect(&frontend).await {
            Ok(_) => break,
            Err(error) if tokio::time::Instant::now() >= deadline => {
                panic!("Temporal frontend {frontend} did not become reachable: {error}");
            }
            Err(_) => tokio::time::sleep(Duration::from_millis(250)).await,
        }
    }

    TemporalHarness {
        container,
        frontend,
        web_ui,
        namespace: "default".to_string(),
    }
}
