//! Integration-test helpers gated by the `it` feature.
//!
//! Spins up an ephemeral Apache Kafka broker via `testcontainers` so each
//! test gets its own isolated cluster. Requires Docker on the host and is
//! intended for CI only.

use std::net::TcpListener;
use std::time::Duration;

use rdkafka::ClientConfig;
use rdkafka::admin::{AdminClient, AdminOptions, NewTopic, TopicReplication};
use rdkafka::client::DefaultClientContext;
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
        let host_port = free_local_port()?;
        let advertised_listeners = format!("PLAINTEXT://127.0.0.1:{host_port}");
        let image = GenericImage::new("apache/kafka", "3.7.1")
            .with_wait_for(WaitFor::message_on_stdout("Kafka Server started"))
            .with_mapped_port(host_port, ContainerPort::Tcp(9092))
            .with_env_var("KAFKA_NODE_ID", "1")
            .with_env_var("KAFKA_PROCESS_ROLES", "broker,controller")
            .with_env_var(
                "KAFKA_LISTENERS",
                "PLAINTEXT://0.0.0.0:9092,CONTROLLER://0.0.0.0:9093",
            )
            .with_env_var("KAFKA_ADVERTISED_LISTENERS", advertised_listeners)
            .with_env_var("KAFKA_CONTROLLER_LISTENER_NAMES", "CONTROLLER")
            // KRaft requires every listener name, including CONTROLLER, in the protocol map.
            .with_env_var(
                "KAFKA_LISTENER_SECURITY_PROTOCOL_MAP",
                "PLAINTEXT:PLAINTEXT,CONTROLLER:PLAINTEXT",
            )
            .with_env_var("KAFKA_CONTROLLER_QUORUM_VOTERS", "1@127.0.0.1:9093")
            .with_env_var("KAFKA_INTER_BROKER_LISTENER_NAME", "PLAINTEXT")
            .with_env_var("KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR", "1")
            .with_env_var("KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR", "1")
            .with_env_var("KAFKA_TRANSACTION_STATE_LOG_MIN_ISR", "1")
            .with_env_var("KAFKA_AUTO_CREATE_TOPICS_ENABLE", "false")
            .with_env_var("KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS", "0");
        let container = image.start().await?;
        Ok(Self {
            _container: container,
            bootstrap_servers: format!("127.0.0.1:{host_port}"),
        })
    }

    /// Build a [`DataBusConfig`] pointed at this broker using a PLAINTEXT
    /// dev principal.
    pub fn config_for(&self, service: &str) -> DataBusConfig {
        // Local librdkafka test builds may omit libzstd; compression is not part of the flow contract.
        DataBusConfig::new(
            &self.bootstrap_servers,
            ServicePrincipal::insecure_dev(service),
        )
        .without_compression()
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

fn free_local_port() -> std::io::Result<u16> {
    TcpListener::bind(("127.0.0.1", 0)).and_then(|listener| {
        let port = listener.local_addr()?.port();
        drop(listener);
        Ok(port)
    })
}
