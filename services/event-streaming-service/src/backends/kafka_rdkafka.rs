//! Real Apache Kafka backend gated by the `kafka-rdkafka` Cargo feature.
//!
//! This is the production replacement for [`super::KafkaUnavailableBackend`].
//! It implements the router-level [`Backend`] trait on top of `rdkafka` and
//! the platform's [`event_bus_data`] wrapper, which bakes in the data-plane
//! defaults (idempotent producer, `acks=all`, `enable.auto.commit=false`,
//! per-service SASL principal, etc.).
//!
//! Topic ↔ pattern semantics
//! -------------------------
//! The router's `subscribe(pattern)` accepts NATS-style patterns (`*` for one
//! token, `>` for "rest of subject"). Kafka has no native subject hierarchy
//! but `rdkafka` accepts a regex pattern when the topic string starts with
//! `^`. We translate the NATS-style pattern to a regex on the way in:
//!
//! | Router pattern  | Kafka subscription           |
//! | --------------- | ---------------------------- |
//! | `data.events`   | `data.events` (literal)      |
//! | `data.*`        | `^data\.[^.]+$`              |
//! | `data.>`        | `^data\..+$`                 |
//!
//! Each call to [`RdKafkaBackend::subscribe`] gets its own short-lived
//! `group.id` (prefix + UUID) so concurrent subscribers do not steal
//! partitions from one another. Callers that need a stable consumer group
//! must use `event-bus-data` directly.

use std::collections::BTreeMap;
use std::sync::Arc;

use async_trait::async_trait;
use bytes::Bytes;
use chrono::Utc;
use event_bus_data::config::{DataBusConfig, ServicePrincipal};
use rdkafka::Message;
use rdkafka::consumer::{Consumer, StreamConsumer};
use rdkafka::message::{Header, Headers, OwnedHeaders};
use rdkafka::producer::{FutureProducer, FutureRecord};
use rdkafka::util::Timeout;
use std::time::Duration;
use tokio::sync::mpsc;
use tokio_stream::StreamExt;
use tokio_stream::wrappers::ReceiverStream;
use uuid::Uuid;

use crate::router::BackendId;

use super::{Backend, BackendError, Envelope, EnvelopeStream};

/// Header key used to propagate `Envelope::schema_id` over Kafka. Mirrors the
/// NATS backend so subscribers see identical metadata regardless of transport.
const SCHEMA_ID_HEADER: &str = "x-of-schema-id";

/// Production Kafka backend.
///
/// Holds a long-lived [`FutureProducer`] (cheap to clone) and the
/// [`DataBusConfig`] needed to spin up per-subscription consumers on demand.
#[derive(Clone)]
pub struct RdKafkaBackend {
    producer: FutureProducer,
    bus_config: Arc<DataBusConfig>,
    group_prefix: String,
    /// Hard cap for `producer.send` queueing + delivery. Kept aligned with
    /// `event_bus_data::publisher::KafkaPublisher` (30 s) to avoid surprising
    /// callers that reuse the same broker across both layers.
    send_timeout: Duration,
}

impl RdKafkaBackend {
    /// Build a backend from a fully-resolved [`DataBusConfig`].
    pub fn new(
        config: DataBusConfig,
        group_prefix: impl Into<String>,
    ) -> Result<Self, BackendError> {
        let producer: FutureProducer =
            config
                .producer_config()
                .create()
                .map_err(|e| BackendError::Unavailable {
                    backend: BackendId::Kafka,
                    message: format!("could not build Kafka producer: {e}"),
                })?;
        Ok(Self {
            producer,
            bus_config: Arc::new(config),
            group_prefix: group_prefix.into(),
            send_timeout: Duration::from_secs(30),
        })
    }

    /// Convenience constructor reading `KAFKA_BOOTSTRAP_SERVERS`,
    /// `KAFKA_SASL_USERNAME` and `KAFKA_SASL_PASSWORD` from the environment.
    /// Falls back to a PLAINTEXT dev principal when SASL credentials are
    /// absent (test brokers / local docker-compose).
    pub fn from_env(service_name: &str) -> Result<Self, BackendError> {
        let bootstrap =
            std::env::var("KAFKA_BOOTSTRAP_SERVERS").map_err(|_| BackendError::Unavailable {
                backend: BackendId::Kafka,
                message: "KAFKA_BOOTSTRAP_SERVERS is not set".to_string(),
            })?;
        let principal = match (
            std::env::var("KAFKA_SASL_USERNAME").ok(),
            std::env::var("KAFKA_SASL_PASSWORD").ok(),
        ) {
            (Some(_user), Some(password)) => {
                // SASL principal user is the service identity (matches ACLs);
                // we ignore KAFKA_SASL_USERNAME if it differs to keep one
                // identity per service.
                ServicePrincipal::scram_sha_512(service_name.to_string(), password)
            }
            _ => ServicePrincipal::insecure_dev(service_name.to_string()),
        };
        Self::new(
            DataBusConfig::new(bootstrap, principal),
            format!("{service_name}-router"),
        )
    }
}

