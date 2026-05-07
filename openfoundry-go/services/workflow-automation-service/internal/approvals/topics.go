// Package approvals holds the FASE 7 / Tarea 7.3 state-machine
// constants for the human-in-the-loop approvals plane (legacy
// approvals-service, S8 merge per ADR-0030).
package approvals

// ApprovalRequestedV1 is outbound — emitted by POST /api/v1/approvals.
// Replaces the implicit "Temporal workflow started" signal.
const ApprovalRequestedV1 = "approval.requested.v1"

// ApprovalDecidedV1 is inbound — reserved for the future "manager
// decided externally" path. No in-tree producer today; the dedup
// table (audit_compliance.processed_events) is provisioned so the
// consumer wires without a schema migration when a producer exists.
const ApprovalDecidedV1 = "approval.decided.v1"

// ApprovalCompletedV1 is outbound — emitted on every terminal
// pending → approved/rejected transition driven by
// POST /api/v1/approvals/{id}/decide.
const ApprovalCompletedV1 = "approval.completed.v1"

// ApprovalExpiredV1 is outbound — subset of approval.completed.v1
// reserved for pending → expired transitions driven by the timeout
// sweep CronJob (FASE 7 / Tarea 7.4). Kept on a separate topic so
// SLO alerts can fire on the expired feed without filtering the
// completed feed.
const ApprovalExpiredV1 = "approval.expired.v1"
