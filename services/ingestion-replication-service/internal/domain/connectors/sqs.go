// Amazon SQS streaming-source connector (Bloque P5).
//
// Long-poll-based pull with explicit per-message ack (DeleteMessage).
// Records that are not deleted before `visibility_timeout` are
// redelivered by SQS automatically; the runner gets at-least-once
// semantics.

package connectors

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// SqsConfig is the operator-facing config persisted in
// `streaming_streams.source_binding.config` for SQS sources.
type SqsConfig struct {
	QueueURL                 string `json:"queue_url"`
	Region                   string `json:"region"`
	WaitTimeSeconds          uint32 `json:"wait_time_seconds"`
	VisibilityTimeoutSeconds uint32 `json:"visibility_timeout_seconds"`
}

func (c *SqsConfig) UnmarshalJSON(b []byte) error {
	type raw struct {
		QueueURL                 string  `json:"queue_url"`
		Region                   string  `json:"region"`
		WaitTimeSeconds          *uint32 `json:"wait_time_seconds"`
		VisibilityTimeoutSeconds *uint32 `json:"visibility_timeout_seconds"`
	}
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	c.QueueURL = r.QueueURL
	c.Region = r.Region
	if r.WaitTimeSeconds != nil {
		c.WaitTimeSeconds = *r.WaitTimeSeconds
	} else {
		c.WaitTimeSeconds = 20
	}
	if r.VisibilityTimeoutSeconds != nil {
		c.VisibilityTimeoutSeconds = *r.VisibilityTimeoutSeconds
	} else {
		c.VisibilityTimeoutSeconds = 60
	}
	return nil
}

// SqsMessage mirrors the Rust struct of the same name.
type SqsMessage struct {
	MessageID     string
	ReceiptHandle string
	Body          string
	SentAt        time.Time
}

// SqsClient is the pluggable transport the connector calls.
// Mirrors the Rust async trait of the same name.
type SqsClient interface {
	ReceiveMessages(ctx context.Context, queueURL string, maxMessages, waitSeconds, visibilitySeconds uint32) ([]SqsMessage, error)
	DeleteMessage(ctx context.Context, queueURL, receiptHandle string) error
}

// StaticSqsClient is the in-memory test client.
type StaticSqsClient struct {
	mu      sync.Mutex
	queued  []SqsMessage
	deleted []string
}

func NewStaticSqsClient() *StaticSqsClient { return &StaticSqsClient{} }

func (c *StaticSqsClient) Enqueue(messages ...SqsMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queued = append(c.queued, messages...)
}

func (c *StaticSqsClient) Deleted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.deleted...)
}

func (c *StaticSqsClient) ReceiveMessages(_ context.Context, _ string, maxMessages, _, _ uint32) ([]SqsMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	take := int(maxMessages)
	if take > len(c.queued) {
		take = len(c.queued)
	}
	out := append([]SqsMessage(nil), c.queued[:take]...)
	c.queued = append([]SqsMessage(nil), c.queued[take:]...)
	return out, nil
}

func (c *StaticSqsClient) DeleteMessage(_ context.Context, _, receiptHandle string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, receiptHandle)
	return nil
}

// SqsConnector implements StreamingSourceConnector against an SqsClient.
type SqsConnector struct {
	Config SqsConfig
	Client SqsClient

	mu       sync.Mutex
	lastPull *time.Time
}

func NewSqsConnector(config SqsConfig, client SqsClient) *SqsConnector {
	return &SqsConnector{Config: config, Client: client}
}

func (c *SqsConnector) Kind() string { return "sqs" }

func (c *SqsConnector) Pull(ctx context.Context, opts PullOptions) ([]SourceRecord, error) {
	max := opts.BatchSize
	if max > 10 { // SQS API hard cap is 10.
		max = 10
	}
	msgs, err := c.Client.ReceiveMessages(ctx, c.Config.QueueURL, max, c.Config.WaitTimeSeconds, c.Config.VisibilityTimeoutSeconds)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	now := time.Now().UTC()
	c.lastPull = &now
	c.mu.Unlock()
	if len(msgs) == 0 {
		return nil, &ConnectorError{Kind: ConnectorErrorEmpty}
	}
	out := make([]SourceRecord, 0, len(msgs))
	for _, m := range msgs {
		var payload json.RawMessage
		if json.Valid([]byte(m.Body)) {
			payload = json.RawMessage(m.Body)
		} else {
			payload, _ = json.Marshal(m.Body)
		}
		metadata, _ := json.Marshal(map[string]any{
			"receipt_handle": m.ReceiptHandle,
			"queue_url":      c.Config.QueueURL,
		})
		out = append(out, SourceRecord{
			SourceID:     m.MessageID,
			PartitionKey: nil,
			Payload:      payload,
			EventTime:    m.SentAt,
			Metadata:     metadata,
		})
	}
	return out, nil
}

func (c *SqsConnector) Checkpoint(_ context.Context, _ ConnectorCheckpoint) error { return nil }

func (c *SqsConnector) Ack(ctx context.Context, record SourceRecord) error {
	handle, ok := metadataString(record.Metadata, "receipt_handle")
	if !ok {
		return newConnectorError(ConnectorErrorDecode, "missing receipt_handle", nil)
	}
	return c.Client.DeleteMessage(ctx, c.Config.QueueURL, handle)
}

func (c *SqsConnector) Health(_ context.Context) ConnectorHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ConnectorHealth{Status: ConnectorStatusHealthy, LastPullAt: c.lastPull}
}

// SqsCatalogEntry mirrors the Rust `catalog_entry` helper.
func SqsCatalogEntry(stream *StreamDefinition) ConnectorCatalogEntry {
	details, _ := json.Marshal(map[string]any{
		"format": stream.SourceBinding.Format,
		"doc":    "Amazon SQS source — long-poll + per-message ack",
	})
	return ConnectorCatalogEntry{
		ConnectorType:       "sqs",
		Direction:           "source",
		Endpoint:            stream.SourceBinding.Endpoint,
		Status:              "healthy",
		Backlog:             0,
		ThroughputPerSecond: 0,
		Details:             details,
	}
}

func metadataString(meta json.RawMessage, key string) (string, bool) {
	if len(meta) == 0 {
		return "", false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(meta, &m); err != nil {
		return "", false
	}
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	if v, err := strconv.Unquote(string(raw)); err == nil {
		return v, true
	}
	return "", false
}

var _ StreamingSourceConnector = (*SqsConnector)(nil)
