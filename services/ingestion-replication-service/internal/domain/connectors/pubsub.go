// Google Cloud Pub/Sub streaming-source connector (Bloque P5).
//
// Uses the REST `pull` API (https://pubsub.googleapis.com) so we
// avoid pulling in google-cloud-pubsub (gRPC). The runner pulls a
// batch, ack-deadline-extends if the downstream commit takes longer
// than the subscription's default, and acks once the records have
// landed in the hot buffer.

package connectors

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// PubSubConfig is the operator-facing config persisted in
// `streaming_streams.source_binding.config` for Pub/Sub sources.
type PubSubConfig struct {
	ProjectID      string `json:"project_id"`
	SubscriptionID string `json:"subscription_id"`
	// MaxMessages is the max ack ids the runner asks for per pull.
	// Pub/Sub honours the number as a soft cap (it may return fewer).
	MaxMessages uint32 `json:"max_messages"`
	// AckDeadlineSeconds is the per-pull ack deadline override in
	// seconds (10..=600). Defaults to 60s, enough for a normal
	// hot-buffer publish.
	AckDeadlineSeconds uint32 `json:"ack_deadline_seconds"`
}

// UnmarshalJSON applies the Rust serde defaults: max_messages defaults
// to 100, ack_deadline_seconds to 60 when missing.
func (c *PubSubConfig) UnmarshalJSON(b []byte) error {
	type raw struct {
		ProjectID          string  `json:"project_id"`
		SubscriptionID     string  `json:"subscription_id"`
		MaxMessages        *uint32 `json:"max_messages"`
		AckDeadlineSeconds *uint32 `json:"ack_deadline_seconds"`
	}
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	c.ProjectID = r.ProjectID
	c.SubscriptionID = r.SubscriptionID
	if r.MaxMessages != nil {
		c.MaxMessages = *r.MaxMessages
	} else {
		c.MaxMessages = 100
	}
	if r.AckDeadlineSeconds != nil {
		c.AckDeadlineSeconds = *r.AckDeadlineSeconds
	} else {
		c.AckDeadlineSeconds = 60
	}
	return nil
}

// PubSubMessage mirrors the Rust struct of the same name.
type PubSubMessage struct {
	MessageID   string
	AckID       string
	Data        []byte
	PublishTime time.Time
	Attributes  map[string]any
}

// PubSubClient is the pluggable transport the connector calls per
// pull/ack/extend. Mirrors the Rust async trait of the same name.
type PubSubClient interface {
	Pull(ctx context.Context, subscription string, maxMessages uint32) ([]PubSubMessage, error)
	Acknowledge(ctx context.Context, subscription string, ackIDs []string) error
	ModifyAckDeadline(ctx context.Context, subscription string, ackIDs []string, deadlineSeconds uint32) error
}

// StaticPubSubClient is the in-memory test client. Mirrors the Rust
// `StaticPubSubClient` 1:1: a FIFO queue + ack log + deadline-extension
// log assertions can inspect.
type StaticPubSubClient struct {
	mu                  sync.Mutex
	queued              []PubSubMessage
	acked               []string
	deadlineExtensions  []DeadlineExtension
}

// DeadlineExtension is one ack-deadline extension recorded by the
// static client (1:1 to the Rust Vec<(String, u32)>).
type DeadlineExtension struct {
	AckID            string
	DeadlineSeconds  uint32
}

// NewStaticPubSubClient builds an empty in-memory client.
func NewStaticPubSubClient() *StaticPubSubClient { return &StaticPubSubClient{} }

// Enqueue appends test messages to the FIFO buffer.
func (c *StaticPubSubClient) Enqueue(messages ...PubSubMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queued = append(c.queued, messages...)
}

// Acked returns a snapshot of ack-ids the connector has acknowledged.
func (c *StaticPubSubClient) Acked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.acked...)
}

// DeadlineExtensions returns a snapshot of deadline extensions issued.
func (c *StaticPubSubClient) DeadlineExtensions() []DeadlineExtension {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]DeadlineExtension(nil), c.deadlineExtensions...)
}

// Pull drains up to maxMessages from the FIFO buffer.
func (c *StaticPubSubClient) Pull(_ context.Context, _ string, maxMessages uint32) ([]PubSubMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	take := int(maxMessages)
	if take > len(c.queued) {
		take = len(c.queued)
	}
	out := append([]PubSubMessage(nil), c.queued[:take]...)
	c.queued = append([]PubSubMessage(nil), c.queued[take:]...)
	return out, nil
}

