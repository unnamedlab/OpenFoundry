// Package models holds wire types for ontology-query-service.
//
// The service intentionally reuses storage-abstraction repository models for
// object payloads so the Go JSON shape stays compatible with the Rust
// ontology-query-service (`tenant`, `id`, `type_id`, `version`, `payload`,
// timestamps, owner, and markings).
package models

import (
	"github.com/google/uuid"

	repos "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

type ListResponse[T any] struct {
	Items     []T     `json:"items"`
	NextToken *string `json:"next_token"`
}

type Object = repos.Object

// EnsureValidUUIDOrEmpty is a defensive check used by handlers when path
// parameters are expected to be UUIDs. It returns ok=false on parse error.
func EnsureValidUUIDOrEmpty(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}
