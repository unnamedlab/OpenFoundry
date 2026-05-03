#![cfg(feature = "runtime")]

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use event_bus_data::{
    DataPublisher, KafkaPublisher, KafkaSubscriber, OpenLineageHeaders, testkit::EphemeralKafka,
};
use ontology_indexer::{runtime, topics};
use storage_abstraction::repositories::{
    ObjectId, Page, ReadConsistency, SearchBackend, SearchQuery, TenantId, TypeId,
    noop::InMemorySearchBackend,
};

#[tokio::test]
#[ignore = "requires Docker for ephemeral Kafka"]
async fn kafka_event_is_consumed_and_indexed() {
    let kafka = EphemeralKafka::start().await.expect("start kafka");
    kafka
        .create_topic(topics::ONTOLOGY_OBJECT_CHANGED_V1, 1)
        .await
        .expect("create topic");

    let backend: Arc<dyn SearchBackend> = Arc::new(InMemorySearchBackend::default());
    let subscriber_cfg = kafka.config_for("ontology-indexer");
    let publisher_cfg = kafka.config_for("ontology-indexer-it");
    let subscriber =
        KafkaSubscriber::new(&subscriber_cfg, runtime::CONSUMER_GROUP).expect("build subscriber");
    let publisher = KafkaPublisher::new(&publisher_cfg).expect("build publisher");

    let backend_for_task = Arc::clone(&backend);
    let task = tokio::spawn(async move { runtime::run(subscriber, backend_for_task).await });

    let payload = serde_json::json!({
        "tenant": "tenant-it",
        "id": "obj-1",
        "type_id": "Aircraft",
        "version": 7,
        "payload": { "tail_number": "EC-123", "color": "blue" },
        "deleted": false
    });
    publisher
        .publish(
            topics::ONTOLOGY_OBJECT_CHANGED_V1,
            Some(b"tenant-it/obj-1"),
            &serde_json::to_vec(&payload).expect("serialize payload"),
            &OpenLineageHeaders::new(
                "of://ontology",
                "ontology-indexer-it",
                "run-it-001",
                "urn:openfoundry:test",
            )
            .with_schema_url("https://openlineage.io/spec/1-0-5/OpenLineage.json"),
        )
        .await
        .expect("publish record");

    let tenant = TenantId("tenant-it".into());
    let query = SearchQuery {
        tenant: tenant.clone(),
        type_id: Some(TypeId("Aircraft".into())),
        q: Some("EC-123".into()),
        filters: HashMap::new(),
        page: Page {
            size: 10,
            token: None,
        },
    };

    let mut indexed = false;
    for _ in 0..30 {
        let result = backend
            .search(query.clone(), ReadConsistency::Eventual)
            .await
            .expect("search backend query");
        if result
            .items
            .iter()
            .any(|hit| hit.id == ObjectId("obj-1".into()))
        {
            indexed = true;
            break;
        }
        tokio::time::sleep(Duration::from_millis(200)).await;
    }

    task.abort();
    let _ = task.await;

    assert!(indexed, "ontology-indexer should index the Kafka event");
}
