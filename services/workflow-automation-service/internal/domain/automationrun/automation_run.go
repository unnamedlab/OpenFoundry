// Package automationrun ports the AutomationRun state machine from
// `services/workflow-automation-service/src/domain/automation_run.rs`
// 1:1.
//
// Pure no-IO state machine; the Postgres-backed persistence layer
// uses libs/state-machine.PgStore[*AutomationRun, AutomationRunEvent].
//
// Allowed transitions:
//
//	Queued       → Running       (consumer claims)
//	Queued       → Failed        (definition broken / pre-flight reject)
//	Running      → Completed     (effect succeeded)
//	Running      → Failed        (effect failed terminally)
//	Running      → Suspended     (multi-step waits on external signal)
//	Running      → Compensating  (saga rollback triggered)
//	Suspended    → Running       (resumed by signal)
//	Suspended    → Failed        (timeout sweep / external cancel)
//	Compensating → Failed        (compensation finished — terminal)
//	<terminal>   → <self>        (idempotent re-application — safe)
package automationrun

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	statemachine "github.com/openfoundry/openfoundry-go/libs/state-machine"
)

// State enumerates the lifecycle of a row in
// `workflow_automation.automation_runs`. Wire format (lowercase)
// matches the SQL CHECK constraint.
type State string

const (
	StateQueued       State = "queued"
	StateRunning      State = "running"
	StateSuspended    State = "suspended"
	StateCompensating State = "compensating"
	StateCompleted    State = "completed"
	StateFailed       State = "failed"
)

// ParseState mirrors AutomationRunState::parse.
func ParseState(value string) (State, error) {
	switch value {
	case "queued":
		return StateQueued, nil
	case "running":
		return StateRunning, nil
	case "suspended":
		return StateSuspended, nil
	case "compensating":
		return StateCompensating, nil
	case "completed":
		return StateCompleted, nil
	case "failed":
		return StateFailed, nil
	default:
		return "", fmt.Errorf("unknown automation run state %q", value)
	}
}

// IsTerminal mirrors AutomationRunState::is_terminal.
func (s State) IsTerminal() bool { return s == StateCompleted || s == StateFailed }

// CanTransitionTo mirrors AutomationRunState::can_transition_to.
//
// Self-transitions on every state are allowed so an INSERT … ON
// CONFLICT DO UPDATE writer that re-applies the same outcome event
// does not raise.
func (s State) CanTransitionTo(next State) bool {
	if s == next {
		return true
	}
	switch {
	case s == StateQueued && next == StateRunning,
		s == StateQueued && next == StateFailed,
		s == StateRunning && next == StateCompleted,
		s == StateRunning && next == StateFailed,
		s == StateRunning && next == StateSuspended,
		s == StateRunning && next == StateCompensating,
		s == StateSuspended && next == StateRunning,
		s == StateSuspended && next == StateFailed,
		s == StateCompensating && next == StateFailed:
		return true
	}
	return false
}

// EventKind is the tag of a domain event the AutomationRun aggregate
// accepts.
type EventKind string

const (
	EventClaim                  EventKind = "claim"
	EventEffectCompleted        EventKind = "effect_completed"
	EventEffectFailed           EventKind = "effect_failed"
	EventPreFlightFailed        EventKind = "pre_flight_failed"
	EventSuspend                EventKind = "suspend"
	EventResume                 EventKind = "resume"
	EventStartCompensating      EventKind = "start_compensating"
	EventCompensationCompleted  EventKind = "compensation_completed"
	EventSuspensionFailed       EventKind = "suspension_failed"
)

// Event mirrors AutomationRunEvent (Rust enum). The `Kind` discriminator
// selects which other field is meaningful — extra fields are zero-valued
// for variants that don't carry them.
type Event struct {
	Kind      EventKind
	Response  json.RawMessage // EventEffectCompleted
	Error     string          // EventEffectFailed / PreFlightFailed / SuspensionFailed / CompensationCompleted
	WaitUntil *time.Time      // EventSuspend
}

// Constructors — match the Rust enum variants 1:1 for callers.
func ClaimEvent() Event { return Event{Kind: EventClaim} }
func EffectCompletedEvent(response json.RawMessage) Event {
	return Event{Kind: EventEffectCompleted, Response: response}
}
func EffectFailedEvent(err string) Event   { return Event{Kind: EventEffectFailed, Error: err} }
func PreFlightFailedEvent(err string) Event { return Event{Kind: EventPreFlightFailed, Error: err} }
func SuspendEvent(waitUntil *time.Time) Event {
	return Event{Kind: EventSuspend, WaitUntil: waitUntil}
}
func ResumeEvent() Event             { return Event{Kind: EventResume} }
func StartCompensatingEvent() Event  { return Event{Kind: EventStartCompensating} }
func CompensationCompletedEvent(err string) Event {
	return Event{Kind: EventCompensationCompleted, Error: err}
}
func SuspensionFailedEvent(err string) Event {
	return Event{Kind: EventSuspensionFailed, Error: err}
}

