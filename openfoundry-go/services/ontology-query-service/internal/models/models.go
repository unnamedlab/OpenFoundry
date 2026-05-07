// Package models holds wire types for ontology-query-service.
//
// Foundation slice scope: skeleton handlers that 501 until
// libs/cassandra-kernel + libs/storage-abstraction-go ports land.
// The hot-path read of object instances + link instances stays in
// Cassandra per ADR S1.5.
package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// ObjectInstance mirrors the Cassandra row read by the get_object
// handler. Fields are tentative: full schema lands when
// libs/cassandra-kernel-go ports the ObjectStore trait.
type ObjectInstance struct {
	Tenant     string          `json:"tenant"`
	TypeID     string          `json:"type_id"`
	ObjectID   string          `json:"object_id"`
	Properties json.RawMessage `json:"properties"`
	Markings   []string        `json:"markings"`
	Version    int64           `json:"version"`
}

// EnsureValidUUIDOrEmpty is a defensive check used by handlers when
// path parameters are expected to be UUIDs. Returns the parsed value
// and ok=false on parse error. Today most callers use string ids so
// this is reserved for the canonical type-system migration.
func EnsureValidUUIDOrEmpty(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
