// Package hotbuffer ports the event-streaming hot buffer abstraction from
// services/ingestion-replication-service/src/event_streaming/domain/hot_buffer.
//
// The "hot" tier of the streaming storage stack is a publish-only view of the
// most recent N seconds of events for each stream. It is backed by either
// Apache Kafka (production) or NATS Core (lightweight dev / tests). Cold-tier
// reads (Iceberg + Parquet) are handled separately by the dataset writer
// (internal/storage). Two implementations live below this package:
//
//   - NatsHotBuffer: always available, used as the default and as the
//     fallback when Kafka is not configured.
//   - NoopHotBuffer: dropped publishes; used when neither NATS nor Kafka is
//     configured so the REST control plane can still boot in degraded mode.
//
// Both implement the HotBuffer interface so the rest of the service stays
// transport-agnostic.
package hotbuffer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
)

// StreamType mirrors the Foundry-parity stream type tunables used by the
// Kafka backend to swap producer settings (linger.ms / batch.size /
// compression.type). NATS / Noop ignore the value.
type StreamType int

const (
	// StreamTypeStandard is the default profile.
	StreamTypeStandard StreamType = iota
	// StreamTypeLowLatency favours latency over throughput.
	StreamTypeLowLatency
	// StreamTypeHighThroughput favours throughput over latency.
	StreamTypeHighThroughput
)

// HotBufferErrorKind enumerates the HotBufferError variants from Rust.
type HotBufferErrorKind int

const (
	// HotBufferErrorUnavailable maps to HotBufferError::Unavailable. The
	// backend is not reachable (broker down, not provisioned, etc.).
	HotBufferErrorUnavailable HotBufferErrorKind = iota
	// HotBufferErrorTransport maps to HotBufferError::Transport. A
	// specific publish / admin call failed at the transport layer.
	HotBufferErrorTransport
)

// HotBufferError is the error type surfaced to the REST layer by every
// HotBuffer implementation. It mirrors the Rust enum and preserves the exact
// error message format ("hot buffer unavailable: ...", "hot buffer transport
// error: ...").
type HotBufferError struct {
	Kind    HotBufferErrorKind
	Message string
	Cause   error
}

func (e *HotBufferError) Error() string {
	switch e.Kind {
	case HotBufferErrorUnavailable:
		return fmt.Sprintf("hot buffer unavailable: %s", e.Message)
	case HotBufferErrorTransport:
		return fmt.Sprintf("hot buffer transport error: %s", e.Message)
	default:
		return e.Message
	}
}

func (e *HotBufferError) Unwrap() error { return e.Cause }

// IsHotBufferErrorKind reports whether err is a *HotBufferError of the given
// kind. Useful for tests asserting on the discriminant.
func IsHotBufferErrorKind(err error, kind HotBufferErrorKind) bool {
	var he *HotBufferError
	if !errors.As(err, &he) {
		return false
	}
	return he.Kind == kind
}

// NewUnavailableError builds a HotBufferError::Unavailable.
func NewUnavailableError(format string, args ...any) *HotBufferError {
	return &HotBufferError{Kind: HotBufferErrorUnavailable, Message: fmt.Sprintf(format, args...)}
}

// NewTransportError builds a HotBufferError::Transport.
func NewTransportError(format string, args ...any) *HotBufferError {
	return &HotBufferError{Kind: HotBufferErrorTransport, Message: fmt.Sprintf(format, args...)}
}

// HotBuffer is the public contract every hot-buffer backend implements.
type HotBuffer interface {
	// ID is a stable identifier (`"kafka"`, `"nats"`, `"noop"`) used in
	// logs and Prometheus labels.
	ID() string

	// EnsureTopic makes sure the topic backing streamID exists.
	//
	//   - For Kafka, this calls the AdminClient with `partitions` and
	//     `replication.factor=1` so subsequent producers don't auto-create
	//     with the broker default partition count.
	//   - For NATS, this is a no-op: subjects are created on the first
	//     publish.
	EnsureTopic(ctx context.Context, streamID uuid.UUID, partitions int32) error

	// Publish posts a single event payload. `key` is used for partitioning
	// when the backend supports it (Kafka). NATS ignores it; pass the
	// empty string when no partition key is available.
	Publish(ctx context.Context, streamID uuid.UUID, key string, payload []byte) error

	// ApplyStreamType applies a Foundry-parity stream type / compression
	// to the producer before subsequent Publish calls. Backends that don't
	// expose tunables (NATS, Noop) ignore the call.
	ApplyStreamType(ctx context.Context, streamID uuid.UUID, streamType StreamType, compression bool) error
}

