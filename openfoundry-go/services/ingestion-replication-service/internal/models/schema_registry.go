package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SchemaSubject is a Confluent-compatible schema-registry subject row.
type SchemaSubject struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	CompatibilityMode string    `json:"compatibility_mode"`
	CreatedAt         time.Time `json:"created_at"`
}

// SchemaVersion stores one registered schema for a subject.
type SchemaVersion struct {
	ID           uuid.UUID  `json:"id"`
	SubjectID    uuid.UUID  `json:"subject_id"`
	Version      int32      `json:"version"`
	SchemaType   string     `json:"schema_type"`
	SchemaText   string     `json:"schema_text"`
	Fingerprint  string     `json:"fingerprint"`
	CreatedAt    time.Time  `json:"created_at"`
	DeprecatedAt *time.Time `json:"deprecated_at,omitempty"`
}

// SchemaReference mirrors the Confluent reference item accepted on register.
type SchemaReference struct {
	Name    string `json:"name"`
	Subject string `json:"subject"`
	Version int32  `json:"version"`
}

// RegisterSchemaVersionRequest is POST /subjects/{name}/versions.
type RegisterSchemaVersionRequest struct {
	Schema          string            `json:"schema"`
	SchemaType      string            `json:"schemaType,omitempty"`
	SchemaTypeAlias string            `json:"schema_type,omitempty"`
	References      []SchemaReference `json:"references,omitempty"`
}

func (r RegisterSchemaVersionRequest) EffectiveSchemaType() string {
	if r.SchemaType != "" {
		return r.SchemaType
	}
	if r.SchemaTypeAlias != "" {
		return r.SchemaTypeAlias
	}
	return "AVRO"
}

// RegisterSchemaVersionResponse matches Confluent's `{ "id": <schema id> }`.
type RegisterSchemaVersionResponse struct {
	ID int32 `json:"id"`
}

// SchemaVersionResponse is GET /subjects/{name}/versions/{version}.
type SchemaVersionResponse struct {
	Subject    string `json:"subject"`
	ID         int32  `json:"id"`
	Version    int32  `json:"version"`
	Schema     string `json:"schema"`
	SchemaType string `json:"schemaType"`
}

// CompatibilityCheckRequest is POST /compatibility/subjects/{name}/versions/{version}.
type CompatibilityCheckRequest struct {
	Schema          string `json:"schema"`
	SchemaType      string `json:"schemaType,omitempty"`
	SchemaTypeAlias string `json:"schema_type,omitempty"`
}

func (r CompatibilityCheckRequest) EffectiveSchemaType() string {
	if r.SchemaType != "" {
		return r.SchemaType
	}
	if r.SchemaTypeAlias != "" {
		return r.SchemaTypeAlias
	}
	return "AVRO"
}

// CompatibilityCheckResponse mirrors Confluent compatibility responses.
type CompatibilityCheckResponse struct {
	IsCompatible bool     `json:"is_compatible"`
	Messages     []string `json:"messages,omitempty"`
}

// CanonicalSchemaJSON returns a stable JSON rendering for fingerprinting.
func CanonicalSchemaJSON(schema string) (string, error) {
	var v any
	if err := json.Unmarshal([]byte(schema), &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
