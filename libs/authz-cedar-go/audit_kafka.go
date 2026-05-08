package cedarauthz

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
)

// KafkaAuditTopic is the canonical topic for authorization decisions.
//
// Provisioned in
// `infra/k8s/platform/manifests/strimzi/topics-domain-v1.yaml`
// (12 partitions, RF=3, ISR=2).
const KafkaAuditTopic = "audit.authz.v1"

// OpenLineage facets attached to every emitted record. Locked to match
// the Rust impl byte-for-byte so downstream consumers (audit-trail,
// audit-compliance, audit-sink) see the same job/namespace/producer
// regardless of which runtime emitted the event.
const (
	olAuditNamespace = "of://authz"
	olAuditJobName   = "authz.decide"
	olAuditProducer  = "https://github.com/unnamedlab/OpenFoundry/libs/authz-cedar"
)

// KafkaAuthzAuditSink is the production [AuthzAuditSink].
//
// Two design points worth pinning down:
//
//   - **Fire-and-forget.** Emit returns immediately; the actual Publish
//     call runs on a detached goroutine so a slow broker can never stall
//     the request hot path. Failures are swallowed at WARN level.
//   - **Partition-by-principal.** The Kafka record key is the principal
//     EntityUID string. All decisions for a given user therefore land
//     on the same partition, which lets downstream sinks reconstruct a
//     per-user timeline without a global sort.
type KafkaAuthzAuditSink struct {
	publisher databus.Publisher
	topic     string
}

// NewKafkaAuditSink builds a sink that publishes to `topic` via
// `publisher`. Pass [KafkaAuditTopic] for the canonical topic.
func NewKafkaAuditSink(publisher databus.Publisher, topic string) *KafkaAuthzAuditSink {
	return &KafkaAuthzAuditSink{publisher: publisher, topic: topic}
}

// NewKafkaAuditSinkDefault is a convenience constructor wired to the
// canonical [KafkaAuditTopic].
func NewKafkaAuditSinkDefault(publisher databus.Publisher) *KafkaAuthzAuditSink {
	return NewKafkaAuditSink(publisher, KafkaAuditTopic)
}

// Topic returns the topic this sink writes to (exposed for tests / metrics).
func (s *KafkaAuthzAuditSink) Topic() string { return s.topic }

// Emit serialises the event to JSON and publishes it asynchronously.
//
// Implements [AuthzAuditSink].
func (s *KafkaAuthzAuditSink) Emit(ctx context.Context, event AuthzAuditEvent) {
	publisher := s.publisher
	topic := s.topic
	go func() {
		payload, err := json.Marshal(event)
		if err != nil {
			slog.Warn("authz.audit.kafka: failed to serialise AuthzAuditEvent — dropping",
				slog.String("error", err.Error()),
				slog.String("principal", event.Principal),
				slog.String("action", event.Action),
			)
			return
		}

		headers := databus.NewOpenLineageHeaders(
			olAuditNamespace, olAuditJobName,
			uuid.NewString(), olAuditProducer,
		).WithEventTime(event.Timestamp)

		key := []byte(event.Principal)
		if err := publisher.Publish(detachContext(ctx), topic, key, payload, &headers); err != nil {
			slog.Warn("authz.audit.kafka: kafka publish failed for authz audit event — dropping",
				slog.String("error", err.Error()),
				slog.String("topic", topic),
				slog.String("principal", event.Principal),
				slog.String("action", event.Action),
				slog.String("decision", event.Decision),
			)
		}
	}()
}
