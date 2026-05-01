use serde_json::{Value, json};

use crate::schema_registry::{self, SchemaType};

#[derive(Debug, Clone)]
pub struct EventStreamTopic {
    pub selector: String,
    pub display_name: String,
    pub sample_messages: Vec<Value>,
    pub partitions: i64,
    pub metadata: Value,
}

pub fn validate_topic_connector_config(
    config: &Value,
    connector_label: &str,
) -> Result<(), String> {
    if bootstrap_servers(config).is_none() {
        return Err(format!(
            "{connector_label} requires 'bootstrap_servers' or 'brokers'"
        ));
    }

    let topics = parse_topic_entries(config, connector_label)?;
    if topics.is_empty() {
        return Err(format!(
            "{connector_label} requires at least one topic in 'topics'"
        ));
    }

    // If a topic ships an inline schema (Avro / JSON Schema / Protobuf
    // descriptor set), validate the configured `sample_messages` against
    // it. This catches schema/payload drift at registration time instead
    // of at first read.
    for topic in &topics {
        validate_topic_samples(&topic.metadata, &topic.sample_messages, connector_label)?;
    }

    Ok(())
}

pub fn parse_topic_entries(
    config: &Value,
    connector_label: &str,
) -> Result<Vec<EventStreamTopic>, String> {
    let topics = config
        .get("topics")
        .and_then(Value::as_array)
        .ok_or_else(|| format!("{connector_label} requires 'topics' to be an array"))?;

    let mut parsed = Vec::with_capacity(topics.len());
    for (index, topic) in topics.iter().enumerate() {
        if let Some(selector) = topic
            .as_str()
            .map(str::trim)
            .filter(|value| !value.is_empty())
        {
            parsed.push(EventStreamTopic {
                selector: selector.to_string(),
                display_name: selector.to_string(),
                sample_messages: Vec::new(),
                partitions: 1,
                metadata: json!({ "topic": selector }),
            });
            continue;
        }

        let selector = topic
            .get("selector")
            .or_else(|| topic.get("topic"))
            .or_else(|| topic.get("name"))
            .and_then(Value::as_str)
            .filter(|value| !value.trim().is_empty())
            .ok_or_else(|| {
                format!("{connector_label} topics[{index}] requires 'selector', 'topic' or 'name'")
            })?
            .to_string();
        let display_name = topic
            .get("display_name")
            .or_else(|| topic.get("name"))
            .and_then(Value::as_str)
            .unwrap_or(&selector)
            .to_string();
        let sample_messages = topic
            .get("sample_messages")
            .or_else(|| topic.get("preview_rows"))
            .and_then(Value::as_array)
            .cloned()
            .unwrap_or_default();
        let partitions = topic
            .get("partitions")
            .and_then(Value::as_i64)
            .unwrap_or(1)
            .max(1);
        let mut metadata = topic.clone();
        if let Some(object) = metadata.as_object_mut() {
            object.remove("sample_messages");
            object.remove("preview_rows");
        }

        parsed.push(EventStreamTopic {
            selector,
            display_name,
            sample_messages,
            partitions,
            metadata,
        });
    }

    Ok(parsed)
}

pub fn find_topic_entry(
    config: &Value,
    selector: &str,
    connector_label: &str,
) -> Result<EventStreamTopic, String> {
    parse_topic_entries(config, connector_label)?
        .into_iter()
        .find(|topic| topic.selector == selector)
        .ok_or_else(|| format!("{connector_label} topic '{selector}' is not configured"))
}

/// Validate a topic's `sample_messages` against an inline schema declared
/// in the topic config. The expected shape is:
///
/// ```json
/// {
///   "selector": "orders",
///   "schema": { "type": "avro" | "protobuf" | "json", "text": "..." },
///   "sample_messages": [ { ... } ]
/// }
/// ```
///
/// `schema_subject` (a reference to a registered subject in the
/// cdc-metadata-service Schema Registry) is recognised but only used by
/// the connector for traceability — actual validation always runs against
/// the inline `schema.text` when present. This keeps the connector usable
/// in environments where the registry is offline (catalog_backed mode).
///
/// Returns `Ok(())` if no schema is configured (validation is opt-in).
pub fn validate_topic_samples(
    topic_metadata: &Value,
    sample_messages: &[Value],
    connector_label: &str,
) -> Result<(), String> {
    let Some(schema) = topic_metadata.get("schema").filter(|value| !value.is_null()) else {
        return Ok(());
    };
    let schema_type_str = schema
        .get("type")
        .and_then(Value::as_str)
        .ok_or_else(|| format!("{connector_label} schema requires 'type'"))?;
    let schema_type: SchemaType = schema_type_str
        .parse()
        .map_err(|error: schema_registry::SchemaError| {
            format!("{connector_label} {error}")
        })?;
    let schema_text = schema
        .get("text")
        .and_then(Value::as_str)
        .ok_or_else(|| format!("{connector_label} schema requires 'text'"))?;
    for (index, sample) in sample_messages.iter().enumerate() {
        schema_registry::validate_payload(schema_type, schema_text, sample).map_err(|error| {
            format!(
                "{connector_label} sample_messages[{index}] does not match schema: {error}"
            )
        })?;
    }
    Ok(())
}

