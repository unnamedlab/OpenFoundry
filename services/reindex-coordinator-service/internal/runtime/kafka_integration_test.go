//go:build integration

// Kafka testcontainer integration test for the Coordinator consumer
// loop. Boots a confluentinc/cp-kafka container via
// testcontainers-go/modules/kafka, plus the existing libs/testing
// Postgres harness for the JobRepo + processed_events tables, and
// drives the full produce → consume → run-job → publish → commit
// path.
//
// Cassandra is replaced with an in-memory scripted scanner so the
// test exercises *only* the runtime wiring; the live Cassandra
// scanner is covered by internal/scan/cassandra_integration_test.go.
//
// Opt-in via `go test -tags=integration ./...`.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	kafka "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	testingx "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/event"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/scan"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/state"
	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/topics"
)

// kafkaHarness wraps the running cp-kafka container plus its
// bootstrap broker list. Stop() is wired in t.Cleanup automatically.
type kafkaHarness struct {
	Container *tckafka.KafkaContainer
	Brokers   []string
}

func bootKafka(ctx context.Context, t *testing.T) *kafkaHarness {
	t.Helper()
	container, err := tckafka.Run(ctx,
		"confluentinc/cp-kafka:7.7.0",
		tckafka.WithClusterID("test-cluster"),
	)
	if err != nil {
		t.Fatalf("kafka container start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = testcontainers.TerminateContainer(container)
		_ = stopCtx
	})
	brokers, err := container.Brokers(ctx)
	if err != nil {
		t.Fatalf("kafka brokers: %v", err)
	}
	return &kafkaHarness{Container: container, Brokers: brokers}
}

func createTopic(t *testing.T, brokers []string, topic string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := kafka.DialContext(ctx, "tcp", brokers[0])
	if err != nil {
		t.Fatalf("dial kafka: %v", err)
	}
	defer conn.Close()
	if err := conn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		t.Fatalf("create topic %s: %v", topic, err)
	}
}

func produceMessage(t *testing.T, brokers []string, topic string, key, value []byte) {
	t.Helper()
	w := &kafka.Writer{Addr: kafka.TCP(brokers...), Topic: topic, RequiredAcks: kafka.RequireAll, Balancer: &kafka.LeastBytes{}}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.WriteMessages(ctx, kafka.Message{Key: key, Value: value}); err != nil {
		t.Fatalf("produce kafka message: %v", err)
	}
}

// readNextOnTopic spins up an ephemeral reader (NOT in the
// coordinator's group) and returns the next published message on
// topic, or context.DeadlineExceeded after timeout.
func readNextOnTopic(t *testing.T, brokers []string, topic string, timeout time.Duration) (kafka.Message, error) {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers, Topic: topic, Partition: 0, MinBytes: 1, MaxBytes: 10e6, MaxWait: 100 * time.Millisecond,
	})
	defer r.Close()
	if err := r.SetOffset(kafka.FirstOffset); err != nil {
		return kafka.Message{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.ReadMessage(ctx)
}

// expectGroupCommitted reads on the coordinator's consumer group; if
// the offset has been committed the read times out (no new messages
// for the group).
func expectGroupCommitted(t *testing.T, brokers []string, topic, group string) {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers, GroupID: group, Topic: topic, MinBytes: 1, MaxBytes: 10e6, MaxWait: 100 * time.Millisecond, CommitInterval: 0,
	})
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	msg, err := r.FetchMessage(ctx)
	if err == nil {
		t.Fatalf("expected committed group offset for %s/%s, but re-read offset %d value=%s", topic, group, msg.Offset, string(msg.Value))
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded while checking committed offset, got %v", err)
	}
}

func bootRepoForRuntime(t *testing.T) *repo.JobRepo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	h := testingx.BootPostgres(ctx, t)
	require.NoError(t, repo.Migrate(ctx, h.Pool))
	return repo.NewJobRepo(h.Pool)
}

func newQuietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestRunDrivesJobToCompletionAndCommitsOffset is the headline test:
// produce a valid requested.v1 message, run the coordinator until it
// completes the job, assert that the completed.v1 record is on the
// wire, and assert that the consumer group has committed the input
// offset (so a restart wouldn't re-deliver).
func TestRunDrivesJobToCompletionAndCommitsOffset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	kh := bootKafka(ctx, t)
	jobRepo := bootRepoForRuntime(t)

	requested := topics.OntologyReindexRequestedV1
	completed := topics.OntologyReindexCompletedV1
	dataPlane := topics.OntologyReindexV1
	createTopic(t, kh.Brokers, requested)
	createTopic(t, kh.Brokers, completed)
	createTopic(t, kh.Brokers, dataPlane)

	body, err := json.Marshal(event.ReindexRequestedV1{TenantID: "tenant-it", TypeID: strPtrIT("users"), RequestID: strPtrIT("req-it-1")})
	require.NoError(t, err)
	produceMessage(t, kh.Brokers, requested, []byte("k1"), body)

	// scripted scanner: one page, one record, no next token → terminal.
	scn := &scriptedScanner{pages: []scan.PageOutcome{
		{
			Records: []scan.ReindexRecord{{
				Tenant: "tenant-it", ID: uuid.NewString(), TypeID: "users", Version: 1, Payload: json.RawMessage(`{"a":1}`),
			}},
			Scanned:   1,
			NextToken: nil,
		},
	}}
	pubCfg := databus.NewConfig(kh.Brokers, databus.InsecureDev("reindex-coordinator-it"))
	publisher, err := databus.NewKafkaPublisher(pubCfg)
	require.NoError(t, err)
	defer publisher.Close()

	idem := repo.NewProcessedEventsStore(jobRepo.Pool())
	c := NewCoordinator(jobRepo, idem, scn, publisher, NewMetrics(prometheus.NewRegistry()), Throttle{}, "openfoundry-it", newQuietLogger())

	subCfg := databus.NewConfig(kh.Brokers, databus.InsecureDev("reindex-coordinator-it"))
	sub, err := databus.NewKafkaSubscriber(subCfg, ConsumerGroup, []string{requested})
	require.NoError(t, err)
	defer sub.Close()

	runCtx, runCancel := context.WithTimeout(ctx, 90*time.Second)
	defer runCancel()
	doneCh := make(chan error, 1)
	go func() { doneCh <- Run(runCtx, c, sub) }()

	// Wait for the data-plane record to land — proves the scan happened
	// and the publisher hit the wire.
	dataMsg, err := readNextOnTopic(t, kh.Brokers, dataPlane, 60*time.Second)
	require.NoError(t, err)
	assert.Contains(t, string(dataMsg.Value), `"tenant":"tenant-it"`)

	// Then the completed event.
	completedMsg, err := readNextOnTopic(t, kh.Brokers, completed, 60*time.Second)
	require.NoError(t, err)
	var completedEvt event.ReindexCompletedV1
	require.NoError(t, json.Unmarshal(completedMsg.Value, &completedEvt))
	assert.Equal(t, "completed", completedEvt.Status)
	assert.Equal(t, "tenant-it", completedEvt.TenantID)
	require.NotNil(t, completedEvt.RequestID)
	assert.Equal(t, "req-it-1", *completedEvt.RequestID)

	// Stop the loop and confirm the offset was committed.
	runCancel()
	select {
	case <-doneCh:
	case <-time.After(15 * time.Second):
		t.Fatal("runtime.Run did not return after cancel")
	}
	expectGroupCommitted(t, kh.Brokers, requested, ConsumerGroup)

	// JobRepo state machine must be at completed.
	jobID := event.DeriveJobID("tenant-it", strPtrIT("users"))
	rec, err := jobRepo.Load(ctx, jobID)
	require.NoError(t, err)
	assert.Equal(t, state.StatusCompleted, rec.Status)
	assert.Equal(t, int64(1), rec.Scanned)
}

// TestRunCommitsOffsetForMalformedPayload — a poison message must be
// committed (not retried forever). Mirrors the Rust contract that
// decode_error is treated as a successfully classified outcome.
func TestRunCommitsOffsetForMalformedPayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	kh := bootKafka(ctx, t)
	jobRepo := bootRepoForRuntime(t)

	requested := topics.OntologyReindexRequestedV1
	createTopic(t, kh.Brokers, requested)
	createTopic(t, kh.Brokers, topics.OntologyReindexV1)
	createTopic(t, kh.Brokers, topics.OntologyReindexCompletedV1)

	produceMessage(t, kh.Brokers, requested, []byte("k1"), []byte(`{not-json`))

	scn := &scriptedScanner{}
	pubCfg := databus.NewConfig(kh.Brokers, databus.InsecureDev("reindex-coordinator-it"))
	publisher, err := databus.NewKafkaPublisher(pubCfg)
	require.NoError(t, err)
	defer publisher.Close()

	idem := repo.NewProcessedEventsStore(jobRepo.Pool())
	c := NewCoordinator(jobRepo, idem, scn, publisher, NewMetrics(prometheus.NewRegistry()), Throttle{}, "ns", newQuietLogger())

	groupID := ConsumerGroup + "-malformed"
	subCfg := databus.NewConfig(kh.Brokers, databus.InsecureDev("reindex-coordinator-it"))
	sub, err := databus.NewKafkaSubscriber(subCfg, groupID, []string{requested})
	require.NoError(t, err)
	defer sub.Close()

	runCtx, runCancel := context.WithTimeout(ctx, 60*time.Second)
	defer runCancel()
	doneCh := make(chan error, 1)
	go func() { doneCh <- Run(runCtx, c, sub) }()

	// Wait long enough for the poll → decode_error → commit cycle.
	require.Eventually(t, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		r := kafka.NewReader(kafka.ReaderConfig{Brokers: kh.Brokers, GroupID: groupID, Topic: requested, MinBytes: 1, MaxBytes: 10e6, MaxWait: 100 * time.Millisecond, CommitInterval: 0})
		defer r.Close()
		_, err := r.FetchMessage(ctx)
		return errors.Is(err, context.DeadlineExceeded)
	}, 30*time.Second, 1*time.Second, "malformed payload offset must commit so it does not replay")

	runCancel()
	select {
	case <-doneCh:
	case <-time.After(15 * time.Second):
		t.Fatal("runtime.Run did not return after cancel")
	}
}

