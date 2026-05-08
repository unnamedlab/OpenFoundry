// Package event holds the wire format of the Kafka events the
// reindex coordinator consumes and produces, plus the deterministic
// event_id derivation used for per-job and per-batch idempotency.
//
// The derivation uses UUID v5 (SHA-1 namespaced UUIDs, RFC 4122)
// so the resulting id depends only on the inputs — replays of the
// same (tenant_id, type_id, page_token) always produce the same id.
package event

import (
	"github.com/google/uuid"
)

// ReindexNamespace is the UUID-v5 namespace for everything emitted
// by this service. Hard-coded constant rather than an OID namespace
// so a future namespace migration is a single-line change. Pinned
// verbatim from the Rust source — DO NOT change without a fleet-wide
// schema dance.
var ReindexNamespace = uuid.UUID{
	0x6f, 0x82, 0x4d, 0x6e, 0x71, 0xa1, 0x4b, 0x9b,
	0x9c, 0xfe, 0x9f, 0x4f, 0x07, 0x2c, 0x88, 0x10,
}

// ReindexRequestedV1 is the payload of ontology.reindex.requested.v1.
//
// The producer does NOT supply a resume token — the coordinator owns
// the cursor in pg-runtime-config.reindex_jobs. A second `requested`
// event for the same (tenant, type) is either a no-op (job still
// running) or a re-start of a finished job (transition validated by
// the state machine).
type ReindexRequestedV1 struct {
	TenantID string `json:"tenant_id"`
	// Optional. Empty / absent ⇒ scan all types via ALLOW FILTERING.
	TypeID *string `json:"type_id,omitempty"`
	// Optional override. Coordinator clamps to [1, 10000] and
	// defaults to 1000 to match the Go worker default.
	PageSize *int32 `json:"page_size,omitempty"`
	// Optional opaque correlation id surfaced by the producer.
	// Carried through to the completed.v1 event so the caller can
	// stitch request → completion together without leaning on Kafka
	// offsets.
	RequestID *string `json:"request_id,omitempty"`
}

// ReindexCompletedV1 is the payload of ontology.reindex.completed.v1.
// Mirrors the OntologyReindexResult struct of the legacy Go workflow.
type ReindexCompletedV1 struct {
	JobID     uuid.UUID `json:"job_id"`
	TenantID  string    `json:"tenant_id"`
	TypeID    *string   `json:"type_id,omitempty"`
	Scanned   int64     `json:"scanned"`
	Published int64     `json:"published"`
	// One of completed / failed / cancelled. Validated by
	// state.JobStatus.IsTerminal before being written here.
	Status    string  `json:"status"`
	Error     *string `json:"error,omitempty"`
	RequestID *string `json:"request_id,omitempty"`
}

// DeriveJobID derives the stable job UUID from (tenantID, typeID).
// (tenant, "") and (tenant, "users") are DISTINCT jobs by design.
// nil and Some("") collapse to the same key (matches Rust).
func DeriveJobID(tenantID string, typeID *string) uuid.UUID {
	t := ""
	if typeID != nil {
		t = *typeID
	}
	buf := make([]byte, 0, len(tenantID)+1+len(t))
	buf = append(buf, tenantID...)
	buf = append(buf, '|')
	buf = append(buf, t...)
	return uuid.NewSHA1(ReindexNamespace, buf)
}

// DeriveBatchEventID derives the per-batch idempotency event_id
// from (tenantID, typeID, pageToken). pageToken is the opaque
// base64 of the gocql PageState; the empty string is used for
// the first page so "page 0" of a job has its own stable id
// distinct from "page 1".
func DeriveBatchEventID(tenantID string, typeID *string, pageToken string) uuid.UUID {
	t := ""
	if typeID != nil {
		t = *typeID
	}
	buf := make([]byte, 0, len(tenantID)+2+len(t)+len(pageToken))
	buf = append(buf, tenantID...)
	buf = append(buf, '|')
	buf = append(buf, t...)
	buf = append(buf, '|')
	buf = append(buf, pageToken...)
	return uuid.NewSHA1(ReindexNamespace, buf)
}
