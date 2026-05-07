package approvals

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ApprovalsNamespace is the UUIDv5 namespace for everything emitted
// by the approvals subsystem. Pinned forever — generated once with
// uuidgen and never rotated.
var ApprovalsNamespace = uuid.UUID{
	0xc4, 0x18, 0x73, 0x59, 0x96, 0xc7, 0x4c, 0x84,
	0xa6, 0x3d, 0x42, 0xab, 0x91, 0x6e, 0x36, 0xa3,
}

// ApprovalRequestedV1Payload mirrors the Rust struct of the same name.
// Emitted once when the HTTP handler accepts a new approval.
type ApprovalRequestedV1Payload struct {
	ApprovalID    uuid.UUID       `json:"approval_id"`
	TenantID      string          `json:"tenant_id"`
	Subject       string          `json:"subject"`
	ApproverSet   []string        `json:"approver_set"`
	ActionPayload json.RawMessage `json:"action_payload,omitempty"`
	CorrelationID uuid.UUID       `json:"correlation_id"`
	TriggeredBy   string          `json:"triggered_by"`
	ExpiresAt     *time.Time      `json:"expires_at,omitempty"`
}

// ApprovalCompletedV1Payload mirrors the Rust struct of the same name.
// Emitted on every terminal pending → approved/rejected transition.
type ApprovalCompletedV1Payload struct {
	ApprovalID    uuid.UUID `json:"approval_id"`
	TenantID      string    `json:"tenant_id"`
	CorrelationID uuid.UUID `json:"correlation_id"`
	Decision      string    `json:"decision"`
	DecidedBy     string    `json:"decided_by"`
	Comment       *string   `json:"comment,omitempty"`
	DecidedAt     time.Time `json:"decided_at"`
}

// ApprovalExpiredV1Payload mirrors the Rust struct of the same name.
// Emitted by the timeout sweep on every pending → expired transition.
type ApprovalExpiredV1Payload struct {
	ApprovalID    uuid.UUID `json:"approval_id"`
	TenantID      string    `json:"tenant_id"`
	CorrelationID uuid.UUID `json:"correlation_id"`
	ExpiredAt     time.Time `json:"expired_at"`
	Deadline      time.Time `json:"deadline"`
}

// ApprovalDecidedV1Payload mirrors the Rust struct of the same name.
// Future inbound topic (no in-tree producer today).
type ApprovalDecidedV1Payload struct {
	ApprovalID    uuid.UUID `json:"approval_id"`
	TenantID      string    `json:"tenant_id"`
	CorrelationID uuid.UUID `json:"correlation_id"`
	Decision      string    `json:"decision"`
	DecidedBy     string    `json:"decided_by"`
	Comment       *string   `json:"comment,omitempty"`
}

// DeriveOutboxEventID mirrors `event::derive_outbox_event_id`.
// Re-publishing the same id collapses via outbox ON CONFLICT DO NOTHING.
func DeriveOutboxEventID(approvalID uuid.UUID, kind string) uuid.UUID {
	buf := make([]byte, 0, 17+len(kind))
	buf = append(buf, approvalID[:]...)
	buf = append(buf, '|')
	buf = append(buf, []byte(kind)...)
	return uuid.NewSHA1(ApprovalsNamespace, buf)
}

// DeriveDecidedEventID mirrors `event::derive_decided_event_id`.
// Reserved for the future inbound approval.decided.v1 consumer.
func DeriveDecidedEventID(approvalID uuid.UUID, decidedBy, decision string) uuid.UUID {
	buf := make([]byte, 0, 17+len(decidedBy)+1+len(decision))
	buf = append(buf, approvalID[:]...)
	buf = append(buf, '|')
	buf = append(buf, []byte(decidedBy)...)
	buf = append(buf, '|')
	buf = append(buf, []byte(decision)...)
	return uuid.NewSHA1(ApprovalsNamespace, buf)
}
