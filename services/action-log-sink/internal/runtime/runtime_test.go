package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	databus "github.com/openfoundry/openfoundry-go/libs/event-bus-data"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/config"
	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
)

// fakeSubscriber serves a scripted sequence of messages. After all
// messages are drained, Poll blocks until ctx is cancelled to mimic
// real Kafka behaviour. CommitMessages remembers every commit so
// tests can assert post-flush state.
type fakeSubscriber struct {
	mu          sync.Mutex
	queue       []*databus.DataMessage
	commits     [][]*databus.DataMessage
	pollErr     error
	commitErr   error
}

func (f *fakeSubscriber) Poll(ctx context.Context) (*databus.DataMessage, error) {
	f.mu.Lock()
	if f.pollErr != nil {
		err := f.pollErr
		f.pollErr = nil
		f.mu.Unlock()
		return nil, err
	}
	if len(f.queue) > 0 {
		msg := f.queue[0]
		f.queue = f.queue[1:]
		f.mu.Unlock()
		return msg, nil
	}
	f.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (f *fakeSubscriber) CommitMessages(_ context.Context, msgs []*databus.DataMessage) error {
	if f.commitErr != nil {
		return f.commitErr
	}
	cp := append([]*databus.DataMessage(nil), msgs...)
	f.mu.Lock()
	f.commits = append(f.commits, cp)
	f.mu.Unlock()
	return nil
}

func (f *fakeSubscriber) CommitOffsets(context.Context) error { return nil }
func (f *fakeSubscriber) Close() error                        { return nil }

// fakeWriter records every batch it receives.
type fakeWriter struct {
	mu      sync.Mutex
	batches [][]envelope.ActionEnvelope
	err     error
}

func (w *fakeWriter) Append(_ context.Context, batch []envelope.ActionEnvelope) error {
	if w.err != nil {
		return w.err
	}
	cp := append([]envelope.ActionEnvelope(nil), batch...)
	w.mu.Lock()
	w.batches = append(w.batches, cp)
	w.mu.Unlock()
	return nil
}
func (w *fakeWriter) Close() error { return nil }

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newMsg(offset int, body string) *databus.DataMessage {
	return &databus.DataMessage{
		Topic: "ontology.actions.applied.v1", Partition: 0,
		Offset: int64(offset), Value: []byte(body),
	}
}

const goodBody = `{"event_id":"e1","action_type_id":"at","action_name":"approve","object_type_id":"ot","tenant":"default","actor_sub":"a","status":"applied","applied_at_ms":1700000000000}`

func TestRun_flushesByMaxRecords(t *testing.T) {
	t.Parallel()
	sub := &fakeSubscriber{queue: []*databus.DataMessage{
		newMsg(0, goodBody), newMsg(1, goodBody), newMsg(2, goodBody),
	}}
	w := &fakeWriter{}
	m := NewMetrics()
	cfg := &config.Config{BatchPolicy: config.BatchPolicy{MaxRecords: 3, MaxWait: time.Hour}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, sub, w, m, discardLogger()) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		flushed := len(w.batches) == 1
		w.mu.Unlock()
		if flushed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.batches) != 1 {
		t.Fatalf("expected 1 flush by MaxRecords, got %d", len(w.batches))
	}
	if len(w.batches[0]) != 3 {
		t.Errorf("flush size = %d, want 3", len(w.batches[0]))
	}
	if len(sub.commits) == 0 || len(sub.commits[0]) != 3 {
		t.Errorf("commits = %v", sub.commits)
	}
}

func TestRun_poisonRecordsCommittedNotAppended(t *testing.T) {
	t.Parallel()
	sub := &fakeSubscriber{queue: []*databus.DataMessage{
		newMsg(0, goodBody),
		newMsg(1, "{not json"),
		newMsg(2, goodBody),
	}}
	w := &fakeWriter{}
	m := NewMetrics()
	cfg := &config.Config{BatchPolicy: config.BatchPolicy{MaxRecords: 3, MaxWait: time.Hour}}

	// MaxRecords counts only good records → flush triggers when 2 goods buffered,
	// but 1 poison sits in the offset-commit list. Use MaxRecords=2.
	cfg.BatchPolicy.MaxRecords = 2

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, sub, w, m, discardLogger()) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		w.mu.Lock()
		flushed := len(w.batches) >= 1
		w.mu.Unlock()
		if flushed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.batches) == 0 {
		t.Fatal("expected at least one flush")
	}
	// First flush contains both good records; poison was committed as offset only.
	if len(w.batches[0]) != 2 {
		t.Errorf("first batch size = %d, want 2 (poison excluded)", len(w.batches[0]))
	}
	// All 3 offsets must have been committed.
	totalCommitted := 0
	for _, c := range sub.commits {
		totalCommitted += len(c)
	}
	if totalCommitted < 3 {
		t.Errorf("expected 3 offset commits across all flushes, got %d", totalCommitted)
	}
}

func TestRun_writerErrorPreservesBatch(t *testing.T) {
	t.Parallel()
	sub := &fakeSubscriber{queue: []*databus.DataMessage{
		newMsg(0, goodBody), newMsg(1, goodBody),
	}}
	wErr := errors.New("adapter exploded")
	w := &fakeWriter{err: wErr}
	m := NewMetrics()
	cfg := &config.Config{BatchPolicy: config.BatchPolicy{MaxRecords: 2, MaxWait: time.Hour}}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	gotErr := Run(ctx, cfg, sub, w, m, discardLogger())

	if !errors.Is(gotErr, wErr) {
		t.Fatalf("Run should return writer error, got %v", gotErr)
	}
	if len(sub.commits) != 0 {
		t.Errorf("offsets must NOT be committed on writer failure, got %d commits", len(sub.commits))
	}
}

func TestRun_ctxCancelFlushesPending(t *testing.T) {
	t.Parallel()
	sub := &fakeSubscriber{queue: []*databus.DataMessage{newMsg(0, goodBody)}}
	w := &fakeWriter{}
	m := NewMetrics()
	cfg := &config.Config{BatchPolicy: config.BatchPolicy{MaxRecords: 100, MaxWait: time.Hour}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg, sub, w, m, discardLogger()) }()

	// Give the loop a moment to pull the single message into the buffer
	// (without ever hitting MaxRecords or MaxWait).
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.batches) != 1 || len(w.batches[0]) != 1 {
		t.Errorf("ctx cancel did not trigger final flush: batches=%v", w.batches)
	}
}