pub fn bootstrap_servers(config: &Value) -> Option<&str> {
    string_field(config, "bootstrap_servers").or_else(|| string_field(config, "brokers"))
}

pub fn sanitize_file_stem(selector: &str, fallback: &str) -> String {
    let stem = selector
        .chars()
        .map(|ch| if ch.is_ascii_alphanumeric() { ch } else { '_' })
        .collect::<String>()
        .trim_matches('_')
        .to_string();
    if stem.is_empty() {
        fallback.to_string()
    } else {
        stem.chars().take(64).collect()
    }
}

fn string_field<'a>(config: &'a Value, field: &str) -> Option<&'a str> {
    config
        .get(field)
        .and_then(Value::as_str)
        .filter(|value| !value.trim().is_empty())
}

#[cfg(test)]
mod tests {
    use serde_json::json;

    use super::{
        bootstrap_servers, find_topic_entry, parse_topic_entries, sanitize_file_stem,
        validate_topic_connector_config,
    };

    #[test]
    fn parses_string_and_object_topics() {
        let topics = parse_topic_entries(
            &json!({
                "topics": [
                    "orders",
                    {
                        "selector": "payments",
                        "display_name": "Payments",
                        "partitions": 6,
                        "sample_messages": [{ "payment_id": "pay-1" }]
                    }
                ]
            }),
            "kafka connector",
        )
        .expect("topics should parse");

        assert_eq!(topics.len(), 2);
        assert_eq!(topics[0].selector, "orders");
        assert_eq!(topics[1].display_name, "Payments");
        assert_eq!(topics[1].partitions, 6);
    }

    #[test]
    fn validates_required_bootstrap_servers() {
        let error =
            validate_topic_connector_config(&json!({ "topics": ["orders"] }), "kafka connector")
                .expect_err("validation should fail");
        assert!(error.contains("bootstrap_servers"));
    }

    #[test]
    fn finds_configured_topic() {
        let topic = find_topic_entry(
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
            "kafka connector",
        )
        .expect("topic should exist");

        assert_eq!(topic.selector, "orders");
        assert_eq!(topic.sample_messages, vec![json!({ "order_id": "ord-1" })]);
        assert_eq!(
            bootstrap_servers(&json!({ "brokers": "broker-a:9092" })),
            Some("broker-a:9092")
        );
    }

    #[test]
    fn sanitizes_file_stems() {
        assert_eq!(sanitize_file_stem("orders.v1", "fallback"), "orders_v1");
        assert_eq!(sanitize_file_stem("///", "fallback"), "fallback");
    }

    #[test]
    fn topic_with_inline_avro_schema_validates_samples() {
        let config = json!({
            "bootstrap_servers": "broker-a:9092",
            "topics": [{
                "selector": "orders",
                "schema": {
                    "type": "avro",
                    "text": r#"{"type":"record","name":"Order","fields":[{"name":"order_id","type":"string"}]}"#
                },
                "sample_messages": [{ "order_id": "ord-1" }]
            }]
        });
        validate_topic_connector_config(&config, "kafka connector").expect("samples match schema");
    }

    #[test]
    fn topic_with_inline_schema_rejects_invalid_sample() {
        let config = json!({
            "bootstrap_servers": "broker-a:9092",
            "topics": [{
                "selector": "orders",
                "schema": {
                    "type": "json",
                    "text": r#"{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}}}"#
                },
                "sample_messages": [{ "order_id": "ord-1" }, { "wrong_field": 1 }]
            }]
        });
        let error = validate_topic_connector_config(&config, "kafka connector")
            .expect_err("second sample is missing required field");
        assert!(error.contains("sample_messages[1]"));
    }
}
