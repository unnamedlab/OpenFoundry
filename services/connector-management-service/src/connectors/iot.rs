//! IoT / IIoT connector — productive MQTT 3.1.1 client backed by `rumqttc`.
//!
//! Replaces the previous HTTP-feed shim with a real MQTT pipeline:
//!
//! * `validate_config` requires `broker_host`. `broker_port` defaults to
//!   1883 (8883 when `tls=true`). At least one topic via `topic` or `topics[]`.
//! * `test_connection` opens an MQTT CONNECT to the broker and waits for the
//!   `ConnAck` packet, then disconnects. Errors include broker-side rejects.
//! * `discover_sources` subscribes to the configured topic filters for a brief
//!   discovery window and reports the unique topics observed; if nothing
//!   arrives within the window the configured filters are returned as-is so
//!   the caller still gets a stable catalog.
//! * `fetch_dataset` subscribes to a topic filter and drains messages until
//!   `max_messages` or `max_duration_ms` is reached. Each MQTT publish is
//!   captured as a row (`topic`, `payload`, `qos`, `retained`, `received_at`)
//!   and the result is materialised as Arrow IPC for the
//!   dataset-versioning-service to create a new version.

use std::time::{Duration, Instant};

use chrono::Utc;
use rumqttc::{AsyncClient, Event, Incoming, MqttOptions, QoS, Transport};
use serde_json::{Value, json};
use tokio::time::timeout;

use super::{
    ConnectionTestResult, SyncPayload, arrow_payload_from_rows, basic_discovered_source,
    virtual_table_response,
};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

const SOURCE_KIND: &str = "mqtt_topic";
const DEFAULT_PORT: u16 = 1883;
const DEFAULT_TLS_PORT: u16 = 8883;
const DEFAULT_KEEPALIVE_SECS: u64 = 30;
const DEFAULT_CONNECT_TIMEOUT_MS: u64 = 5_000;
const DEFAULT_DISCOVERY_WINDOW_MS: u64 = 2_000;
const DEFAULT_MAX_MESSAGES: usize = 1_000;
const DEFAULT_FETCH_WINDOW_MS: u64 = 5_000;
const DEFAULT_CHANNEL_CAPACITY: usize = 32;

pub fn validate_config(config: &Value) -> Result<(), String> {
    if config
        .get("broker_host")
        .and_then(Value::as_str)
        .map(str::trim)
        .unwrap_or("")
        .is_empty()
    {
        return Err("iot connector requires 'broker_host'".to_string());
    }
    if topic_filters(config).is_empty() {
        return Err("iot connector requires 'topic' or non-empty 'topics'".to_string());
    }
    Ok(())
}

pub async fn test_connection(
    _state: &AppState,
    config: &Value,
    _agent_url: Option<&str>,
) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;
    let started = Instant::now();
    let (client, mut eventloop) = build_client(config, "test")?;
    let connect_timeout = Duration::from_millis(connect_timeout_ms(config));

    let outcome = timeout(connect_timeout, async {
        loop {
            match eventloop.poll().await {
                Ok(Event::Incoming(Incoming::ConnAck(connack))) => {
                    return Ok::<_, String>(format!("{:?}", connack.code));
                }
                Ok(_) => continue,
                Err(error) => return Err(format!("mqtt connect error: {error}")),
            }
        }
    })
    .await
    .map_err(|_| {
        format!(
            "mqtt CONNECT timed out after {} ms",
            connect_timeout.as_millis()
        )
    })??;

    let _ = client.disconnect().await;
    let latency_ms = started.elapsed().as_millis();
    Ok(ConnectionTestResult {
        success: true,
        message: format!("MQTT broker reachable ({outcome})"),
        latency_ms,
        details: Some(json!({
            "broker_host": config.get("broker_host").cloned().unwrap_or(Value::Null),
            "broker_port": resolved_port(config),
            "tls": tls_enabled(config),
            "client_id": client_id(config, "test"),
        })),
    })
}

pub async fn discover_sources(
    _state: &AppState,
    config: &Value,
    _agent_url: Option<&str>,
) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    let filters = topic_filters(config);
    let mut observed = std::collections::BTreeSet::new();

    if let Ok(messages) = drain_messages(
        config,
        &filters,
        usize::MAX,
        discovery_window_ms(config),
        "discover",
    )
    .await
    {
        for message in messages {
            if let Some(topic) = message.get("topic").and_then(Value::as_str) {
                observed.insert(topic.to_string());
            }
        }
    }

    if observed.is_empty() {
        return Ok(filters
            .into_iter()
            .map(|filter| {
                basic_discovered_source(
                    filter.clone(),
                    filter.clone(),
                    SOURCE_KIND,
                    json!({ "filter": filter, "observed": false }),
                )
            })
            .collect());
    }

    Ok(observed
        .into_iter()
        .map(|topic| {
            basic_discovered_source(
                topic.clone(),
                topic.clone(),
                SOURCE_KIND,
                json!({ "topic": topic, "observed": true }),
            )
        })
        .collect())
}

