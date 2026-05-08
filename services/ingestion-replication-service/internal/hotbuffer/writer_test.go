package hotbuffer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestTopicForMatchesRustFormat(t *testing.T) {
	// Mirrors `topic_for` in
	// services/ingestion-replication-service/src/event_streaming/domain/hot_buffer/mod.rs:
	// "openfoundry.streams.{stream_id}".
	id := uuid.MustParse("00000000-0000-0000-0000-0000000000aa")
	if got, want := TopicFor(id), "openfoundry.streams.00000000-0000-0000-0000-0000000000aa"; got != want {
		t.Errorf("TopicFor: got %q, want %q", got, want)
	}
}

func TestNoopHotBufferIsNoop(t *testing.T) {
	b := NewNoopHotBuffer(nil)
	if b.ID() != "noop" {
		t.Errorf("ID: got %q", b.ID())
	}
	if err := b.EnsureTopic(context.Background(), uuid.New(), 4); err != nil {
		t.Errorf("EnsureTopic: %v", err)
	}
	if err := b.Publish(context.Background(), uuid.New(), "key", []byte("payload")); err != nil {
		t.Errorf("Publish: %v", err)
	}
	if err := b.ApplyStreamType(context.Background(), uuid.New(), StreamTypeStandard, true); err != nil {
		t.Errorf("ApplyStreamType: %v", err)
	}
}

func TestHotBufferErrorMessageFormat(t *testing.T) {
	cases := []struct {
		err  *HotBufferError
		want string
	}{
		{NewUnavailableError("broker down"), "hot buffer unavailable: broker down"},
		{NewTransportError("publish failed: %s", "boom"), "hot buffer transport error: publish failed: boom"},
	}
	for _, c := range cases {
		if got := c.err.Error(); got != c.want {
			t.Errorf("Error: got %q, want %q", got, c.want)
		}
	}
}

func TestIsHotBufferErrorKind(t *testing.T) {
	if !IsHotBufferErrorKind(NewUnavailableError("x"), HotBufferErrorUnavailable) {
		t.Errorf("expected unavailable kind to match")
	}
	if IsHotBufferErrorKind(NewUnavailableError("x"), HotBufferErrorTransport) {
		t.Errorf("kinds should not cross-match")
	}
	if IsHotBufferErrorKind(errors.New("plain"), HotBufferErrorTransport) {
		t.Errorf("non-HotBufferError should not match")
	}
}

// stubNatsPublisher records Publish calls so we can verify the NATS hot buffer
// without a live NATS server.
type stubNatsPublisher struct {
	subject string
	data    []byte
	err     error
}

func (s *stubNatsPublisher) Publish(subject string, data []byte) error {
	s.subject = subject
	s.data = append([]byte(nil), data...)
	return s.err
}

func TestNatsHotBufferPublishesToTopicForSubject(t *testing.T) {
	stub := &stubNatsPublisher{}
	b := newNatsHotBufferFromPublisher(stub)
	if b.ID() != "nats" {
		t.Errorf("ID: got %q", b.ID())
	}
	if err := b.EnsureTopic(context.Background(), uuid.New(), 1); err != nil {
		t.Errorf("EnsureTopic should be a no-op, got %v", err)
	}
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	if err := b.Publish(context.Background(), id, "ignored-by-nats", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got, want := stub.subject, "openfoundry.streams.11111111-2222-3333-4444-555555555555"; got != want {
		t.Errorf("subject: got %q, want %q", got, want)
	}
	if string(stub.data) != "hello" {
		t.Errorf("payload: got %q", stub.data)
	}
}

func TestNatsHotBufferWrapsTransportError(t *testing.T) {
	stub := &stubNatsPublisher{err: errors.New("connection lost")}
	b := newNatsHotBufferFromPublisher(stub)
	err := b.Publish(context.Background(), uuid.New(), "", []byte("x"))
	if !IsHotBufferErrorKind(err, HotBufferErrorTransport) {
		t.Errorf("kind: got %v", err)
	}
	if !strings.Contains(err.Error(), "hot buffer transport error: connection lost") {
		t.Errorf("message: %v", err)
	}
}
