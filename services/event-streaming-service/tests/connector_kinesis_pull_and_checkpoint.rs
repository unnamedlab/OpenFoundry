//! Bloque P5 — Kinesis connector smoke.
//!
//! Exercises [`KinesisConnector::pull`] and `checkpoint` end-to-end
//! using the [`StaticKinesisClient`] to feed canned shard records.
//! No LocalStack required; the user's instructions allow either a
//! testcontainer or a mock and we pick the mock for fast CI.

use chrono::Utc;
use event_streaming_service::domain::connectors::kinesis::{
    KinesisConfig, KinesisConnector, KinesisRecord, StaticKinesisClient,
};
use event_streaming_service::domain::connectors::source_trait::{
    ConnectorCheckpoint, ConnectorError, PullOptions, StreamingSourceConnector,
};
use uuid::Uuid;

fn build() -> KinesisConnector<StaticKinesisClient> {
    KinesisConnector::new(
        KinesisConfig {
            stream_name: "demo".into(),
            region: "us-east-1".into(),
            shard_iterator_type: "TRIM_HORIZON".into(),
            max_records_per_shard: 50,
        },
        StaticKinesisClient::default(),
        "shardId-000000000000",
    )
}

#[tokio::test]
async fn connector_kinesis_pulls_records_in_sequence_order() {
    let connector = build();
    {
        let mut q = connector.client.queued.lock().unwrap();
        q.push(KinesisRecord {
            sequence_number: "100".into(),
            partition_key: "k1".into(),
            data: b"{\"v\":1}".to_vec(),
            approximate_arrival_timestamp: Utc::now(),
        });
        q.push(KinesisRecord {
            sequence_number: "101".into(),
            partition_key: "k1".into(),
            data: b"{\"v\":2}".to_vec(),
            approximate_arrival_timestamp: Utc::now(),
        });
    }
    let records = connector.pull(&PullOptions::default()).await.unwrap();
    assert_eq!(records.len(), 2);
    assert_eq!(records[0].source_id, "100");
    assert_eq!(records[1].source_id, "101");
    assert_eq!(records[0].partition_key.as_deref(), Some("k1"));
}

#[tokio::test]
async fn connector_kinesis_returns_empty_when_shard_drained() {
    let connector = build();
    let err = connector.pull(&PullOptions::default()).await.unwrap_err();
    assert!(matches!(err, ConnectorError::Empty));
}

#[tokio::test]
async fn connector_kinesis_persists_checkpoint() {
    let connector = build();
    let cp = ConnectorCheckpoint {
        connector_kind: "kinesis".into(),
        stream_id: Uuid::nil(),
        cursor: serde_json::json!({ "shardId-000000000000": "100" }),
        updated_at: Utc::now(),
    };
    connector.checkpoint(&cp).await.unwrap();
    let stored = connector.checkpoint_store.lock().unwrap().clone();
    assert!(stored.is_some());
    assert_eq!(
        stored.unwrap().cursor["shardId-000000000000"],
        serde_json::Value::String("100".into())
    );
}

#[tokio::test]
async fn connector_kinesis_kind_label_matches_metric_keys() {
    // Lock in the `kind()` string the metrics labels read.
    let connector = build();
    assert_eq!(connector.kind(), "kinesis");
}
