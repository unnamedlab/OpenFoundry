// Package scan holds the pure decoders for the request payload +
// the ontology.reindex.v1 record body. The live Cassandra scanner
// lands in a follow-up slice.
//
// Wire-compat with the legacy Go worker:
//   - Same JSON record shape published to ontology.reindex.v1, so
//     services/ontology-indexer keeps decoding both
//     object.changed.v1 and reindex.v1 with the same code path.
//   - Same partition key composition (tenant/id) so re-indexed
//     records hash to the same partition as live object.changed.v1
//     records — required for the indexer's per-object version check.
package scan

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/openfoundry/openfoundry-go/services/reindex-coordinator-service/internal/event"
)

// Page-size bounds. Lower bound matches the minimum useful page;
// upper bound matches the SQL CHECK on reindex_jobs.
const (
	MinPageSize     int32 = 1
	MaxPageSize     int32 = 10_000
	DefaultPageSize int32 = 1000
)

// DecodedRequest is the parsed + validated requested.v1 payload.
// page_size is clamped into [MinPageSize, MaxPageSize] and defaults
// to DefaultPageSize.
type DecodedRequest struct {
	TenantID  string
	TypeID    *string
	PageSize  int32
	RequestID *string
}

// DecodeError mirrors the Rust enum variants.
type DecodeError struct {
	Kind   string // "json" | "missing" | "invalid"
	Field  string
	Reason string
	Inner  error
}

func (e *DecodeError) Error() string {
	switch e.Kind {
	case "json":
		return fmt.Sprintf("invalid JSON payload: %s", e.Inner)
	case "missing":
		return fmt.Sprintf("missing required field: %s", e.Field)
	case "invalid":
		return fmt.Sprintf("invalid field %s: %s", e.Field, e.Reason)
	default:
		return "decode error"
	}
}

// DecodeRequest parses + validates a requested.v1 payload from raw bytes.
func DecodeRequest(b []byte) (DecodedRequest, error) {
	var raw event.ReindexRequestedV1
	if err := json.Unmarshal(b, &raw); err != nil {
		return DecodedRequest{}, &DecodeError{Kind: "json", Inner: err}
	}
	if trimmed := trimSpace(raw.TenantID); trimmed == "" {
		return DecodedRequest{}, &DecodeError{Kind: "missing", Field: "tenant_id"}
	}

	var typeID *string
	if raw.TypeID != nil {
		t := trimSpace(*raw.TypeID)
		if t != "" {
			typeID = &t
		}
	}

	pageSize := DefaultPageSize
	if raw.PageSize != nil {
		switch {
		case *raw.PageSize == 0:
			// keep default
		case *raw.PageSize < 0:
			return DecodedRequest{}, &DecodeError{Kind: "invalid", Field: "page_size", Reason: "must be non-negative"}
		default:
			n := *raw.PageSize
			if n < MinPageSize {
				n = MinPageSize
			}
			if n > MaxPageSize {
				n = MaxPageSize
			}
			pageSize = n
		}
	}

	return DecodedRequest{
		TenantID:  raw.TenantID,
		TypeID:    typeID,
		PageSize:  pageSize,
		RequestID: raw.RequestID,
	}, nil
}

// ReindexRecord is one JSON record published to ontology.reindex.v1.
// Matches the shape produced by the legacy Go worker and consumed
// by services/ontology-indexer::ObjectChangedV1.
type ReindexRecord struct {
	Tenant  string          `json:"tenant"`
	ID      string          `json:"id"`
	TypeID  string          `json:"type_id"`
	Version int64           `json:"version"`
	Payload json.RawMessage `json:"payload"`
	// Optional dense vector. Pass-through only — the coordinator
	// does not compute it; whatever was in objects_by_id.properties
	// is forwarded verbatim.
	Embedding []float64 `json:"embedding,omitempty"`
	// Always false: deleted rows are filtered out before publish.
	Deleted bool `json:"deleted,omitempty"`
}

// PartitionKey is the Kafka partition key for this record. Same
// "tenant/id" composition as the legacy Go worker so re-indexed
// records hash to the same partition as live object.changed.v1
// records for the same object.
func (r *ReindexRecord) PartitionKey() string {
	return r.Tenant + "/" + r.ID
}

// EncodeBatchRecord builds a ReindexRecord from the raw fields
// fetched from Cassandra. `properties` is the JSON column from
// objects_by_id; we attempt to extract `embedding` from it (same
// heuristic as the Go worker).
func EncodeBatchRecord(tenant, id, typeID string, version int64, properties json.RawMessage) ReindexRecord {
	emb := extractEmbedding(properties)
	return ReindexRecord{
		Tenant:    tenant,
		ID:        id,
		TypeID:    typeID,
		Version:   version,
		Payload:   properties,
		Embedding: emb,
		Deleted:   false,
	}
}

func extractEmbedding(properties json.RawMessage) []float64 {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(properties, &m); err != nil {
		return nil
	}
	raw, ok := m["embedding"]
	if !ok {
		return nil
	}
	var arr []float64
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		return nil
	}
	return arr
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// errors.As helper — keep tests using errors.As-friendly type checks.
var _ error = (*DecodeError)(nil)

// IsDecodeError tests for a DecodeError of any kind.
func IsDecodeError(err error) bool {
	var de *DecodeError
	return errors.As(err, &de)
}
