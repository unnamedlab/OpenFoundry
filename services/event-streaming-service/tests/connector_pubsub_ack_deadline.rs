//! Bloque P5 — Pub/Sub ack-deadline behaviour.
//!
//! Each pull must extend the ack deadline before the runner commits
//! to the hot buffer; otherwise Pub/Sub redelivers the batch
//! underneath us and we observe duplicates downstream.

use chrono::Utc;
use event_streaming_service::domain::connectors::pubsub::{
    PubSubConfig, PubSubConnector, PubSubMessage, StaticPubSubClient,
};
use event_streaming_service::domain::connectors::source_trait::{
    PullOptions, StreamingSourceConnector,
};

fn enqueue(client: &StaticPubSubClient, ack_id: &str) {
    client.queued.lock().unwrap().push(PubSubMessage {
        message_id: format!("m-{ack_id}"),
        ack_id: ack_id.to_string(),
        data: b"{\"v\":1}".to_vec(),
        publish_time: Utc::now(),
        attributes: Default::default(),
    });
}

#[tokio::test]
async fn connector_pubsub_extends_ack_deadline_on_each_pull() {
    let client = StaticPubSubClient::default();
    enqueue(&client, "a");
    enqueue(&client, "b");
    let connector = PubSubConnector::new(
        PubSubConfig {
            project_id: "p".into(),
            subscription_id: "s".into(),
            max_messages: 10,
            ack_deadline_seconds: 120,
        },
        client,
    );
    let records = connector.pull(&PullOptions::default()).await.unwrap();
    assert_eq!(records.len(), 2);
    let exts = connector.client.deadline_extensions.lock().unwrap().clone();
    assert!(exts.iter().any(|(id, dl)| id == "a" && *dl == 120));
    assert!(exts.iter().any(|(id, dl)| id == "b" && *dl == 120));
}

#[tokio::test]
async fn connector_pubsub_acks_individual_messages() {
    let client = StaticPubSubClient::default();
    enqueue(&client, "ack-1");
    let connector = PubSubConnector::new(
        PubSubConfig {
            project_id: "p".into(),
            subscription_id: "s".into(),
            max_messages: 10,
            ack_deadline_seconds: 60,
        },
        client,
    );
    let mut records = connector.pull(&PullOptions::default()).await.unwrap();
    let r = records.remove(0);
    connector.ack(&r).await.unwrap();
    assert_eq!(
        connector.client.acked.lock().unwrap().clone(),
        vec!["ack-1".to_string()]
    );
}

#[tokio::test]
async fn connector_pubsub_kind_label_is_stable() {
    let connector = PubSubConnector::new(
        PubSubConfig {
            project_id: "p".into(),
            subscription_id: "s".into(),
            max_messages: 10,
            ack_deadline_seconds: 60,
        },
        StaticPubSubClient::default(),
    );
    assert_eq!(connector.kind(), "pubsub");
}
