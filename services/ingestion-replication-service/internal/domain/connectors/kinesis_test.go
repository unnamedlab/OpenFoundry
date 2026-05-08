package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// pull_returns_records_in_order_and_advances_iterator (1:1 port from
// services/ingestion-replication-service/src/event_streaming/domain/connectors/kinesis.rs).
func TestKinesisPullReturnsRecordsInOrderAndAdvancesIterator(t *testing.T) {
	t.Parallel()
	client := NewStaticKinesisClient()
	now := time.Now().UTC()
	client.Enqueue(
		KinesisRecord{
			SequenceNumber:              "1",
			PartitionKey:                "k",
			Data:                        []byte(`{"v":1}`),
			ApproximateArrivalTimestamp: now,
		},
		KinesisRecord{
			SequenceNumber:              "2",
			PartitionKey:                "k",
			Data:                        []byte(`{"v":2}`),
			ApproximateArrivalTimestamp: now,
		},
	)
	connector := NewKinesisConnector(KinesisConfig{
		StreamName:         "s",
		Region:             "us-east-1",
		ShardIteratorType:  "LATEST",
		MaxRecordsPerShard: 100,
	}, client, "shard-0")
	recs, err := connector.Pull(context.Background(), DefaultPullOptions())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("len(recs)=%d, want 2", len(recs))
	}
	if recs[0].SourceID != "1" {
		t.Fatalf("recs[0].SourceID=%q, want %q", recs[0].SourceID, "1")
	}
	if recs[1].PartitionKey == nil || *recs[1].PartitionKey != "k" {
		got := "<nil>"
		if recs[1].PartitionKey != nil {
			got = *recs[1].PartitionKey
		}
		t.Fatalf("recs[1].PartitionKey=%q, want %q", got, "k")
	}
}

// pull_returns_empty_when_no_records (1:1 port).
func TestKinesisPullReturnsEmptyWhenNoRecords(t *testing.T) {
	t.Parallel()
	connector := NewKinesisConnector(KinesisConfig{
		StreamName:         "s",
		Region:             "us-east-1",
		ShardIteratorType:  "LATEST",
		MaxRecordsPerShard: 100,
	}, NewStaticKinesisClient(), "shard-0")
	_, err := connector.Pull(context.Background(), DefaultPullOptions())
	if !errors.Is(err, ErrConnectorEmpty) {
		t.Fatalf("Pull err=%v, want ConnectorErrorEmpty", err)
	}
}

// Sanity: KinesisConfig's serde defaults are applied when keys are absent.
func TestKinesisConfigDefaultsFromJSON(t *testing.T) {
	t.Parallel()
	var cfg KinesisConfig
	if err := json.Unmarshal([]byte(`{"stream_name":"s","region":"us-east-1"}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.ShardIteratorType != "LATEST" {
		t.Errorf("ShardIteratorType=%q, want LATEST", cfg.ShardIteratorType)
	}
	if cfg.MaxRecordsPerShard != 100 {
		t.Errorf("MaxRecordsPerShard=%d, want 100", cfg.MaxRecordsPerShard)
	}
}