// TopicFor composes the conventional topic / subject name for a stream so
// every backend uses the same naming scheme. Mirrors topic_for in Rust.
func TopicFor(streamID uuid.UUID) string {
	return fmt.Sprintf("openfoundry.streams.%s", streamID)
}

// NoopHotBuffer is the fallback hot buffer used when neither NATS nor Kafka
// are configured. Logs and discards every publish so the REST control plane
// can still boot in degraded mode (e.g. CI smoke tests against an in-memory
// Postgres).
type NoopHotBuffer struct {
	logger *slog.Logger
}

// NewNoopHotBuffer builds a Noop hot buffer. Pass nil to use the default
// slog logger.
func NewNoopHotBuffer(logger *slog.Logger) *NoopHotBuffer {
	if logger == nil {
		logger = slog.Default()
	}
	return &NoopHotBuffer{logger: logger}
}

// ID implements HotBuffer.
func (b *NoopHotBuffer) ID() string { return "noop" }

// EnsureTopic implements HotBuffer.
func (b *NoopHotBuffer) EnsureTopic(_ context.Context, streamID uuid.UUID, _ int32) error {
	b.logger.Debug("noop hot buffer: ensure_topic", slog.String("stream_id", streamID.String()))
	return nil
}

// Publish implements HotBuffer.
func (b *NoopHotBuffer) Publish(_ context.Context, streamID uuid.UUID, _ string, payload []byte) error {
	b.logger.Debug("noop hot buffer: publish dropped",
		slog.String("stream_id", streamID.String()),
		slog.Int("bytes", len(payload)),
	)
	return nil
}

// ApplyStreamType implements HotBuffer. No-op for Noop / NATS.
func (b *NoopHotBuffer) ApplyStreamType(_ context.Context, _ uuid.UUID, _ StreamType, _ bool) error {
	return nil
}

// natsPublisher is the minimal slice of *nats.Conn that NatsHotBuffer uses.
// Defining it here lets tests inject a stub publisher without standing up a
// real NATS server.
type natsPublisher interface {
	Publish(subject string, data []byte) error
}

// NatsHotBuffer is the NATS Core implementation of HotBuffer.
//
// Subjects are created lazily on the first publish, so EnsureTopic is a
// no-op. Publishes go through the same *nats.Conn used by the gRPC routing
// facade (passed in at construction time so we don't open a second TCP
// connection).
type NatsHotBuffer struct {
	client natsPublisher
}

// ConnectNatsHotBuffer dials a NATS Core server and returns a hot buffer
// publishing through the resulting connection. Mirrors NatsHotBuffer::connect
// in Rust.
func ConnectNatsHotBuffer(url string) (*NatsHotBuffer, error) {
	conn, err := nats.Connect(url)
	if err != nil {
		return nil, NewUnavailableError("could not connect to NATS at %s: %s", url, err.Error())
	}
	return &NatsHotBuffer{client: conn}, nil
}

// NewNatsHotBufferFromConn wraps an already-connected *nats.Conn (used by
// tests and by the gRPC router which has already paid the connection cost).
// Mirrors NatsHotBuffer::from_client in Rust.
func NewNatsHotBufferFromConn(conn *nats.Conn) *NatsHotBuffer {
	return &NatsHotBuffer{client: conn}
}

// newNatsHotBufferFromPublisher is the test seam for swapping in a stub
// publisher.
func newNatsHotBufferFromPublisher(p natsPublisher) *NatsHotBuffer {
	return &NatsHotBuffer{client: p}
}

// ID implements HotBuffer.
func (b *NatsHotBuffer) ID() string { return "nats" }

// EnsureTopic implements HotBuffer. NATS subjects are namespaces, not
// first-class entities, so the first publish materialises them with no setup
// cost.
func (b *NatsHotBuffer) EnsureTopic(_ context.Context, _ uuid.UUID, _ int32) error {
	return nil
}

// Publish implements HotBuffer.
func (b *NatsHotBuffer) Publish(_ context.Context, streamID uuid.UUID, _ string, payload []byte) error {
	subject := TopicFor(streamID)
	if err := b.client.Publish(subject, payload); err != nil {
		return NewTransportError("%s", err.Error())
	}
	return nil
}

// ApplyStreamType implements HotBuffer. No-op for NATS.
func (b *NatsHotBuffer) ApplyStreamType(_ context.Context, _ uuid.UUID, _ StreamType, _ bool) error {
	return nil
}