pub async fn fetch_dataset(
    _state: &AppState,
    config: &Value,
    selector: &str,
    _agent_url: Option<&str>,
) -> Result<SyncPayload, String> {
    validate_config(config)?;
    let filters = if selector.trim().is_empty() {
        topic_filters(config)
    } else {
        vec![selector.trim().to_string()]
    };
    let max = max_messages(config);
    let window = fetch_window_ms(config);
    let messages = drain_messages(config, &filters, max, window, "sync").await?;

    let columns = vec![
        "topic".to_string(),
        "payload".to_string(),
        "qos".to_string(),
        "retained".to_string(),
        "received_at".to_string(),
    ];
    let metadata = json!({
        "selector": selector,
        "filters": filters,
        "broker_host": config.get("broker_host").cloned().unwrap_or(Value::Null),
        "broker_port": resolved_port(config),
        "tls": tls_enabled(config),
        "messages": messages.len(),
        "window_ms": window,
    });
    arrow_payload_from_rows(
        format!("mqtt_{}.arrow", sanitize_file_name(selector)),
        columns,
        messages,
        metadata,
    )
}

pub async fn query_virtual_table(
    state: &AppState,
    config: &Value,
    request: &VirtualTableQueryRequest,
    agent_url: Option<&str>,
) -> Result<VirtualTableQueryResponse, String> {
    let mut bounded = config.clone();
    if let Some(object) = bounded.as_object_mut() {
        let limit = request.limit.unwrap_or(50).clamp(1, 500) as i64;
        object.insert("max_messages".to_string(), Value::from(limit));
        object.insert("max_duration_ms".to_string(), Value::from(2_000));
    }
    let payload = fetch_dataset(state, &bounded, &request.selector, agent_url).await?;
    let filters = if request.selector.trim().is_empty() {
        topic_filters(&bounded)
    } else {
        vec![request.selector.trim().to_string()]
    };
    let limit = request.limit.unwrap_or(50).clamp(1, 500);
    let rows = drain_messages(&bounded, &filters, limit, 1_500, "preview")
        .await
        .unwrap_or_default();
    Ok(virtual_table_response(
        &request.selector,
        rows,
        payload.metadata,
    ))
}

async fn drain_messages(
    config: &Value,
    filters: &[String],
    max_messages: usize,
    window_ms: u64,
    purpose: &str,
) -> Result<Vec<Value>, String> {
    if filters.is_empty() {
        return Ok(Vec::new());
    }
    let (client, mut eventloop) = build_client(config, purpose)?;
    let qos = subscription_qos(config);

    // Wait for ConnAck before issuing subscribes.
    let connect_deadline = Duration::from_millis(connect_timeout_ms(config));
    timeout(connect_deadline, async {
        loop {
            match eventloop.poll().await {
                Ok(Event::Incoming(Incoming::ConnAck(_))) => return Ok::<(), String>(()),
                Ok(_) => continue,
                Err(error) => return Err(format!("mqtt connect error: {error}")),
            }
        }
    })
    .await
    .map_err(|_| "mqtt CONNECT timed out".to_string())??;

    for filter in filters {
        client
            .subscribe(filter.clone(), qos)
            .await
            .map_err(|error| format!("mqtt subscribe '{filter}' failed: {error}"))?;
    }

    let mut messages = Vec::new();
    let drain_deadline = Duration::from_millis(window_ms);
    let started = Instant::now();
    while messages.len() < max_messages {
        let remaining = drain_deadline.saturating_sub(started.elapsed());
        if remaining.is_zero() {
            break;
        }
        match timeout(remaining, eventloop.poll()).await {
            Ok(Ok(Event::Incoming(Incoming::Publish(publish)))) => {
                let payload_bytes = publish.payload.to_vec();
                let payload_value = serde_json::from_slice::<Value>(&payload_bytes)
                    .unwrap_or_else(|_| Value::String(String::from_utf8_lossy(&payload_bytes).to_string()));
                messages.push(json!({
                    "topic": publish.topic,
                    "payload": payload_value,
                    "qos": format!("{:?}", publish.qos),
                    "retained": publish.retain,
                    "received_at": Utc::now().to_rfc3339(),
                }));
            }
            Ok(Ok(_)) => continue,
            Ok(Err(error)) => return Err(format!("mqtt poll error: {error}")),
            Err(_) => break, // window elapsed
        }
    }

    let _ = client.disconnect().await;
    Ok(messages)
}

fn build_client(config: &Value, purpose: &str) -> Result<(AsyncClient, rumqttc::EventLoop), String> {
    let host = config
        .get("broker_host")
        .and_then(Value::as_str)
        .ok_or_else(|| "iot connector requires 'broker_host'".to_string())?
        .to_string();
    let port = resolved_port(config);
    let id = client_id(config, purpose);
    let mut options = MqttOptions::new(id, host, port);
    options.set_keep_alive(Duration::from_secs(keep_alive_secs(config)));
    options.set_clean_session(true);

    if let (Some(user), Some(pass)) = (
        config.get("username").and_then(Value::as_str),
        config.get("password").and_then(Value::as_str),
    ) {
        options.set_credentials(user, pass);
    }
    if tls_enabled(config) {
        options.set_transport(Transport::Tls(rumqttc::TlsConfiguration::default()));
    }
    Ok(AsyncClient::new(options, DEFAULT_CHANNEL_CAPACITY))
}

