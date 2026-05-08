package connectors

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// ack_calls_delete_message (1:1 port from
// services/ingestion-replication-service/src/event_streaming/domain/connectors/sqs.rs).
func TestSqsAckCallsDeleteMessage(t *testing.T) {
	t.Parallel()
	client := NewStaticSqsClient()
	client.Enqueue(SqsMessage{
		MessageID:     "m-1",
		ReceiptHandle: "rcpt-1",
		Body:          `{"v":1}`,
		SentAt:        time.Now().UTC(),
	})
	connector := NewSqsConnector(SqsConfig{
		QueueURL:                 "https://sqs.example.com/q",
		Region:                   "us-east-1",
		WaitTimeSeconds:          20,
		VisibilityTimeoutSeconds: 60,
	}, client)
	recs, err := connector.Pull(context.Background(), DefaultPullOptions())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(recs)=%d, want 1", len(recs))
	}
	if err := connector.Ack(context.Background(), recs[0]); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	got := client.Deleted()
	want := []string{"rcpt-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deleted=%v, want %v", got, want)
	}
}

// Sanity: SqsConfig's serde defaults are applied when keys are absent.
func TestSqsConfigDefaultsFromJSON(t *testing.T) {
	t.Parallel()
	var cfg SqsConfig
	if err := json.Unmarshal([]byte(`{"queue_url":"u","region":"r"}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.WaitTimeSeconds != 20 {
		t.Errorf("WaitTimeSeconds=%d, want 20", cfg.WaitTimeSeconds)
	}
	if cfg.VisibilityTimeoutSeconds != 60 {
		t.Errorf("VisibilityTimeoutSeconds=%d, want 60", cfg.VisibilityTimeoutSeconds)
	}
}