// Acknowledge appends ack ids to the ack log.
func (c *StaticPubSubClient) Acknowledge(_ context.Context, _ string, ackIDs []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.acked = append(c.acked, ackIDs...)
	return nil
}

// ModifyAckDeadline appends one entry per ack id to the extension log.
func (c *StaticPubSubClient) ModifyAckDeadline(_ context.Context, _ string, ackIDs []string, deadlineSeconds uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ackIDs {
		c.deadlineExtensions = append(c.deadlineExtensions, DeadlineExtension{AckID: id, DeadlineSeconds: deadlineSeconds})
	}
	return nil
}

// HttpPubSubClient is the production HTTP-backed Pub/Sub client.
// SigV4-equivalent auth lives in the `Authorization: Bearer
// <oauth2-token>` header; the token is resolved by the caller
// (workload identity / metadata server).
type HttpPubSubClient struct {
	BaseURL     string
	BearerToken string
	HTTP        HttpDoer
}

// Pull issues a Pub/Sub `pull` REST call.
func (c *HttpPubSubClient) Pull(ctx context.Context, subscription string, maxMessages uint32) ([]PubSubMessage, error) {
	body, err := json.Marshal(map[string]any{
		"maxMessages":      maxMessages,
		"returnImmediately": false,
	})
	if err != nil {
		return nil, newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/%s:pull", strings.TrimRight(c.BaseURL, "/"), subscription),
		bytes.NewReader(body))
	if err != nil {
		return nil, newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newConnectorError(ConnectorErrorTransport,
			fmt.Sprintf("pubsub pull status %d", resp.StatusCode), nil)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, newConnectorError(ConnectorErrorDecode, err.Error(), err)
	}
	var decoded struct {
		ReceivedMessages []struct {
			AckID   string `json:"ackId"`
			Message struct {
				MessageID   string         `json:"messageId"`
				Data        string         `json:"data"`
				PublishTime string         `json:"publishTime"`
				Attributes  map[string]any `json:"attributes"`
			} `json:"message"`
		} `json:"receivedMessages"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, newConnectorError(ConnectorErrorDecode, err.Error(), err)
	}
	out := make([]PubSubMessage, 0, len(decoded.ReceivedMessages))
	for _, r := range decoded.ReceivedMessages {
		data, _ := base64.StdEncoding.DecodeString(r.Message.Data)
		ts, err := time.Parse(time.RFC3339Nano, r.Message.PublishTime)
		if err != nil {
			ts = time.Now().UTC()
		}
		attrs := r.Message.Attributes
		if attrs == nil {
			attrs = map[string]any{}
		}
		out = append(out, PubSubMessage{
			MessageID:   r.Message.MessageID,
			AckID:       r.AckID,
			Data:        data,
			PublishTime: ts,
			Attributes:  attrs,
		})
	}
	return out, nil
}

// Acknowledge issues a Pub/Sub `acknowledge` REST call.
func (c *HttpPubSubClient) Acknowledge(ctx context.Context, subscription string, ackIDs []string) error {
	body, err := json.Marshal(map[string]any{"ackIds": ackIDs})
	if err != nil {
		return newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/%s:acknowledge", strings.TrimRight(c.BaseURL, "/"), subscription),
		bytes.NewReader(body))
	if err != nil {
		return newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newConnectorError(ConnectorErrorTransport,
			fmt.Sprintf("pubsub ack status %d", resp.StatusCode), nil)
	}
	return nil
}

// ModifyAckDeadline issues a Pub/Sub `modifyAckDeadline` REST call.
func (c *HttpPubSubClient) ModifyAckDeadline(ctx context.Context, subscription string, ackIDs []string, deadlineSeconds uint32) error {
	body, err := json.Marshal(map[string]any{
		"ackIds":             ackIDs,
		"ackDeadlineSeconds": deadlineSeconds,
	})
	if err != nil {
		return newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		fmt.Sprintf("%s/v1/%s:modifyAckDeadline", strings.TrimRight(c.BaseURL, "/"), subscription),
		bytes.NewReader(body))
	if err != nil {
		return newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return newConnectorError(ConnectorErrorTransport, err.Error(), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return newConnectorError(ConnectorErrorTransport,
			fmt.Sprintf("pubsub modifyAckDeadline status %d", resp.StatusCode), nil)
	}
	return nil
}

// PubSubConnector implements StreamingSourceConnector against a
// PubSubClient. Mirrors the Rust generic struct of the same name.
type PubSubConnector struct {
	Config PubSubConfig
	Client PubSubClient

	mu       sync.Mutex
	lastPull *time.Time
}

// NewPubSubConnector builds a Pub/Sub connector against client.
func NewPubSubConnector(config PubSubConfig, client PubSubClient) *PubSubConnector {
	return &PubSubConnector{Config: config, Client: client}
}

// Subscription returns the fully-qualified subscription resource name.
func (c *PubSubConnector) Subscription() string {
	return fmt.Sprintf("projects/%s/subscriptions/%s", c.Config.ProjectID, c.Config.SubscriptionID)
}

// Kind reports the stable identifier.
func (c *PubSubConnector) Kind() string { return "pubsub" }

// Pull issues a Pub/Sub pull, extends the ack deadline so a slow
// downstream commit doesn't cause Pub/Sub to redeliver the batch
// underneath us, and adapts each message into a SourceRecord. Returns
// ConnectorErrorEmpty when the subscription had nothing to deliver.
func (c *PubSubConnector) Pull(ctx context.Context, opts PullOptions) ([]SourceRecord, error) {
	n := opts.BatchSize
	if c.Config.MaxMessages < n {
		n = c.Config.MaxMessages
	}
	subscription := c.Subscription()
	msgs, err := c.Client.Pull(ctx, subscription, n)
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
	ackIDs := make([]string, 0, len(msgs))
	for _, m := range msgs {
		ackIDs = append(ackIDs, m.AckID)
	}
	_ = c.Client.ModifyAckDeadline(ctx, subscription, ackIDs, c.Config.AckDeadlineSeconds)
	out := make([]SourceRecord, 0, len(msgs))
	for _, m := range msgs {
		var payload json.RawMessage
		if json.Valid(m.Data) {
			payload = append(json.RawMessage(nil), m.Data...)
		} else {
			payload, _ = json.Marshal(string(m.Data))
		}
		var partitionKey *string
		if v, ok := m.Attributes["ordering_key"]; ok {
			if s, ok := v.(string); ok {
				partitionKey = &s
			}
		}
		metadata, _ := json.Marshal(map[string]any{
			"ack_id":     m.AckID,
			"attributes": m.Attributes,
		})
		out = append(out, SourceRecord{
			SourceID:     m.MessageID,
			PartitionKey: partitionKey,
			Payload:      payload,
			EventTime:    m.PublishTime,
			Metadata:     metadata,
		})
	}
	return out, nil
}

// Checkpoint is a no-op for Pub/Sub — progress is tracked via
// per-message ack, not a separate cursor.
func (c *PubSubConnector) Checkpoint(_ context.Context, _ ConnectorCheckpoint) error { return nil }

// Ack pulls ack_id out of the record metadata and acknowledges the
// message.
func (c *PubSubConnector) Ack(ctx context.Context, record SourceRecord) error {
	ackID, ok := metadataString(record.Metadata, "ack_id")
	if !ok {
		return newConnectorError(ConnectorErrorDecode, "missing ack_id", nil)
	}
	return c.Client.Acknowledge(ctx, c.Subscription(), []string{ackID})
}

// Health reports a healthy snapshot tagged with the most recent pull.
func (c *PubSubConnector) Health(_ context.Context) ConnectorHealth {
	c.mu.Lock()
	defer c.mu.Unlock()
	return ConnectorHealth{Status: ConnectorStatusHealthy, LastPullAt: c.lastPull}
}

// PubSubCatalogEntry mirrors the Rust `catalog_entry` helper.
func PubSubCatalogEntry(stream *StreamDefinition) ConnectorCatalogEntry {
	details, _ := json.Marshal(map[string]any{
		"format": stream.SourceBinding.Format,
		"doc":    "Google Cloud Pub/Sub source — REST pull + ack",
	})
	return ConnectorCatalogEntry{
		ConnectorType:       "pubsub",
		Direction:           "source",
		Endpoint:            stream.SourceBinding.Endpoint,
		Status:              "healthy",
		Backlog:             0,
		ThroughputPerSecond: 0,
		Details:             details,
	}
}

// Compile-time assertion the connector satisfies the trait.
var _ StreamingSourceConnector = (*PubSubConnector)(nil)
