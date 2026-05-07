package controlbus

// kafka_live.go ports libs/event-bus-control/src/kafka_live.rs.
//
// Live Kafka I/O helpers for the data-connection plane. Three
// helpers back the real test_connection / discover_sources /
// query_virtual_table paths in the per-service connectors::kafka
// modules.
//
// Implementation notes:
//   - Rust uses BaseConsumer + tokio::task::spawn_blocking around
//     librdkafka. Go's segmentio/kafka-go is a pure-Go client with
//     a goroutine-friendly API, so we drop the spawn_blocking dance.
//     Default builds get the helpers without a native dep — the Rust
//     `cfg(feature = "kafka-rdkafka")` gate only existed to avoid
//     pulling librdkafka.
//   - All three helpers honour an inbound timeout (default 5 s) and
//     return error so they slot directly into the existing connector
//     error contract (errors bubble up to the HTTP layer as 502 Bad
//     Gateway-style messages).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	kafka "github.com/segmentio/kafka-go"
)

// DefaultKafkaTimeout is the timeout for metadata / watermark calls
// when the caller passes 0.
const DefaultKafkaTimeout = 5 * time.Second

// LiveTestOutcome is the outcome of a successful broker probe.
// Mirrors libs/event-bus-control/src/kafka_live.rs::LiveTestOutcome.
type LiveTestOutcome struct {
	TopicCount        int
	BrokerCount       int
	OriginatingBroker string
	LatencyMS         int64
}

// DiscoveredKafkaTopic is a single topic returned by DiscoverTopics.
type DiscoveredKafkaTopic struct {
	Name       string
	Partitions int64
}

// TestConnection probes the broker, returning cluster-level metadata.
// Read-only: dials a single broker, reads the cluster description.
//
// Pass timeout=0 to use DefaultKafkaTimeout.
func TestConnection(ctx context.Context, bootstrap string, timeout time.Duration) (LiveTestOutcome, error) {
	if timeout <= 0 {
		timeout = DefaultKafkaTimeout
	}
	started := time.Now()

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addr := firstBootstrap(bootstrap)
	conn, err := (&kafka.Dialer{Timeout: timeout, ClientID: "openfoundry-connector-test"}).
		DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return LiveTestOutcome{}, fmt.Errorf("kafka client init failed: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))
	parts, err := conn.ReadPartitions()
	if err != nil {
		return LiveTestOutcome{}, fmt.Errorf("kafka metadata fetch failed: %w", err)
	}
	brokers := map[string]struct{}{}
	topics := map[string]struct{}{}
	for _, p := range parts {
		topics[p.Topic] = struct{}{}
		brokers[p.Leader.Host+":"+itoa(p.Leader.Port)] = struct{}{}
		for _, r := range p.Replicas {
			brokers[r.Host+":"+itoa(r.Port)] = struct{}{}
		}
	}
	originating := addr
	if b := conn.Broker(); b.Host != "" {
		originating = b.Host + ":" + itoa(b.Port)
	}
	return LiveTestOutcome{
		TopicCount:        len(topics),
		BrokerCount:       len(brokers),
		OriginatingBroker: originating,
		LatencyMS:         time.Since(started).Milliseconds(),
	}, nil
}

// DiscoverTopics enumerates topics on the broker. Internal/system
// topics (those whose name starts with `__`, e.g. __consumer_offsets)
// are filtered out.
func DiscoverTopics(ctx context.Context, bootstrap string, timeout time.Duration) ([]DiscoveredKafkaTopic, error) {
	if timeout <= 0 {
		timeout = DefaultKafkaTimeout
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	addr := firstBootstrap(bootstrap)
	conn, err := (&kafka.Dialer{Timeout: timeout, ClientID: "openfoundry-connector-discover"}).
		DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("kafka client init failed: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(timeout))
	parts, err := conn.ReadPartitions()
	if err != nil {
		return nil, fmt.Errorf("kafka metadata fetch failed: %w", err)
	}
	counts := map[string]int{}
	for _, p := range parts {
		if strings.HasPrefix(p.Topic, "__") {
			continue
		}
		counts[p.Topic]++
	}
	out := make([]DiscoveredKafkaTopic, 0, len(counts))
	for name, n := range counts {
		out = append(out, DiscoveredKafkaTopic{Name: name, Partitions: int64(n)})
	}
	return out, nil
}

// TailMessages tails the last `limit` messages on partition 0 of
// `topic`. Looks up the high watermark, seeks to `max(low, high-limit)`,
// and reads until either `limit` rows are gathered or `timeout`
// elapses.
//
// Payloads are decoded as JSON when possible, otherwise wrapped as
// `{"value_utf8": "..."}` — same fall-back the Rust impl uses.
func TailMessages(ctx context.Context, bootstrap, topic string, limit int, timeout time.Duration) ([]json.RawMessage, error) {
	if timeout <= 0 {
		timeout = DefaultKafkaTimeout
	}
	if limit < 1 {
		limit = 1
	}

	// Resolve partition 0 watermarks via a direct broker connection.
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	dialer := &kafka.Dialer{
		Timeout:  timeout,
		ClientID: "openfoundry-tail-" + uuid.NewString(),
	}
	leader, err := dialer.DialLeader(dialCtx, "tcp", firstBootstrap(bootstrap), topic, 0)
	if err != nil {
		return nil, fmt.Errorf("kafka client init failed: %w", err)
	}
	_ = leader.SetDeadline(time.Now().Add(timeout))
	low, high, err := leader.ReadOffsets()
	_ = leader.Close()
	if err != nil {
		return nil, fmt.Errorf("kafka watermark fetch failed: %w", err)
	}
	if high <= low {
		return []json.RawMessage{}, nil
	}
	start := high - int64(limit)
	if start < low {
		start = low
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     splitBootstrap(bootstrap),
		Topic:       topic,
		Partition:   0,
		Dialer:      dialer,
		StartOffset: start,
		MaxWait:     200 * time.Millisecond,
	})
	defer reader.Close()
	if err := reader.SetOffset(start); err != nil {
		return nil, fmt.Errorf("kafka offset assignment failed: %w", err)
	}

	deadline := time.Now().Add(timeout)
	rows := make([]json.RawMessage, 0, limit)
	for len(rows) < limit && time.Now().Before(deadline) {
		fetchCtx, fcancel := context.WithDeadline(ctx, deadline)
		msg, err := reader.FetchMessage(fetchCtx)
		fcancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				break
			}
			return nil, fmt.Errorf("kafka poll error: %w", err)
		}
		rows = append(rows, decodeMessage(msg))
		if msg.Offset+1 >= high {
			break
		}
	}
	return rows, nil
}

func decodeMessage(m kafka.Message) json.RawMessage {
	var key any
	if m.Key != nil {
		key = string(m.Key)
	}
	var value any = nil
	if len(m.Value) > 0 {
		var v any
		if err := json.Unmarshal(m.Value, &v); err == nil {
			value = v
		} else {
			value = map[string]string{"value_utf8": string(m.Value)}
		}
	}
	out, _ := json.Marshal(map[string]any{
		"key":       key,
		"value":     value,
		"partition": m.Partition,
		"offset":    m.Offset,
	})
	return out
}

func firstBootstrap(bootstrap string) string {
	for _, part := range strings.Split(bootstrap, ",") {
		if s := strings.TrimSpace(part); s != "" {
			return s
		}
	}
	return bootstrap
}

func splitBootstrap(bootstrap string) []string {
	out := []string{}
	for _, part := range strings.Split(bootstrap, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{bootstrap}
	}
	return out
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