// AutomationRun mirrors the Rust struct of the same name.
type AutomationRun struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       uuid.UUID       `json:"tenant_id"`
	DefinitionID   uuid.UUID       `json:"definition_id"`
	CorrelationID  uuid.UUID       `json:"correlation_id"`
	State          State           `json:"state"`
	ExpiresAtField *time.Time      `json:"expires_at,omitempty"`
	Attempts       uint32          `json:"attempts,omitempty"`
	LastError      *string         `json:"last_error,omitempty"`
	EffectResponse json.RawMessage `json:"effect_response,omitempty"`
	Progress       json.RawMessage `json:"progress,omitempty"`
}

// New builds a fresh AutomationRun in the Queued state.
func New(id, tenantID, definitionID, correlationID uuid.UUID, expiresAt *time.Time) *AutomationRun {
	return &AutomationRun{
		ID:             id,
		TenantID:       tenantID,
		DefinitionID:   definitionID,
		CorrelationID:  correlationID,
		State:          StateQueued,
		ExpiresAtField: expiresAt,
		Progress:       json.RawMessage(`{}`),
	}
}

// AggregateID satisfies the libs/state-machine Aggregate interface.
func (r *AutomationRun) AggregateID() uuid.UUID { return r.ID }

// CurrentState renders the discriminator into the queryable column.
func (r *AutomationRun) CurrentState() string { return string(r.State) }

// ExpiresAt is the optional timeout deadline.
func (r *AutomationRun) ExpiresAt() *time.Time { return r.ExpiresAtField }

// Apply ports the Rust `transition` arm-table 1:1.
func (r *AutomationRun) Apply(event Event) error {
	var next State
	switch {
	case r.State == StateQueued && event.Kind == EventClaim:
		r.Attempts = saturatingAdd(r.Attempts, 1)
		next = StateRunning
	case r.State == StateQueued && event.Kind == EventPreFlightFailed:
		r.LastError = ptrStr(event.Error)
		next = StateFailed
	case r.State == StateRunning && event.Kind == EventEffectCompleted:
		r.EffectResponse = append(json.RawMessage(nil), event.Response...)
		r.ExpiresAtField = nil
		next = StateCompleted
	case r.State == StateRunning && event.Kind == EventEffectFailed:
		r.LastError = ptrStr(event.Error)
		r.ExpiresAtField = nil
		next = StateFailed
	case r.State == StateRunning && event.Kind == EventSuspend:
		if event.WaitUntil != nil {
			t := *event.WaitUntil
			r.ExpiresAtField = &t
		} else {
			r.ExpiresAtField = nil
		}
		next = StateSuspended
	case r.State == StateRunning && event.Kind == EventStartCompensating:
		next = StateCompensating
	case r.State == StateSuspended && event.Kind == EventResume:
		r.Attempts = saturatingAdd(r.Attempts, 1)
		r.ExpiresAtField = nil
		next = StateRunning
	case r.State == StateSuspended && event.Kind == EventSuspensionFailed:
		r.LastError = ptrStr(event.Error)
		r.ExpiresAtField = nil
		next = StateFailed
	case r.State == StateCompensating && event.Kind == EventCompensationCompleted:
		r.LastError = ptrStr(event.Error)
		next = StateFailed
	default:
		return statemachine.InvalidTransition(
			fmt.Sprintf("no AutomationRun transition from %s for event %s", r.State, event.Kind))
	}

	if !r.State.CanTransitionTo(next) {
		return statemachine.InvalidTransition(
			fmt.Sprintf("AutomationRun produced disallowed transition: %s → %s", r.State, next))
	}
	r.State = next
	return nil
}

func saturatingAdd(a, b uint32) uint32 {
	if a > ^uint32(0)-b {
		return ^uint32(0)
	}
	return a + b
}

func ptrStr(s string) *string { return &s }

// TableName is the fully-qualified Postgres table backing the
// state-machine PgStore for AutomationRun.
const TableName = "workflow_automation.automation_runs"

// ProcessedEventsTable is the fully-qualified Postgres table backing
// the per-service idempotency store consumed by the condition
// consumer.
const ProcessedEventsTable = "workflow_automation.processed_events"
