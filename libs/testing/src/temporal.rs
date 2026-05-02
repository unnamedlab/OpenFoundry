//! Ephemeral Temporal harness for integration tests.
//!
//! [`boot_temporal`] starts a `temporalio/auto-setup:1.24` container
//! — the all-in-one image that includes the Temporal server, the
//! default namespace bootstrapper, and an embedded SQLite for
//! persistence — and waits until the gRPC frontend on `7233` is
//! reachable.
//!
//! Why `auto-setup` instead of the production combo
//! (server + cassandra)? Tests do not need to validate Cassandra
//! schema migrations of Temporal itself (that is covered by the
//! upstream Temporal release suite); they need a working Temporal
//! frontend to push workflows at. `auto-setup` ships a default
//! namespace and is ready in ~10 s on a warm Docker daemon.
//!
//! Per [ADR-0021](../../docs/architecture/adr/ADR-0021-temporal-on-cassandra-go-workers.md),
//! production Temporal pins to v1.24 — keep this image in lockstep
//! with that ADR when bumping versions.
//!
//! ## What this module does *not* do
//!
//! Build a Temporal SDK client. The Rust workspace does not yet
//! depend on the Temporal Rust SDK; tests that need a typed client
//! should add `temporal-sdk-core = "..."` to their own
//! `dev-dependencies` and connect to [`TemporalHarness::frontend`].

use std::time::Duration;

use testcontainers::{
    ContainerAsync, GenericImage, ImageExt,
    core::{IntoContainerPort, WaitFor},
    runners::AsyncRunner,
};

const FRONTEND_PORT: u16 = 7233;
const UI_PORT: u16 = 8233;
const TEMPORAL_IMAGE: &str = "temporalio/auto-setup";
const TEMPORAL_TAG: &str = "1.24";

/// Live Temporal container.
///
/// Drop ⇒ teardown.
pub struct TemporalHarness {
    /// Container handle.
    pub container: ContainerAsync<GenericImage>,
    /// `host:port` of the Temporal gRPC frontend (`temporal://…/`).
    pub frontend: String,
    /// `host:port` of the Temporal Web UI (handy for manual
    /// inspection from a debugger session).
    pub web_ui: String,
    /// Default namespace created by `auto-setup`.
    pub namespace: String,
}

/// Boot a `temporalio/auto-setup:1.24` container.
pub async fn boot_temporal() -> TemporalHarness {
    let container = GenericImage::new(TEMPORAL_IMAGE, TEMPORAL_TAG)
        .with_wait_for(WaitFor::message_on_stdout(
            "Starting to serve on",
        ))
        .with_exposed_port(FRONTEND_PORT.tcp())
        .with_exposed_port(UI_PORT.tcp())
        // auto-setup defaults: SQLite persistence, default namespace
        // already created. We pin them explicitly so behaviour is
        // independent of upstream image defaults drifting.
        .with_env_var("DB", "sqlite")
        .with_env_var("SKIP_DEFAULT_NAMESPACE_CREATION", "false")
        .with_env_var("DEFAULT_NAMESPACE", "default")
        .with_env_var("DEFAULT_NAMESPACE_RETENTION", "1d")
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

    // Give the frontend a moment to finish wiring after the log line
    // appears — auto-setup serialises namespace creation behind it.
    tokio::time::sleep(Duration::from_millis(500)).await;

    TemporalHarness {
        container,
        frontend,
        web_ui,
        namespace: "default".to_string(),
    }
}
