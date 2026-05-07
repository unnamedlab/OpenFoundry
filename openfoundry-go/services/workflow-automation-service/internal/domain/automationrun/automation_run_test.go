package automationrun

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func freshRun() *AutomationRun {
	return New(uuid.New(), uuid.New(), uuid.New(), uuid.New(), nil)
}

func TestParseRoundTripsEveryState(t *testing.T) {
	t.Parallel()
	for _, s := range []State{StateQueued, StateRunning, StateSuspended, StateCompensating, StateCompleted, StateFailed} {
		got, err := ParseState(string(s))
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		if got != s {
			t.Fatalf("got %s want %s", got, s)
		}
	}
}

func TestParseRejectsUnknown(t *testing.T) {
	t.Parallel()
	if _, err := ParseState("escalated"); err == nil {
		t.Fatal("expected error")
	}
}

func TestTerminalClassification(t *testing.T) {
	t.Parallel()
	if !StateCompleted.IsTerminal() || !StateFailed.IsTerminal() {
		t.Fatal("completed + failed must be terminal")
	}
	for _, s := range []State{StateQueued, StateRunning, StateSuspended, StateCompensating} {
		if s.IsTerminal() {
			t.Fatalf("%s must not be terminal", s)
		}
	}
}

func TestAllowedTransitions(t *testing.T) {
	t.Parallel()
	allowed := []struct{ from, to State }{
		{StateQueued, StateRunning}, {StateQueued, StateFailed},
		{StateRunning, StateCompleted}, {StateRunning, StateFailed},
		{StateRunning, StateSuspended}, {StateRunning, StateCompensating},
		{StateSuspended, StateRunning}, {StateSuspended, StateFailed},
		{StateCompensating, StateFailed},
	}
	for _, tr := range allowed {
		if !tr.from.CanTransitionTo(tr.to) {
			t.Fatalf("%s → %s should be allowed", tr.from, tr.to)
		}
	}
}

func TestForbiddenTransitions(t *testing.T) {
	t.Parallel()
	forbidden := []struct{ from, to State }{
		{StateRunning, StateQueued},
		{StateSuspended, StateQueued},
		{StateCompleted, StateRunning},
		{StateFailed, StateRunning},
		{StateCompleted, StateFailed},
		{StateCompensating, StateRunning},
		{StateCompensating, StateCompleted},
		{StateQueued, StateSuspended},
		{StateQueued, StateCompleted},
		{StateQueued, StateCompensating},
	}
	for _, tr := range forbidden {
		if tr.from.CanTransitionTo(tr.to) {
			t.Fatalf("%s → %s should be forbidden", tr.from, tr.to)
		}
	}
}

func TestSelfTransitionsAreIdempotent(t *testing.T) {
	t.Parallel()
	for _, s := range []State{StateQueued, StateRunning, StateSuspended, StateCompensating, StateCompleted, StateFailed} {
		if !s.CanTransitionTo(s) {
			t.Fatalf("%s should be self-transitionable", s)
		}
	}
}

func TestHappyPathQueuedRunningCompleted(t *testing.T) {
	t.Parallel()
	run := freshRun()
	if err := run.Apply(ClaimEvent()); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if run.State != StateRunning {
		t.Fatalf("got %s", run.State)
	}
	if run.Attempts != 1 {
		t.Fatalf("attempts %d", run.Attempts)
	}
	if err := run.Apply(EffectCompletedEvent(json.RawMessage(`{"ok":true}`))); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if run.State != StateCompleted {
		t.Fatalf("got %s", run.State)
	}
	if run.LastError != nil {
		t.Fatal("last_error must be nil after success")
	}
	if run.ExpiresAtField != nil {
		t.Fatal("expires_at must be cleared on success")
	}
}

func TestPreFlightFailureJumpsQueuedToFailed(t *testing.T) {
	t.Parallel()
	run := freshRun()
	if err := run.Apply(PreFlightFailedEvent("missing action_id")); err != nil {
		t.Fatalf("pre-flight: %v", err)
	}
	if run.State != StateFailed {
		t.Fatalf("got %s", run.State)
	}
	if run.LastError == nil || *run.LastError != "missing action_id" {
		t.Fatalf("last_error %v", run.LastError)
	}
	if run.Attempts != 0 {
		t.Fatal("attempts should stay 0 — never claimed")
	}
}

func TestSuspendResumeCycle(t *testing.T) {
	t.Parallel()
	run := freshRun()
	_ = run.Apply(ClaimEvent())
	deadline := time.Now().Add(time.Hour).UTC()
	if err := run.Apply(SuspendEvent(&deadline)); err != nil {
		t.Fatal(err)
	}
	if run.State != StateSuspended {
		t.Fatalf("got %s", run.State)
	}
	if run.ExpiresAtField == nil || !run.ExpiresAtField.Equal(deadline) {
		t.Fatalf("expires_at %v", run.ExpiresAtField)
	}
	if err := run.Apply(ResumeEvent()); err != nil {
		t.Fatal(err)
	}
	if run.State != StateRunning {
		t.Fatalf("got %s", run.State)
	}
	if run.Attempts != 2 {
		t.Fatalf("attempts %d", run.Attempts)
	}
	if run.ExpiresAtField != nil {
		t.Fatal("expires_at must be cleared on resume")
	}
}

func TestSuspensionTimeoutIsTerminalFailure(t *testing.T) {
	t.Parallel()
	run := freshRun()
	_ = run.Apply(ClaimEvent())
	_ = run.Apply(SuspendEvent(nil))
	if err := run.Apply(SuspensionFailedEvent("wait deadline exceeded")); err != nil {
		t.Fatal(err)
	}
	if run.State != StateFailed {
		t.Fatalf("got %s", run.State)
	}
	if run.LastError == nil || *run.LastError != "wait deadline exceeded" {
		t.Fatalf("last_error %v", run.LastError)
	}
}

func TestCompensatingChainLandsInFailed(t *testing.T) {
	t.Parallel()
	run := freshRun()
	_ = run.Apply(ClaimEvent())
	if err := run.Apply(StartCompensatingEvent()); err != nil {
		t.Fatal(err)
	}
	if run.State != StateCompensating {
		t.Fatalf("got %s", run.State)
	}
	if err := run.Apply(CompensationCompletedEvent("rolled back")); err != nil {
		t.Fatal(err)
	}
	if run.State != StateFailed {
		t.Fatalf("got %s", run.State)
	}
	if run.LastError == nil || *run.LastError != "rolled back" {
		t.Fatalf("last_error %v", run.LastError)
	}
}

func TestInvalidEventInTerminalStateRejected(t *testing.T) {
	t.Parallel()
	run := freshRun()
	_ = run.Apply(ClaimEvent())
	_ = run.Apply(EffectCompletedEvent(json.RawMessage(`{}`)))
	if err := run.Apply(ClaimEvent()); err == nil {
		t.Fatal("re-claim of completed run must fail")
	}
}
