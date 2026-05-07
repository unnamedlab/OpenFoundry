package approvals

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	statemachine "github.com/openfoundry/openfoundry-go/libs/state-machine"
)

// ApprovalRequestState ports the Rust enum of the same name.
//
// Allowed transitions:
//
//	Pending    → Approved      (POST /approvals/{id}/decide → approve)
//	Pending    → Rejected      (POST /approvals/{id}/decide → reject)
//	Pending    → Expired       (timeout sweep CronJob)
//	Pending    → Escalated     (RESERVED — no caller in-tree today)
//	Escalated  → Approved      (RESERVED — escalation flow)
//	Escalated  → Rejected      (RESERVED — escalation flow)
//	Escalated  → Expired       (RESERVED — escalation timeout)
//	<terminal> → <self>        (idempotent re-application — safe)
type ApprovalRequestState string

const (
	StatePending   ApprovalRequestState = "pending"
	StateApproved  ApprovalRequestState = "approved"
	StateRejected  ApprovalRequestState = "rejected"
	StateExpired   ApprovalRequestState = "expired"
	StateEscalated ApprovalRequestState = "escalated"
)

// ParseState mirrors `ApprovalRequestState::parse`.
func ParseState(value string) (ApprovalRequestState, error) {
	switch value {
	case "pending":
		return StatePending, nil
	case "approved":
		return StateApproved, nil
	case "rejected":
		return StateRejected, nil
	case "expired":
		return StateExpired, nil
	case "escalated":
		return StateEscalated, nil
	default:
		return "", fmt.Errorf("unknown approval request state %q", value)
	}
}

// IsTerminal mirrors `ApprovalRequestState::is_terminal`.
func (s ApprovalRequestState) IsTerminal() bool {
	return s == StateApproved || s == StateRejected || s == StateExpired
}

// CanTransitionTo mirrors `ApprovalRequestState::can_transition_to`.
func (s ApprovalRequestState) CanTransitionTo(next ApprovalRequestState) bool {
	if s == next {
		return true
	}
	switch {
	case s == StatePending && next == StateApproved,
		s == StatePending && next == StateRejected,
		s == StatePending && next == StateExpired,
		s == StatePending && next == StateEscalated,
		s == StateEscalated && next == StateApproved,
		s == StateEscalated && next == StateRejected,
		s == StateEscalated && next == StateExpired:
		return true
	}
	return false
}

// EventKind tags ApprovalRequestEvent variants 1:1 with the Rust enum.
type EventKind string

const (
	EventApprove  EventKind = "approve"
	EventReject   EventKind = "reject"
	EventExpire   EventKind = "expire"
	EventEscalate EventKind = "escalate"
)

// Event mirrors the Rust `ApprovalRequestEvent`. The `Kind`
// discriminator selects which other field is meaningful.
type Event struct {
	Kind        EventKind
	DecidedBy   string     // Approve / Reject
	Comment     *string    // Approve / Reject
	ExpiredAt   time.Time  // Expire
	EscalatedAt time.Time  // Escalate
}

// ApprovalRequest mirrors the Rust aggregate. Persisted in the
// `state_data` JSON column; the SQL migration projects the operator-
// facing fields onto dedicated columns (tenant_id, subject, etc.).
type ApprovalRequest struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       string          `json:"tenant_id"`
	Subject        string          `json:"subject"`
	ApproverSet    []string        `json:"approver_set"`
	ActionPayload  json.RawMessage `json:"action_payload,omitempty"`
	CorrelationID  uuid.UUID       `json:"correlation_id"`
	StateField     ApprovalRequestState `json:"state"`
	ExpiresAtField *time.Time      `json:"expires_at,omitempty"`
	DecidedBy      *string         `json:"decided_by,omitempty"`
	DecidedAt      *time.Time      `json:"decided_at,omitempty"`
	Comment        *string         `json:"comment,omitempty"`
}

// New builds a fresh ApprovalRequest in the Pending state.
func New(id uuid.UUID, tenantID, subject string, approverSet []string, actionPayload json.RawMessage, correlationID uuid.UUID, expiresAt *time.Time) *ApprovalRequest {
	return &ApprovalRequest{
		ID:             id,
		TenantID:       tenantID,
		Subject:        subject,
		ApproverSet:    approverSet,
		ActionPayload:  actionPayload,
		CorrelationID:  correlationID,
		StateField:     StatePending,
		ExpiresAtField: expiresAt,
	}
}

// AggregateID satisfies the libs/state-machine Aggregate interface.
func (r *ApprovalRequest) AggregateID() uuid.UUID { return r.ID }

// CurrentState renders the discriminator into the queryable column.
func (r *ApprovalRequest) CurrentState() string { return string(r.StateField) }

// ExpiresAt is the optional timeout deadline.
func (r *ApprovalRequest) ExpiresAt() *time.Time { return r.ExpiresAtField }

// State returns the typed enum value.
func (r *ApprovalRequest) State() ApprovalRequestState { return r.StateField }

// Apply ports the Rust transition arms 1:1.
func (r *ApprovalRequest) Apply(event Event) error {
	now := time.Now().UTC()
	var next ApprovalRequestState
	switch {
	case (r.StateField == StatePending || r.StateField == StateEscalated) && event.Kind == EventApprove:
		decidedBy := event.DecidedBy
		r.DecidedBy = &decidedBy
		r.DecidedAt = &now
		r.Comment = cloneStr(event.Comment)
		r.ExpiresAtField = nil
		next = StateApproved
	case (r.StateField == StatePending || r.StateField == StateEscalated) && event.Kind == EventReject:
		decidedBy := event.DecidedBy
		r.DecidedBy = &decidedBy
		r.DecidedAt = &now
		r.Comment = cloneStr(event.Comment)
		r.ExpiresAtField = nil
		next = StateRejected
	case (r.StateField == StatePending || r.StateField == StateEscalated) && event.Kind == EventExpire:
		expiredAt := event.ExpiredAt
		if expiredAt.IsZero() {
			expiredAt = now
		}
		r.DecidedAt = &expiredAt
		r.ExpiresAtField = nil
		next = StateExpired
	case r.StateField == StatePending && event.Kind == EventEscalate:
		next = StateEscalated
	default:
		return statemachine.InvalidTransition(
			fmt.Sprintf("no ApprovalRequest transition from %s for event %s", r.StateField, event.Kind))
	}
	if !r.StateField.CanTransitionTo(next) {
		return statemachine.InvalidTransition(
			fmt.Sprintf("ApprovalRequest produced disallowed transition: %s → %s", r.StateField, next))
	}
	r.StateField = next
	return nil
}

func cloneStr(s *string) *string {
	if s == nil {
		return nil
	}
	c := *s
	return &c
}

// TableName is the fully-qualified Postgres table backing the
// state-machine PgStore for ApprovalRequest.
const TableName = "audit_compliance.approval_requests"
