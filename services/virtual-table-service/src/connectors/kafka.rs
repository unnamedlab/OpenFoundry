use event_bus_control::connectors::{
    EventStreamTopic, bootstrap_servers, find_topic_entry, parse_topic_entries, sanitize_file_stem,
    validate_topic_connector_config,
};
use serde_json::{Value, json};

use super::{
    ConnectionTestResult, SyncPayload, add_source_signature, basic_discovered_source,
    virtual_table_response,
};
use crate::{
    AppState,
    models::registration::{DiscoveredSource, VirtualTableQueryRequest, VirtualTableQueryResponse},
};

type TopicEntry = EventStreamTopic;

pub fn validate_config(config: &Value) -> Result<(), String> {
    validate_topic_connector_config(config, "kafka connector")
}

pub async fn test_connection(
    _state: &AppState,
    config: &Value,
    _agent_url: Option<&str>,
) -> Result<ConnectionTestResult, String> {
    validate_config(config)?;
    let topics = parse_topics(config)?;
    Ok(ConnectionTestResult {
        success: true,
        message: format!("validated kafka catalog with {} topic(s)", topics.len()),
        latency_ms: 0,
        details: Some(json!({
            "bootstrap_servers": bootstrap_servers(config),
            "topic_count": topics.len(),
            "mode": "catalog_backed",
        })),
    })
}

pub async fn discover_sources(
    _state: &AppState,
    config: &Value,
    _agent_url: Option<&str>,
) -> Result<Vec<DiscoveredSource>, String> {
    validate_config(config)?;
    Ok(parse_topics(config)?
        .into_iter()
        .map(|topic| {
            basic_discovered_source(
                topic.selector,
                topic.display_name,
                "kafka_topic",
                topic.metadata,
            )
        })
        .collect())
}

pub async fn query_virtual_table(
    _state: &AppState,
    config: &Value,
    request: &VirtualTableQueryRequest,
    _agent_url: Option<&str>,
) -> Result<VirtualTableQueryResponse, String> {
    validate_config(config)?;
    let topic = find_topic(config, &request.selector)?;
    let rows = topic
        .sample_messages
        .iter()
        .take(request.limit.unwrap_or(50).clamp(1, 500))
        .cloned()
        .collect::<Vec<_>>();

    Ok(virtual_table_response(
        &request.selector,
        rows,
        json!({
            "bootstrap_servers": bootstrap_servers(config),
            "partitions": topic.partitions,
            "entry": topic.metadata,
        }),
    ))
}

pub async fn fetch_dataset(
    _state: &AppState,
    config: &Value,
    selector: &str,
    _agent_url: Option<&str>,
) -> Result<SyncPayload, String> {
    validate_config(config)?;
    let topic = find_topic(config, selector)?;
    let rows_synced = topic.sample_messages.len() as i64;
    let mut payload = SyncPayload {
        bytes: serde_json::to_vec(&topic.sample_messages).map_err(|error| error.to_string())?,
        format: "json".to_string(),
        rows_synced,
        file_name: format!("{}.json", sanitize_file_stem(selector, "kafka_sync")),
        metadata: json!({
            "bootstrap_servers": bootstrap_servers(config),
            "topic": selector,
            "partitions": topic.partitions,
            "entry": topic.metadata,
        }),
    };
    add_source_signature(&mut payload);
    Ok(payload)
}

fn find_topic(config: &Value, selector: &str) -> Result<TopicEntry, String> {
    find_topic_entry(config, selector, "kafka connector")
}

fn parse_topics(config: &Value) -> Result<Vec<TopicEntry>, String> {
    parse_topic_entries(config, "kafka connector")
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::{find_topic, parse_topics, validate_config};

    #[test]
    fn parses_string_and_object_topics() {
        let topics = parse_topics(&json!({
            "topics": [
                "orders",
                {
                    "selector": "payments",
                    "display_name": "Payments",
                    "partitions": 6,
                    "sample_messages": [{ "payment_id": "pay-1" }]
                }
            ]
        }))
        .expect("topics should parse");

        assert_eq!(topics.len(), 2);
        assert_eq!(topics[0].selector, "orders");
        assert_eq!(topics[1].display_name, "Payments");
        assert_eq!(topics[1].partitions, 6);
    }

    #[test]
    fn validates_required_bootstrap_servers() {
        let error =
            validate_config(&json!({ "topics": ["orders"] })).expect_err("validation should fail");
        assert!(error.contains("bootstrap_servers"));
    }

    #[test]
    fn finds_configured_topic() {
        let topic = find_topic(
            &json!({
                "bootstrap_servers": "broker-a:9092",
                "topics": [
                    {
                        "selector": "orders",
                        "sample_messages": [{ "order_id": "ord-1" }]
                    }
                ]
            }),
            "orders",
        )
        .expect("topic should exist");

        assert_eq!(topic.selector, "orders");
        assert_eq!(topic.sample_messages, vec![json!({ "order_id": "ord-1" })]);
    }
}
