//! Live Kafka I/O helpers for the data-connection plane.
//!
//! Compiled only when the `kafka-rdkafka` feature is enabled, so default
//! workspace builds (and CI) stay free of the native `librdkafka`
//! dependency. The three helpers exposed here back the real
//! `test_connection` / `discover_sources` / `query_virtual_table` paths in
//! the per-service `connectors::kafka` modules.
//!
//! Implementation notes:
//! - We use `BaseConsumer` wrapped in `tokio::task::spawn_blocking` because
//!   `fetch_metadata`, `fetch_watermarks`, `assign` and `poll` are
//!   synchronous C calls into librdkafka. Spawning them on the blocking
//!   pool keeps the async runtime responsive without giving up the simple
//!   request/response shape that the connector trait expects.
//! - All three helpers honour an inbound timeout (default 5 s) and return
//!   `Result<_, String>` so they slot directly into the existing connector
//!   error contract (`String` errors bubble up to the HTTP layer as
//!   `502 Bad Gateway`-style messages).

use std::time::{Duration, Instant};

use rdkafka::ClientConfig;
use rdkafka::Message;
use rdkafka::consumer::{BaseConsumer, Consumer};
use rdkafka::topic_partition_list::{Offset, TopicPartitionList};
use rdkafka::util::Timeout;
use serde_json::{Value, json};

/// Default timeout for metadata / watermark calls.
pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(5);

/// Outcome of a successful broker probe.
#[derive(Debug, Clone)]
pub struct LiveTestOutcome {
    pub topic_count: usize,
    pub broker_count: usize,
    pub originating_broker: String,
    pub latency_ms: u128,
}

/// A single topic returned by [`discover_topics`].
#[derive(Debug, Clone)]
pub struct DiscoveredKafkaTopic {
    pub name: String,
    pub partitions: i64,
}

/// Probe the broker, returning cluster-level metadata.
///
/// This is intentionally read-only: we instantiate a `BaseConsumer` (no
/// `group.id` required for metadata) and ask for the cluster description.
/// Any librdkafka error is mapped to a human-readable string.
pub async fn test_connection(
    bootstrap: &str,
    timeout: Duration,
) -> Result<LiveTestOutcome, String> {
    let bootstrap = bootstrap.to_string();
    tokio::task::spawn_blocking(move || {
        let started = Instant::now();
        let consumer: BaseConsumer = ClientConfig::new()
            .set("bootstrap.servers", &bootstrap)
            .set("client.id", "openfoundry-connector-test")
            .set(
                "socket.timeout.ms",
                (timeout.as_millis().min(60_000) as u64).to_string(),
            )
            .create()
            .map_err(|error| format!("kafka client init failed: {error}"))?;
        let metadata = consumer
            .fetch_metadata(None, Timeout::After(timeout))
            .map_err(|error| format!("kafka metadata fetch failed: {error}"))?;
        Ok::<_, String>(LiveTestOutcome {
            topic_count: metadata.topics().len(),
            broker_count: metadata.brokers().len(),
            originating_broker: metadata.orig_broker_name().to_string(),
            latency_ms: started.elapsed().as_millis(),
        })
    })
    .await
    .map_err(|error| format!("kafka probe task join failed: {error}"))?
}

/// Enumerate topics on the broker. Internal/system topics (those whose name
/// starts with `__`, e.g. `__consumer_offsets`) are filtered out.
pub async fn discover_topics(
    bootstrap: &str,
    timeout: Duration,
) -> Result<Vec<DiscoveredKafkaTopic>, String> {
    let bootstrap = bootstrap.to_string();
    tokio::task::spawn_blocking(move || {
        let consumer: BaseConsumer = ClientConfig::new()
            .set("bootstrap.servers", &bootstrap)
            .set("client.id", "openfoundry-connector-discover")
            .create()
            .map_err(|error| format!("kafka client init failed: {error}"))?;
        let metadata = consumer
            .fetch_metadata(None, Timeout::After(timeout))
            .map_err(|error| format!("kafka metadata fetch failed: {error}"))?;
        Ok(metadata
            .topics()
            .iter()
            .filter(|topic| !topic.name().starts_with("__"))
            .map(|topic| DiscoveredKafkaTopic {
                name: topic.name().to_string(),
                partitions: topic.partitions().len() as i64,
            })
            .collect())
    })
    .await
    .map_err(|error| format!("kafka discover task join failed: {error}"))?
}

/// Tail the last `limit` messages on partition 0 of `topic`.
///
/// We look up the high watermark, seek to `max(low, high - limit)`, and
/// poll until either `limit` rows are gathered or `timeout` elapses.
/// Payloads are decoded as JSON when possible, otherwise wrapped as
/// `{ "value_utf8": "..." }`.
pub async fn tail_messages(
    bootstrap: &str,
    topic: &str,
    limit: usize,
    timeout: Duration,
) -> Result<Vec<Value>, String> {
    let bootstrap = bootstrap.to_string();
    let topic = topic.to_string();
    let limit = limit.max(1);
    tokio::task::spawn_blocking(move || {
        let group_id = format!("openfoundry-tail-{}", uuid::Uuid::now_v7());
        let consumer: BaseConsumer = ClientConfig::new()
            .set("bootstrap.servers", &bootstrap)
            .set("group.id", &group_id)
            .set("enable.auto.commit", "false")
            .set("auto.offset.reset", "latest")
            .set("session.timeout.ms", "10000")
            .create()
            .map_err(|error| format!("kafka client init failed: {error}"))?;

        let (low, high) = consumer
            .fetch_watermarks(&topic, 0, Timeout::After(timeout))
            .map_err(|error| format!("kafka watermark fetch failed: {error}"))?;
        if high <= low {
            return Ok(Vec::new());
        }
        let start = (high.saturating_sub(limit as i64)).max(low);

        let mut tpl = TopicPartitionList::new();
        tpl.add_partition_offset(&topic, 0, Offset::Offset(start))
            .map_err(|error| format!("kafka offset assignment failed: {error}"))?;
        consumer
            .assign(&tpl)
            .map_err(|error| format!("kafka assign failed: {error}"))?;

        let deadline = Instant::now() + timeout;
        let mut rows: Vec<Value> = Vec::with_capacity(limit);
        while rows.len() < limit && Instant::now() < deadline {
            match consumer.poll(Duration::from_millis(200)) {
                Some(Ok(message)) => rows.push(decode_message(&message)),
                Some(Err(error)) => return Err(format!("kafka poll error: {error}")),
                None => continue,
            }
        }
        Ok(rows)
    })
    .await
    .map_err(|error| format!("kafka tail task join failed: {error}"))?
}

fn decode_message<M: Message>(message: &M) -> Value {
    let key = message
        .key()
        .map(|bytes| String::from_utf8_lossy(bytes).into_owned());
    let payload = match message.payload() {
        None => Value::Null,
        Some(bytes) => match serde_json::from_slice::<Value>(bytes) {
            Ok(value) => value,
            Err(_) => json!({ "value_utf8": String::from_utf8_lossy(bytes).into_owned() }),
        },
    };
    json!({
        "key": key,
        "value": payload,
        "partition": message.partition(),
        "offset": message.offset(),
    })
}