// TestRunReplaysOnUpsertError — an internal failure (here: pool
// closed) leaves the offset uncommitted so Kafka redelivers. Pins
// the at-least-once contract from the Rust impl.
func TestRunReplaysOnUpsertError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	kh := bootKafka(ctx, t)
	jobRepo := bootRepoForRuntime(t)

	requested := topics.OntologyReindexRequestedV1
	createTopic(t, kh.Brokers, requested)
	createTopic(t, kh.Brokers, topics.OntologyReindexV1)
	createTopic(t, kh.Brokers, topics.OntologyReindexCompletedV1)

	body, err := json.Marshal(event.ReindexRequestedV1{TenantID: "tenant-replay"})
	require.NoError(t, err)
	produceMessage(t, kh.Brokers, requested, []byte("k1"), body)

	// Closing the pool BEFORE the coordinator polls forces UpsertQueued
	// to error so processRequestMessage returns an error and the
	// commit is skipped.
	jobRepo.Pool().Close()

	scn := &scriptedScanner{}
	pubCfg := databus.NewConfig(kh.Brokers, databus.InsecureDev("reindex-coordinator-it"))
	publisher, err := databus.NewKafkaPublisher(pubCfg)
	require.NoError(t, err)
	defer publisher.Close()

	idem := repo.NewProcessedEventsStore(jobRepo.Pool())
	c := NewCoordinator(jobRepo, idem, scn, publisher, NewMetrics(prometheus.NewRegistry()), Throttle{}, "ns", newQuietLogger())

	groupID := ConsumerGroup + "-replay"
	subCfg := databus.NewConfig(kh.Brokers, databus.InsecureDev("reindex-coordinator-it"))
	sub, err := databus.NewKafkaSubscriber(subCfg, groupID, []string{requested})
	require.NoError(t, err)
	defer sub.Close()

	runCtx, runCancel := context.WithTimeout(ctx, 30*time.Second)
	defer runCancel()
	doneCh := make(chan error, 1)
	go func() { doneCh <- Run(runCtx, c, sub) }()

	// Wait for at least one error processing cycle so we know the
	// coordinator definitely saw the message and chose not to commit.
	time.Sleep(8 * time.Second)
	runCancel()
	select {
	case <-doneCh:
	case <-time.After(20 * time.Second):
		t.Fatal("runtime.Run did not return after cancel")
	}

	// Replay assertion: a brand-new reader on the same group must
	// re-deliver the original payload.
	r := kafka.NewReader(kafka.ReaderConfig{Brokers: kh.Brokers, GroupID: groupID, Topic: requested, MinBytes: 1, MaxBytes: 10e6, MaxWait: 250 * time.Millisecond, CommitInterval: 0})
	defer r.Close()
	fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer fetchCancel()
	msg, err := r.FetchMessage(fetchCtx)
	require.NoError(t, err)
	assert.Equal(t, "tenant-replay", string(jsonField(t, msg.Value, "tenant_id")))
}

// jsonField returns the raw value of a top-level field in body or
// fails the test if the field is missing / not a string.
func jsonField(t *testing.T, body []byte, key string) string {
	t.Helper()
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw))
	v, ok := raw[key]
	require.Truef(t, ok, "missing field %s in %s", key, string(body))
	var s string
	require.NoError(t, json.Unmarshal(v, &s))
	return s
}

func strPtrIT(s string) *string { return &s }

// Compile-time guards: production *repo.JobRepo and the in-memory
// fake satisfy JobStore; *scan.CassandraScanner satisfies Scanner.
// Catches accidental signature drift in either package.
var (
	_ JobStore = (*repo.JobRepo)(nil)
	_ Scanner  = (*scan.CassandraScanner)(nil)
)

// emit only if test fails: avoid generic helper cleanup logs in CI.
var _ = strings.TrimSpace
