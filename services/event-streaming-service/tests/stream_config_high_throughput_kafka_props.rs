//! Stream-config Kafka producer tuning — verifies that the
//! [`KafkaProducerSettings`] derived from the Foundry stream type ends
//! up on the `rdkafka` `ClientConfig` used by the hot buffer.
//!
//! The test is gated by `kafka-rdkafka` because the assertions inspect
//! `rdkafka::ClientConfig` values that are only built when the feature
//! is on. No broker is required — we only build the config and check
//! the values.

#![cfg(feature = "kafka-rdkafka")]

use event_bus_data::config::{DataBusConfig, ServicePrincipal};
use event_streaming_service::models::stream::{KafkaProducerSettings, StreamType};

#[test]
fn high_throughput_overrides_linger_and_batch_size() {
    let settings = KafkaProducerSettings::for_stream(StreamType::HighThroughput, false);
    assert_eq!(settings.linger_ms, 20);
    assert_eq!(settings.batch_size_bytes, 131_072);
    assert_eq!(settings.compression, None);
}

#[test]
fn compressed_emits_lz4_codec() {
    let settings = KafkaProducerSettings::for_stream(StreamType::Compressed, false);
    assert_eq!(settings.compression, Some("lz4"));
    assert_eq!(settings.linger_ms, 5);
}

#[test]
fn high_throughput_compressed_combines_both() {
    let settings = KafkaProducerSettings::for_stream(StreamType::HighThroughputCompressed, false);
    assert_eq!(settings.linger_ms, 20);
    assert_eq!(settings.batch_size_bytes, 131_072);
    assert_eq!(settings.compression, Some("lz4"));
}

#[test]
fn explicit_compression_flag_forces_lz4_even_on_standard() {
    let settings = KafkaProducerSettings::for_stream(StreamType::Standard, true);
    assert_eq!(settings.compression, Some("lz4"));
    // Standard latency profile is preserved.
    assert_eq!(settings.linger_ms, 5);
    assert_eq!(settings.batch_size_bytes, 32_768);
}

#[test]
fn settings_are_applied_to_rdkafka_client_config() {
    let cfg = DataBusConfig::new(
        "127.0.0.1:9092".to_string(),
        ServicePrincipal::insecure_dev("test"),
    );
    let mut client_config = cfg.producer_config();
    let settings = KafkaProducerSettings::for_stream(StreamType::HighThroughputCompressed, false);
    for (key, value) in settings.to_kafka_pairs() {
        client_config.set(key, &value);
    }
    assert_eq!(client_config.get("linger.ms"), Some("20"));
    assert_eq!(client_config.get("batch.size"), Some("131072"));
    assert_eq!(client_config.get("compression.type"), Some("lz4"));
}