fn topic_filters(config: &Value) -> Vec<String> {
    if let Some(topics) = config.get("topics").and_then(Value::as_array) {
        let collected: Vec<String> = topics
            .iter()
            .filter_map(|value| value.as_str().map(str::to_string))
            .filter(|value| !value.trim().is_empty())
            .collect();
        if !collected.is_empty() {
            return collected;
        }
    }
    config
        .get("topic")
        .and_then(Value::as_str)
        .map(|value| vec![value.to_string()])
        .unwrap_or_default()
}

fn tls_enabled(config: &Value) -> bool {
    config
        .get("tls")
        .and_then(Value::as_bool)
        .unwrap_or(false)
}

fn resolved_port(config: &Value) -> u16 {
    config
        .get("broker_port")
        .and_then(Value::as_u64)
        .map(|port| port as u16)
        .unwrap_or_else(|| {
            if tls_enabled(config) {
                DEFAULT_TLS_PORT
            } else {
                DEFAULT_PORT
            }
        })
}

fn client_id(config: &Value, purpose: &str) -> String {
    config
        .get("client_id")
        .and_then(Value::as_str)
        .map(str::to_string)
        .unwrap_or_else(|| format!("openfoundry-{purpose}-{}", Utc::now().timestamp_millis()))
}

fn keep_alive_secs(config: &Value) -> u64 {
    config
        .get("keep_alive_secs")
        .and_then(Value::as_u64)
        .unwrap_or(DEFAULT_KEEPALIVE_SECS)
        .clamp(5, 600)
}

fn connect_timeout_ms(config: &Value) -> u64 {
    config
        .get("connect_timeout_ms")
        .and_then(Value::as_u64)
        .unwrap_or(DEFAULT_CONNECT_TIMEOUT_MS)
        .clamp(500, 60_000)
}

fn discovery_window_ms(config: &Value) -> u64 {
    config
        .get("discovery_window_ms")
        .and_then(Value::as_u64)
        .unwrap_or(DEFAULT_DISCOVERY_WINDOW_MS)
        .clamp(100, 30_000)
}

fn fetch_window_ms(config: &Value) -> u64 {
    config
        .get("max_duration_ms")
        .and_then(Value::as_u64)
        .unwrap_or(DEFAULT_FETCH_WINDOW_MS)
        .clamp(100, 600_000)
}

fn max_messages(config: &Value) -> usize {
    config
        .get("max_messages")
        .and_then(Value::as_u64)
        .unwrap_or(DEFAULT_MAX_MESSAGES as u64)
        .clamp(1, 1_000_000) as usize
}

fn subscription_qos(config: &Value) -> QoS {
    match config.get("qos").and_then(Value::as_u64).unwrap_or(0) {
        0 => QoS::AtMostOnce,
        1 => QoS::AtLeastOnce,
        _ => QoS::ExactlyOnce,
    }
}

fn sanitize_file_name(selector: &str) -> String {
    selector
        .chars()
        .map(|c| if c.is_ascii_alphanumeric() { c } else { '_' })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn requires_broker_and_topic() {
        assert!(validate_config(&json!({})).is_err());
        assert!(
            validate_config(&json!({ "broker_host": "broker" })).is_err()
        );
        assert!(
            validate_config(&json!({ "broker_host": "broker", "topic": "sensors/#" })).is_ok()
        );
        assert!(
            validate_config(&json!({
                "broker_host": "broker",
                "topics": ["a/#", "b/+"]
            }))
            .is_ok()
        );
    }

    #[test]
    fn port_defaults_consider_tls() {
        assert_eq!(
            resolved_port(&json!({ "broker_host": "h", "topic": "t" })),
            1883
        );
        assert_eq!(
            resolved_port(&json!({ "broker_host": "h", "topic": "t", "tls": true })),
            8883
        );
        assert_eq!(
            resolved_port(&json!({ "broker_host": "h", "topic": "t", "broker_port": 9001 })),
            9001
        );
    }

    #[test]
    fn topic_filters_prefer_array() {
        let filters = topic_filters(&json!({
            "topic": "ignored",
            "topics": ["a", "b"]
        }));
        assert_eq!(filters, vec!["a".to_string(), "b".to_string()]);
    }

    #[test]
    fn qos_levels_map_correctly() {
        assert!(matches!(subscription_qos(&json!({})), QoS::AtMostOnce));
        assert!(matches!(
            subscription_qos(&json!({ "qos": 1 })),
            QoS::AtLeastOnce
        ));
        assert!(matches!(
            subscription_qos(&json!({ "qos": 2 })),
            QoS::ExactlyOnce
        ));
    }

    #[test]
    fn fetch_window_is_clamped() {
        assert_eq!(fetch_window_ms(&json!({ "max_duration_ms": 0 })), 100);
        assert_eq!(
            fetch_window_ms(&json!({ "max_duration_ms": 10_000 })),
            10_000
        );
    }
}
