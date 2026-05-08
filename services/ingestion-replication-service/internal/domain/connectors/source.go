// Package connectors holds Foundry-parity streaming source-connector
// adapters (Bloque P5). Every external source — Kafka, Kinesis, SQS,
// Pub/Sub, Aveva PI, Magritte external transform — implements
// [StreamingSourceConnector] so the streaming-sync runner can pull
// records uniformly. Each connector also owns its own offset
// checkpointing so a connector restart does not replay or lose data.
//
// The trait is intentionally narrow: pull a batch, ack/checkpoint,
// describe yourself for the catalogue, surface backpressure /
// liveness signals. The runner is responsible for committing batches
// to the hot buffer and progressing the cold-tier archiver.
package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ConnectorErrorKind enumerates the closed set of failures a connector
// may surface to the runner. Mirrors the Rust thiserror enum
// `ConnectorError` 1:1.
type ConnectorErrorKind int

const (
	ConnectorErrorEmpty ConnectorErrorKind = iota
	ConnectorErrorAuth
	ConnectorErrorUnavailable
	ConnectorErrorClient
	ConnectorErrorTransport
	ConnectorErrorDecode
)

// ConnectorError is the typed error returned by every connector method.
// The wire string format mirrors Rust's `#[error(...)]` exactly.
type ConnectorError struct {
	Kind    ConnectorErrorKind
	Message string
	Cause   error
}

func (e *ConnectorError) Error() string {
	switch e.Kind {
	case ConnectorErrorEmpty:
		return "source returned no records"
	case ConnectorErrorAuth:
		return "auth: " + e.Message
	case ConnectorErrorUnavailable:
		return "unavailable: " + e.Message
	case ConnectorErrorClient:
		return "client error: " + e.Message
	case ConnectorErrorTransport:
		return "transport: " + e.Message
	case ConnectorErrorDecode:
		return "decode: " + e.Message
	default:
		return "connector error: " + e.Message
	}
}

func (e *ConnectorError) Unwrap() error { return e.Cause }

var (
	ErrConnectorEmpty       = errors.New("source returned no records")
	ErrConnectorAuth        = errors.New("auth")
	ErrConnectorUnavailable = errors.New("unavailable")
	ErrConnectorClient      = errors.New("client error")
	ErrConnectorTransport   = errors.New("transport")
	ErrConnectorDecode      = errors.New("decode")
)

func (e *ConnectorError) Is(target error) bool {
	switch e.Kind {
	case ConnectorErrorEmpty:
		return target == ErrConnectorEmpty
	case ConnectorErrorAuth:
		return target == ErrConnectorAuth
	case ConnectorErrorUnavailable:
		return target == ErrConnectorUnavailable
	case ConnectorErrorClient:
		return target == ErrConnectorClient
	case ConnectorErrorTransport:
		return target == ErrConnectorTransport
	case ConnectorErrorDecode:
		return target == ErrConnectorDecode
	}
	return false
}

func newConnectorError(kind ConnectorErrorKind, msg string, cause error) *ConnectorError {
	return &ConnectorError{Kind: kind, Message: msg, Cause: cause}
}

// SourceRecord mirrors event_streaming::domain::connectors::source_trait::SourceRecord.
type SourceRecord struct {
	SourceID     string          `json:"source_id"`
	PartitionKey *string         `json:"partition_key"`
	Payload      json.RawMessage `json:"payload"`
	EventTime    time.Time       `json:"event_time"`
	Metadata     json.RawMessage `json:"metadata"`
}

// StreamingSyncConfig mirrors the Rust struct of the same name.
type StreamingSyncConfig struct {
	TargetStreamRID string `json:"target_stream_rid"`
	BatchSize       uint32 `json:"batch_size"`
	PollIntervalMS  uint64 `json:"poll_interval_ms"`
	SchemaInference bool   `json:"schema_inference"`
}

// DefaultStreamingSyncConfig mirrors the Rust `Default` impl.
func DefaultStreamingSyncConfig() StreamingSyncConfig {
	return StreamingSyncConfig{BatchSize: 100, PollIntervalMS: 1000}
}

