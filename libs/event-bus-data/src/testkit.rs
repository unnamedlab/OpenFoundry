//! Integration-test helpers gated by the `it` feature.
//!
//! Spins up an ephemeral Apache Kafka broker via `testcontainers` so each
//! test gets its own isolated cluster. Requires Docker on the host and is
//! intended for CI only.

use std::time::Duration;

use rdkafka::admin::{AdminClient, AdminOptions, NewTopic, TopicReplication};
use rdkafka::client::DefaultClientContext;
use rdkafka::ClientConfig;
use testcontainers::core::{ContainerPort, WaitFor};
use testcontainers::runners::AsyncRunner;
use testcontainers::{ContainerAsync, GenericImage, ImageExt};

use crate::config::{DataBusConfig, ServicePrincipal};

/// A running ephemeral Kafka broker for tests. Drop the value to tear it
/// down.
pub struct EphemeralKafka {
    _container: ContainerAsync<GenericImage>,
    pub bootstrap_servers: String,
}

impl EphemeralKafka {
    /// Start an Apache Kafka broker in KRaft single-node mode. Auto-create is
    /// disabled at the broker level so tests must provision topics
    /// explicitly via [`EphemeralKafka::create_topic`].
    pub async fn start() -> Result<Self, Box<dyn std::error::Error + Send + Sync>> {
        let image = GenericImage::new("apache/kafka", "3.7.0")
            .with_exposed_port(ContainerPort::Tcp(9092))
            .with_wait_for(WaitFor::message_on_stdout("Kafka Server started"))
            .with_env_var("KAFKA_AUTO_CREATE_TOPICS_ENABLE", "false")
            // KRaft single-node defaults are baked into the image; we only
            // need to publish the listener address so the host can reach it.
            .with_env_var(
                "KAFKA_ADVERTISED_LISTENERS",
                "PLAINTEXT://localhost:9092,CONTROLLER://localhost:9093",
            );
        let container = image.start().await?;
        let host_port = container.get_host_port_ipv4(9092).await?;
        Ok(Self {
            _container: container,
            bootstrap_servers: format!("localhost:{host_port}"),
        })
    }

    /// Build a [`DataBusConfig`] pointed at this broker using a PLAINTEXT
    /// dev principal.
    pub fn config_for(&self, service: &str) -> DataBusConfig {
        DataBusConfig::new(&self.bootstrap_servers, ServicePrincipal::insecure_dev(service))
    }

    /// Provision a topic via the AdminClient. Required because broker-level
    /// auto-create is disabled.
    pub async fn create_topic(
        &self,
        name: &str,
        partitions: i32,
    ) -> Result<(), Box<dyn std::error::Error + Send + Sync>> {
        let admin: AdminClient<DefaultClientContext> = ClientConfig::new()
            .set("bootstrap.servers", &self.bootstrap_servers)
            .create()?;
        let topic = NewTopic::new(name, partitions, TopicReplication::Fixed(1));
        let opts = AdminOptions::new().request_timeout(Some(Duration::from_secs(10)));
        let results = admin.create_topics(&[topic], &opts).await?;
        for r in results {
            r.map_err(|(t, e)| format!("create_topic({t}) failed: {e}"))?;
        }
        Ok(())
    }
}
