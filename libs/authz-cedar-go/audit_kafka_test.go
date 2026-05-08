package cedarauthz_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
)

// capturingPublisher implements databus.Publisher for tests. It records
// every Publish call so we can assert key/payload/lineage shape without
// spinning up a real broker.
type capturingPublisher struct {
	mu       sync.Mutex
	calls    []capturedPublish
	failNext bool
}

type capturedPublish struct {
	Topic   string
	Key     []byte
	Payload []byte
	Lineage *databus.OpenLineageHeaders
}

func (p *capturingPublisher) Publish(_ context.Context, topic string, key, payload []byte, lineage *databus.OpenLineageHeaders) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failNext {
		p.failNext = false
		return errors.New("simulated broker failure")
	}
	p.calls = append(p.calls, capturedPublish{
		Topic: topic, Key: append([]byte(nil), key...),
		Payload: append([]byte(nil), payload...),
		Lineage: lineage,
	})
	return nil
}

func (p *capturingPublisher) Flush(context.Context) error { return nil }
func (p *capturingPublisher) Close() error                { return nil }

func (p *capturingPublisher) snapshot() []capturedPublish {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]capturedPublish, len(p.calls))
	copy(out, p.calls)
	return out
}

// ─── Wire-format pinning ────────────────────────────────────────────

func TestKafkaAuditTopicConstant(t *testing.T) {
	t.Parallel()
	// Locked. Changing this breaks every Rust producer/consumer.
	assert.Equal(t, "audit.authz.v1", cedarauthz.KafkaAuditTopic)
}

func TestKafkaSinkPublishesEvent(t *testing.T) {
	t.Parallel()
	pub := &capturingPublisher{}
	sink := cedarauthz.NewKafkaAuditSinkDefault(pub)

	tenant := "acme"
	ev := cedarauthz.AuthzAuditEvent{
		Timestamp: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		Principal: `User::"alice"`,
		Action:    `Action::"read"`,
		Resource:  `Dataset::"ds-1"`,
		Decision:  "allow",
		Tenant:    &tenant,
		PolicyIDs: []string{"permit-cleared-readers"},
	}
	sink.Emit(context.Background(), ev)

	require.Eventually(t, func() bool {
		return len(pub.snapshot()) == 1
	}, time.Second, time.Millisecond)
	got := pub.snapshot()[0]
	assert.Equal(t, "audit.authz.v1", got.Topic)
	assert.Equal(t, []byte(`User::"alice"`), got.Key,
		"partition key MUST be the principal EntityUID — guarantees per-user partition affinity")

	// Payload round-trips back into AuthzAuditEvent with snake_case fields.
	var roundTrip cedarauthz.AuthzAuditEvent
	require.NoError(t, json.Unmarshal(got.Payload, &roundTrip))
	assert.Equal(t, "allow", roundTrip.Decision)
	assert.Equal(t, ev.Principal, roundTrip.Principal)

	// OpenLineage headers locked.
	require.NotNil(t, got.Lineage)
	assert.Equal(t, "of://authz", got.Lineage.Namespace)
	assert.Equal(t, "authz.decide", got.Lineage.JobName)
	assert.Equal(t, "https://github.com/unnamedlab/OpenFoundry/libs/authz-cedar", got.Lineage.Producer)
	require.NotNil(t, got.Lineage.EventTime)
	assert.True(t, got.Lineage.EventTime.Equal(ev.Timestamp))
}

// Publish failure is silent (logged at WARN) and never blocks the
// caller. Re-emit after a failure must succeed (sink isn't poisoned).
func TestKafkaSinkSwallowsPublishError(t *testing.T) {
	t.Parallel()
	pub := &capturingPublisher{failNext: true}
	sink := cedarauthz.NewKafkaAuditSinkDefault(pub)

	sink.Emit(context.Background(), cedarauthz.AuthzAuditEvent{
		Timestamp: time.Now(), Principal: "P", Action: "A", Resource: "R", Decision: "deny",
	})
	// Drain — first emission was simulated to fail.
	time.Sleep(10 * time.Millisecond)
	assert.Empty(t, pub.snapshot(), "first call failed → no record persisted")

	// Subsequent emissions succeed.
	sink.Emit(context.Background(), cedarauthz.AuthzAuditEvent{
		Timestamp: time.Now(), Principal: "P2", Action: "A", Resource: "R", Decision: "allow",
	})
	require.Eventually(t, func() bool {
		return len(pub.snapshot()) == 1
	}, time.Second, time.Millisecond)
}

// Topic getter exposes the configured topic for /metrics + tests.
func TestKafkaSinkTopicGetter(t *testing.T) {
	t.Parallel()
	pub := &capturingPublisher{}
	sink := cedarauthz.NewKafkaAuditSink(pub, "custom.topic")
	assert.Equal(t, "custom.topic", sink.Topic())

	defaultSink := cedarauthz.NewKafkaAuditSinkDefault(pub)
	assert.Equal(t, cedarauthz.KafkaAuditTopic, defaultSink.Topic())
}