#[async_trait]
impl Backend for RdKafkaBackend {
    fn id(&self) -> BackendId {
        BackendId::Kafka
    }

    async fn publish(&self, envelope: Envelope) -> Result<(), BackendError> {
        if envelope.topic.is_empty() {
            return Err(BackendError::Rejected {
                backend: BackendId::Kafka,
                message: "envelope.topic must not be empty".to_string(),
            });
        }

        let headers = build_headers(&envelope);
        let topic = envelope.topic.clone();
        // `FutureRecord` borrows from its inputs; keep `payload` alive for
        // the duration of the await.
        let payload = envelope.payload;
        let record: FutureRecord<'_, [u8], [u8]> = FutureRecord::to(&topic)
            .payload(payload.as_ref())
            .headers(headers);

        match self
            .producer
            .send(record, Timeout::After(self.send_timeout))
            .await
        {
            Ok((partition, offset)) => {
                tracing::debug!(
                    topic = %topic,
                    partition,
                    offset,
                    "kafka publish acknowledged"
                );
                Ok(())
            }
            Err((err, _msg)) => Err(map_kafka_send_error(err)),
        }
    }

    async fn subscribe(&self, pattern: &str) -> Result<EnvelopeStream, BackendError> {
        if pattern.is_empty() {
            return Err(BackendError::Rejected {
                backend: BackendId::Kafka,
                message: "subscribe pattern must not be empty".to_string(),
            });
        }

        let group_id = format!("{}-{}", self.group_prefix, Uuid::now_v7());
        let consumer: StreamConsumer = self
            .bus_config
            .consumer_config(&group_id)
            .create()
            .map_err(|e| BackendError::Transport {
                backend: BackendId::Kafka,
                message: format!("could not build Kafka consumer: {e}"),
            })?;

        let kafka_pattern = translate_pattern(pattern);
        consumer
            .subscribe(&[kafka_pattern.as_str()])
            .map_err(|e| BackendError::Transport {
                backend: BackendId::Kafka,
                message: format!("subscribe({pattern}) -> {kafka_pattern}: {e}"),
            })?;

        let consumer = Arc::new(consumer);
        // Pump records from the StreamConsumer into a bounded mpsc so we can
        // hand back a `'static` Stream to the router. The spawned task owns
        // the only Arc kept alive by the closure; once the receiver is
        // dropped, the channel closes, the task exits and `pump_consumer`
        // is dropped along with it (releasing the consumer).
        let (tx, rx) = mpsc::channel::<Result<Envelope, BackendError>>(64);
        let pump_consumer = Arc::clone(&consumer);
        tokio::spawn(async move {
            let stream = pump_consumer.stream();
            tokio::pin!(stream);
            while let Some(item) = stream.next().await {
                let payload = match item {
                    Ok(borrowed) => Ok(borrowed_to_envelope(&borrowed)),
                    Err(e) => Err(BackendError::Transport {
                        backend: BackendId::Kafka,
                        message: e.to_string(),
                    }),
                };
                if tx.send(payload).await.is_err() {
                    break; // subscriber went away
                }
            }
        });
        // Hold a reference so the consumer outlives this function frame; the
        // spawned task keeps the only other strong ref.
        drop(consumer);

        Ok(Box::pin(ReceiverStream::new(rx)))
    }
}

fn build_headers(envelope: &Envelope) -> OwnedHeaders {
    let mut headers = OwnedHeaders::new();
    for (k, v) in &envelope.headers {
        headers = headers.insert(Header {
            key: k.as_str(),
            value: Some(v.as_str()),
        });
    }
    if let Some(schema) = envelope.schema_id.as_deref() {
        headers = headers.insert(Header {
            key: SCHEMA_ID_HEADER,
            value: Some(schema),
        });
    }
    // Stamp `ol-event-time` if absent so downstream OpenLineage consumers
    // (see `event_bus_data::headers`) always see a usable timestamp without
    // forcing every router caller to populate it explicitly.
    if !envelope
        .headers
        .contains_key(event_bus_data::headers::keys::EVENT_TIME)
    {
        let now = Utc::now().to_rfc3339();
        headers = headers.insert(Header {
            key: event_bus_data::headers::keys::EVENT_TIME,
            value: Some(now.as_str()),
        });
    }
    headers
}

