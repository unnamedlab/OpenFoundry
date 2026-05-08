package models

// schema_history.go ports
// services/ingestion-replication-service/src/event_streaming/models/schema_history.rs.
//
// Wire types for the schema-validation + history endpoints (IRF-9):
//   POST /api/v1/streaming/streams/{id}/schema:validate
//   GET  /api/v1/streaming/streams/{id}/schema/history

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// StreamSchemaVersion is one accepted schema for a stream, surfaced
// by the history endpoint. Mirrors the Rust struct of the same name.
type StreamSchemaVersion struct {
	ID            uuid.UUID       `json:"id"`
	StreamID      uuid.UUID       `json:"stream_id"`
	Version       int32           `json:"version"`
	SchemaAvro    json.RawMessage `json:"schema_avro"`
	Fingerprint   string          `json:"fingerprint"`
	Compatibility string          `json:"compatibility"`
	CreatedBy     string          `json:"created_by"`
	CreatedAt     time.Time       `json:"created_at"`
}

// ValidateSchemaRequest is the body of POST /streams/{id}/schema:validate.
//
// Sample is optional — when present it is validated against schema_avro
// and any compatibility issues against the current stream schema are
// also returned. Compatibility, when omitted, defaults to the stream's
// persisted compatibility mode.
type ValidateSchemaRequest struct {
	SchemaAvro    json.RawMessage `json:"schema_avro"`
	Sample        json.RawMessage `json:"sample,omitempty"`
	Compatibility *string         `json:"compatibility,omitempty"`
}

// ValidateSchemaResponse is the response body. Errors and Warnings are
// always non-nil JSON arrays so the wire shape matches the Rust impl
// (which serialises empty Vecs as `[]`, not `null`).
type ValidateSchemaResponse struct {
	Valid         bool                  `json:"valid"`
	Fingerprint   *string               `json:"fingerprint"`
	Errors        []string              `json:"errors"`
	Warnings      []string              `json:"warnings"`
	Compatibility *CompatibilityOutcome `json:"compatibility"`
}

// CompatibilityOutcome reports the result of the compatibility check
// against the current persisted schema.
type CompatibilityOutcome struct {
	Mode       string  `json:"mode"`
	Compatible bool    `json:"compatible"`
	Reason     *string `json:"reason"`
}
