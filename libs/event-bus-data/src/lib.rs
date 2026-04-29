//! Data plane event bus built on Apache Kafka via `rdkafka`.
//!
//! This crate is the counterpart to `event-bus-control` (NATS). The split is
//! intentional:
//!
//! | Plane                | Transport          | Latency | Retention | Throughput | Use cases                                           |
//! | -------------------- | ------------------ | ------- | --------- | ---------- | --------------------------------------------------- |
//! | `event-bus-control`  | NATS JetStream     | µs–ms   | hours/days| MB/s       | RPC-ish events, signals, control flow, fan-out      |
//! | `event-bus-data`     | Apache Kafka       | ms      | weeks–PB  | GB–PB/s    | CDC, ingestion, lineage, analytics-grade firehoses  |
//!
//! ## Delivery semantics
//!
//! [`DataPublisher`] and [`DataSubscriber`] expose **at-least-once** delivery
//! with **explicit commits**: consumers MUST call
//! [`DataMessage::commit`](subscriber::DataMessage::commit) (or
//! [`DataSubscriber::commit_offsets`]) after a record has been durably
//! processed. Auto-commit is disabled in the default consumer configuration.
//!
//! ## OpenLineage headers
//!
//! Records carry a small, well-known set of Kafka headers modelling the
//! [OpenLineage] facets that the platform propagates through pipelines:
//! `ol-namespace`, `ol-job-name`, `ol-run-id`, `ol-event-time`,
//! `ol-producer`, `ol-schema-url`. See [`headers::OpenLineageHeaders`].
//!
//! [OpenLineage]: https://openlineage.io
//!
//! ## Auto-creation and ACLs
//!
//! Topic auto-creation is **disabled** for both producers and consumers
//! (`allow.auto.create.topics=false`). Topic provisioning is handled out of
//! band by the platform's topic registry, and every service authenticates
//! with its own SASL principal via [`config::ServicePrincipal`].

pub mod config;
pub mod headers;
pub mod publisher;
pub mod subscriber;

#[cfg(feature = "it")]
pub mod testkit;

pub use config::{DataBusConfig, ServicePrincipal};
pub use headers::OpenLineageHeaders;
pub use publisher::{DataPublisher, KafkaPublisher, PublishError};
pub use subscriber::{CommitError, DataMessage, DataSubscriber, KafkaSubscriber, SubscribeError};
