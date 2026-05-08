// Lineage-deletion request + audit trace types.
//
// Mirrors `services/audit-compliance-service/src/lineage_deletion/models/deletion.rs`
// 1:1.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// LineageDeletionRequestInput mirrors the Rust `LineageDeletionRequest`
// (the wire-level request payload, not the persisted row).
type LineageDeletionRequestInput struct {
	DatasetID  uuid.UUID `json:"dataset_id"`
	SubjectID  *string   `json:"subject_id,omitempty"`
	HardDelete bool      `json:"hard_delete,omitempty"`
	LegalHold  bool      `json:"legal_hold,omitempty"`
	Reason     *string   `json:"reason,omitempty"`
}

// LineageImpactSummary mirrors the Rust struct.
type LineageImpactSummary struct {
	DownstreamNodeCount    int          `json:"downstream_node_count"`
	DownstreamDatasetIDs   []uuid.UUID  `json:"downstream_dataset_ids"`
	BlockedByLegalHold     bool         `json:"blocked_by_legal_hold"`
	ImpactNotes            []string     `json:"impact_notes"`
}

// LineageDeletionResponse mirrors the Rust struct returned by
// `request_deletion`. The Go wire shape uses `request_id` to match
// the Rust impl.
type LineageDeletionResponse struct {
	RequestID    uuid.UUID            `json:"request_id"`
	DatasetID    uuid.UUID            `json:"dataset_id"`
	SubjectID    *string              `json:"subject_id"`
	Impact       LineageImpactSummary `json:"impact"`
	Status       string               `json:"status"`
	DeletedPaths []string             `json:"deleted_paths"`
	AuditTrace   json.RawMessage      `json:"audit_trace"`
	RequestedAt  time.Time            `json:"requested_at"`
	CompletedAt  *time.Time           `json:"completed_at"`
}

// DeletionAuditRecord mirrors the Rust struct.
type DeletionAuditRecord struct {
	Service   string          `json:"service"`
	Action    string          `json:"action"`
	SubjectID *string         `json:"subject_id"`
	Metadata  json.RawMessage `json:"metadata"`
}
