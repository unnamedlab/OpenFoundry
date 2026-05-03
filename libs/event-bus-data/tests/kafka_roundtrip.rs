//! Integration tests against an ephemeral Kafka broker.
//!
//! Run with: `cargo test -p event-bus-data --features it --test kafka_roundtrip`
//! Requires Docker on the host.

#![cfg(feature = "it")]

use std::time::Duration;

use event_bus_data::testkit::EphemeralKafka;
use event_bus_data::{
    DataPublisher, DataSubscriber, KafkaPublisher, KafkaSubscriber, OpenLineageHeaders,
};

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn publish_and_consume_roundtrip_with_explicit_commit() {
    let kafka = EphemeralKafka::start()
        .await
        .expect("start kafka container");
    let topic = "of.test.orders.v1";
    kafka.create_topic(topic, 1).await.expect("create topic");

    let producer_cfg = kafka.config_for("orders-producer");
    let consumer_cfg = kafka.config_for("orders-consumer");

    let publisher = KafkaPublisher::new(&producer_cfg).expect("build publisher");
    let subscriber = KafkaSubscriber::new(&consumer_cfg, "orders-cg-it").expect("build subscriber");
    subscriber.subscribe(&[topic]).expect("subscribe");

    let lineage = OpenLineageHeaders::new(
        "of://datasets",
        "etl.orders.publish_test",
        "run-it-001",
        "https://github.com/unnamedlab/OpenFoundry",
    )
    .with_schema_url("https://schemas.openfoundry.dev/orders/v1");

    publisher
        .publish(
            topic,
            Some(b"order-1"),
            b"{\"order_id\":\"order-1\"}",
            &lineage,
        )
        .await
        .expect("publish");
    publisher
        .flush(Duration::from_secs(10))
        .await
        .expect("flush");

    let msg = tokio::time::timeout(Duration::from_secs(20), subscriber.recv())
        .await
        .expect("recv timed out")
        .expect("recv");

    assert_eq!(msg.topic(), topic);
    assert_eq!(msg.key(), Some(b"order-1".as_ref()));
    assert_eq!(msg.payload(), Some(b"{\"order_id\":\"order-1\"}".as_ref()));

    let parsed = msg
        .lineage()
        .expect("OpenLineage headers should be present");
    assert_eq!(parsed.namespace, "of://datasets");
    assert_eq!(parsed.job_name, "etl.orders.publish_test");
    assert_eq!(parsed.run_id, "run-it-001");
    assert_eq!(
        parsed.schema_url.as_deref(),
        Some("https://schemas.openfoundry.dev/orders/v1")
    );

    // Explicit commit; with auto-commit disabled this is the only thing that
    // advances the consumer group offset.
    msg.commit().expect("commit offset");
}