fn borrowed_to_envelope(msg: &rdkafka::message::BorrowedMessage<'_>) -> Envelope {
    let mut headers = BTreeMap::new();
    let mut schema_id = None;
    if let Some(h) = msg.headers() {
        for i in 0..h.count() {
            let header = h.get(i);
            let value = match header.value.and_then(|v| std::str::from_utf8(v).ok()) {
                Some(v) => v.to_string(),
                None => continue,
            };
            if header.key == SCHEMA_ID_HEADER {
                schema_id = Some(value);
            } else {
                headers.insert(header.key.to_string(), value);
            }
        }
    }
    Envelope {
        topic: msg.topic().to_string(),
        payload: Bytes::from(msg.payload().map(|p| p.to_vec()).unwrap_or_default()),
        headers,
        schema_id,
    }
}

/// Translate a NATS-style pattern (`a.b.*`, `a.>`) into something `rdkafka`
/// accepts as a `subscribe(...)` argument. Plain topic names are returned
/// unchanged; patterns containing `*` or `>` are converted to a regex
/// prefixed by `^` (rdkafka's switch for regex subscriptions) and anchored
/// with `$` for safety.
fn translate_pattern(pattern: &str) -> String {
    if !pattern.contains('*') && !pattern.contains('>') {
        return pattern.to_string();
    }
    let mut out = String::with_capacity(pattern.len() + 4);
    out.push('^');
    for ch in pattern.chars() {
        match ch {
            '.' => out.push_str("\\."),
            '*' => out.push_str("[^.]+"),
            '>' => out.push_str(".+"),
            other => out.push(other),
        }
    }
    out.push('$');
    out
}

fn map_kafka_send_error(err: rdkafka::error::KafkaError) -> BackendError {
    use rdkafka::error::{KafkaError, RDKafkaErrorCode};
    match err {
        // Broker returned an explicit, retriable-or-fatal error code.
        KafkaError::MessageProduction(code) => match code {
            RDKafkaErrorCode::TopicAuthorizationFailed
            | RDKafkaErrorCode::ClusterAuthorizationFailed
            | RDKafkaErrorCode::SaslAuthenticationFailed
            | RDKafkaErrorCode::UnknownTopicOrPartition => BackendError::Rejected {
                backend: BackendId::Kafka,
                message: format!("kafka rejected publish: {code:?}"),
            },
            _ => BackendError::Transport {
                backend: BackendId::Kafka,
                message: format!("kafka transport error: {code:?}"),
            },
        },
        other => BackendError::Transport {
            backend: BackendId::Kafka,
            message: other.to_string(),
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn translate_literal_topic_unchanged() {
        assert_eq!(translate_pattern("data.orders"), "data.orders");
    }

    #[test]
    fn translate_single_token_wildcard() {
        assert_eq!(translate_pattern("data.*"), "^data\\.[^.]+$");
    }

    #[test]
    fn translate_multi_token_wildcard() {
        assert_eq!(translate_pattern("data.>"), "^data\\..+$");
    }

    #[test]
    fn build_headers_attaches_schema_id_under_well_known_key() {
        let mut envelope = Envelope {
            topic: "t".into(),
            payload: Bytes::from_static(b""),
            headers: BTreeMap::new(),
            schema_id: Some("schema-42".into()),
        };
        envelope.headers.insert("a".into(), "1".into());
        let headers = build_headers(&envelope);
        // Walk the headers to assert what we encoded.
        let mut found_schema = false;
        let mut found_event_time = false;
        for i in 0..headers.count() {
            let h = headers.get(i);
            if h.key == SCHEMA_ID_HEADER {
                found_schema = true;
                assert_eq!(
                    h.value.and_then(|v| std::str::from_utf8(v).ok()),
                    Some("schema-42")
                );
            }
            if h.key == event_bus_data::headers::keys::EVENT_TIME {
                found_event_time = true;
            }
        }
        assert!(found_schema, "schema id header must be present");
        assert!(found_event_time, "event time header must be auto-stamped");
    }

    #[test]
    fn build_headers_preserves_caller_event_time() {
        let mut envelope = Envelope {
            topic: "t".into(),
            payload: Bytes::from_static(b""),
            headers: BTreeMap::new(),
            schema_id: None,
        };
        envelope.headers.insert(
            event_bus_data::headers::keys::EVENT_TIME.into(),
            "2026-05-01T00:00:00Z".into(),
        );
        let headers = build_headers(&envelope);
        let mut event_times: Vec<String> = Vec::new();
        for i in 0..headers.count() {
            let h = headers.get(i);
            if h.key == event_bus_data::headers::keys::EVENT_TIME {
                if let Some(v) = h.value.and_then(|v| std::str::from_utf8(v).ok()) {
                    event_times.push(v.to_string());
                }
            }
        }
        assert_eq!(event_times, vec!["2026-05-01T00:00:00Z".to_string()]);
    }
}
