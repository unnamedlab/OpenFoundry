package approvals

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func freshRequest() *ApprovalRequest {
	deadline := time.Now().Add(24 * time.Hour).UTC()
	return New(uuid.New(), "acme", "Promote", []string{"alice"}, json.RawMessage(`{"x":1}`), uuid.New(), &deadline)
}

func TestApprovalParseRoundTrips(t *testing.T) {
	t.Parallel()
	for _, s := range []ApprovalRequestState{StatePending, StateApproved, StateRejected, StateExpired, StateEscalated} {
		got, err := ParseState(string(s))
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		if got != s {
			t.Fatalf("got %s want %s", got, s)
		}
	}
}

func TestApprovalTerminalClassification(t *testing.T) {
	t.Parallel()
	if !StateApproved.IsTerminal() || !StateRejected.IsTerminal() || !StateExpired.IsTerminal() {
		t.Fatal("approved/rejected/expired must be terminal")
	}
	if StatePending.IsTerminal() || StateEscalated.IsTerminal() {
		t.Fatal("pending/escalated must not be terminal")
	}
}

func TestApprovalAllowedTransitions(t *testing.T) {
	t.Parallel()
	allowed := []struct{ from, to ApprovalRequestState }{
		{StatePending, StateApproved}, {StatePending, StateRejected},
		{StatePending, StateExpired}, {StatePending, StateEscalated},
		{StateEscalated, StateApproved}, {StateEscalated, StateRejected},
		{StateEscalated, StateExpired},
	}
	for _, tr := range allowed {
		if !tr.from.CanTransitionTo(tr.to) {
			t.Fatalf("%s → %s should be allowed", tr.from, tr.to)
		}
	}
}

func TestApprovalForbiddenTransitions(t *testing.T) {
	t.Parallel()
	forbidden := []struct{ from, to ApprovalRequestState }{
		{StateApproved, StatePending},
		{StateRejected, StatePending},
		{StateExpired, StatePending},
		{StateApproved, StateRejected},
		{StateApproved, StateExpired},
		{StateApproved, StateEscalated},
	}
	for _, tr := range forbidden {
		if tr.from.CanTransitionTo(tr.to) {
			t.Fatalf("%s → %s should be forbidden", tr.from, tr.to)
		}
	}
}

func TestApprovalApproveRecordsDeciderAndClearsDeadline(t *testing.T) {
	t.Parallel()
	req := freshRequest()
	if req.ExpiresAtField == nil {
		t.Fatal("setup: expires_at must be set")
	}
	comment := "LGTM"
	if err := req.Apply(Event{Kind: EventApprove, DecidedBy: "alice", Comment: &comment}); err != nil {
		t.Fatal(err)
	}
	if req.StateField != StateApproved {
		t.Fatalf("got %s", req.StateField)
	}
	if req.DecidedBy == nil || *req.DecidedBy != "alice" {
		t.Fatalf("decided_by %v", req.DecidedBy)
	}
	if req.Comment == nil || *req.Comment != "LGTM" {
		t.Fatalf("comment %v", req.Comment)
	}
	if req.DecidedAt == nil {
		t.Fatal("decided_at must be set")
	}
	if req.ExpiresAtField != nil {
		t.Fatal("expires_at must be cleared")
	}
}

func TestApprovalRejectWithoutComment(t *testing.T) {
	t.Parallel()
	req := freshRequest()
	if err := req.Apply(Event{Kind: EventReject, DecidedBy: "bob"}); err != nil {
		t.Fatal(err)
	}
	if req.StateField != StateRejected {
		t.Fatalf("got %s", req.StateField)
	}
	if req.Comment != nil {
		t.Fatal("comment must remain nil")
	}
}

func TestApprovalExpireSetsDecidedAt(t *testing.T) {
	t.Parallel()
	req := freshRequest()
	when := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	if err := req.Apply(Event{Kind: EventExpire, ExpiredAt: when}); err != nil {
		t.Fatal(err)
	}
	if req.StateField != StateExpired {
		t.Fatalf("got %s", req.StateField)
	}
	if req.DecidedBy != nil {
		t.Fatal("decided_by must remain nil on expire")
	}
	if req.DecidedAt == nil || !req.DecidedAt.Equal(when) {
		t.Fatalf("decided_at %v", req.DecidedAt)
	}
	if req.ExpiresAtField != nil {
		t.Fatal("expires_at must be cleared")
	}
}

func TestApprovalEscalatedCanStillBeDecided(t *testing.T) {
	t.Parallel()
	req := freshRequest()
	if err := req.Apply(Event{Kind: EventEscalate, EscalatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if req.StateField != StateEscalated {
		t.Fatalf("got %s", req.StateField)
	}
	if req.ExpiresAtField == nil {
		t.Fatal("escalation must keep expires_at")
	}
	if err := req.Apply(Event{Kind: EventApprove, DecidedBy: "carol"}); err != nil {
		t.Fatal(err)
	}
	if req.StateField != StateApproved {
		t.Fatalf("got %s", req.StateField)
	}
}

func TestApprovalInvalidEventInTerminalRejected(t *testing.T) {
	t.Parallel()
	req := freshRequest()
	_ = req.Apply(Event{Kind: EventApprove, DecidedBy: "alice"})
	if err := req.Apply(Event{Kind: EventApprove, DecidedBy: "alice"}); err == nil {
		t.Fatal("re-approve of approved must fail")
	}
}

func TestApprovalSerdeRoundTrip(t *testing.T) {
	t.Parallel()
	req := freshRequest()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var back ApprovalRequest
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatal(err)
	}
	if back.StateField != req.StateField {
		t.Fatalf("state lost in round-trip: %s vs %s", back.StateField, req.StateField)
	}
}

func TestDeriveOutboxEventIDIsDeterministicV5(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	a := DeriveOutboxEventID(id, "requested")
	b := DeriveOutboxEventID(id, "requested")
	c := DeriveOutboxEventID(id, "completed")
	if a != b {
		t.Fatal("must be stable")
	}
	if a == c {
		t.Fatal("kind must distinguish")
	}
	if a.Version() != 5 {
		t.Fatal("expected v5")
	}
}