func (c *StreamingSyncConfig) UnmarshalJSON(b []byte) error {
	type raw struct {
		TargetStreamRID string  `json:"target_stream_rid"`
		BatchSize       *uint32 `json:"batch_size"`
		PollIntervalMS  *uint64 `json:"poll_interval_ms"`
		SchemaInference *bool   `json:"schema_inference"`
	}
	var r raw
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	c.TargetStreamRID = r.TargetStreamRID
	if r.BatchSize != nil {
		c.BatchSize = *r.BatchSize
	} else {
		c.BatchSize = 100
	}
	if r.PollIntervalMS != nil {
		c.PollIntervalMS = *r.PollIntervalMS
	} else {
		c.PollIntervalMS = 1000
	}
	if r.SchemaInference != nil {
		c.SchemaInference = *r.SchemaInference
	}
	return nil
}

// PullOptions mirrors event_streaming::domain::connectors::source_trait::PullOptions.
type PullOptions struct {
	BatchSize uint32
	MaxWaitMS uint64
}

// DefaultPullOptions mirrors the Rust `Default` impl.
func DefaultPullOptions() PullOptions {
	return PullOptions{BatchSize: 100, MaxWaitMS: 5000}
}

// ConnectorCheckpoint mirrors event_streaming::domain::connectors::source_trait::ConnectorCheckpoint.
type ConnectorCheckpoint struct {
	ConnectorKind string          `json:"connector_kind"`
	StreamID      uuid.UUID       `json:"stream_id"`
	Cursor        json.RawMessage `json:"cursor"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// ConnectorStatus is the connector liveness state.
type ConnectorStatus string

const (
	ConnectorStatusHealthy     ConnectorStatus = "healthy"
	ConnectorStatusDegraded    ConnectorStatus = "degraded"
	ConnectorStatusUnreachable ConnectorStatus = "unreachable"
	ConnectorStatusDisabled    ConnectorStatus = "disabled"
)

// ConnectorHealth mirrors event_streaming::domain::connectors::source_trait::ConnectorHealth.
type ConnectorHealth struct {
	Status              ConnectorStatus `json:"status"`
	Backlog             int64           `json:"backlog"`
	ThroughputPerSecond float64         `json:"throughput_per_second"`
	LastPullAt          *time.Time      `json:"last_pull_at"`
}

// DefaultConnectorHealth mirrors the Rust `Default` impl.
func DefaultConnectorHealth() ConnectorHealth {
	return ConnectorHealth{Status: ConnectorStatusHealthy}
}

// StreamingSourceConnector mirrors the Rust async_trait of the same name.
type StreamingSourceConnector interface {
	Kind() string
	Pull(ctx context.Context, opts PullOptions) ([]SourceRecord, error)
	Checkpoint(ctx context.Context, checkpoint ConnectorCheckpoint) error
	Ack(ctx context.Context, record SourceRecord) error
	Health(ctx context.Context) ConnectorHealth
}

// ConnectorBinding mirrors event_streaming::models::stream::ConnectorBinding.
type ConnectorBinding struct {
	ConnectorType string          `json:"connector_type"`
	Endpoint      string          `json:"endpoint"`
	Format        string          `json:"format"`
	Config        json.RawMessage `json:"config"`
}

// StreamDefinition is the minimal projection of the Rust
// `event_streaming::models::stream::StreamDefinition` the connector
// catalog_entry helpers care about.
type StreamDefinition struct {
	ID            uuid.UUID        `json:"id"`
	Name          string           `json:"name"`
	SourceBinding ConnectorBinding `json:"source_binding"`
}

// ConnectorCatalogEntry mirrors event_streaming::models::sink::ConnectorCatalogEntry.
type ConnectorCatalogEntry struct {
	ConnectorType       string          `json:"connector_type"`
	Direction           string          `json:"direction"`
	Endpoint            string          `json:"endpoint"`
	Status              string          `json:"status"`
	Backlog             int32           `json:"backlog"`
	ThroughputPerSecond float32         `json:"throughput_per_second"`
	Details             json.RawMessage `json:"details"`
}
