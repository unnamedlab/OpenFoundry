package connectors

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// extends_ack_deadline_on_each_pull (1:1 port from
// services/ingestion-replication-service/src/event_streaming/domain/connectors/pubsub.rs).
func TestPubSubExtendsAckDeadlineOnEachPull(t *testing.T) {
	t.Parallel()
	client := NewStaticPubSubClient()
	client.Enqueue(PubSubMessage{
		MessageID:   "m-1",
		AckID:       "ack-1",
		Data:        []byte(`{"v":1}`),
		PublishTime: time.Now().UTC(),
		Attributes:  map[string]any{},
	})
	connector := NewPubSubConnector(PubSubConfig{
		ProjectID:          "p",
		SubscriptionID:     "s",
		MaxMessages:        10,
		AckDeadlineSeconds: 90,
	}, client)
	recs, err := connector.Pull(context.Background(), DefaultPullOptions())
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("len(recs)=%d, want 1", len(recs))
	}
	got := client.DeadlineExtensions()
	want := []DeadlineExtension{{AckID: "ack-1", DeadlineSeconds: 90}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extensions=%+v, want %+v", got, want)
	}
}

// ack_acknowledges_one_message (1:1 port).
func TestPubSubAckAcknowledgesOneMessage(t *testing.T) {
	t.Parallel()
	client := NewStaticPubSubClient()
	client.Enqueue(PubSubMessage{
		MessageID:   "m-1",
		AckID:       "ack-1",
		Data:        []byte("hi"),
		PublishTime: time.Now().UTC(),
		Attributes:  map[string]any{},
	})
	connector := NewPubSubConnector(PubSubConfig{
		ProjectID:          "p",
		SubscriptionID:     "s",
		MaxMessages:        10,
		AckDeadlineSeconds: 60,
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
	got := client.Acked()
	want := []string{"ack-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("acked=%v, want %v", got, want)
	}
}

// Sanity: PubSubConfig's serde defaults are applied when keys are absent.
func TestPubSubConfigDefaultsFromJSON(t *testing.T) {
	t.Parallel()
	var cfg PubSubConfig
	if err := json.Unmarshal([]byte(`{"project_id":"p","subscription_id":"s"}`), &cfg); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if cfg.MaxMessages != 100 {
		t.Errorf("MaxMessages=%d, want 100", cfg.MaxMessages)
	}
	if cfg.AckDeadlineSeconds != 60 {
		t.Errorf("AckDeadlineSeconds=%d, want 60", cfg.AckDeadlineSeconds)
	}
}
