// Package models holds wire types for authorization-policy-service.
//
// Foundation slice (slice 1): CedarPolicy CRUD wire format. Roles,
// groups, permissions, restricted views, and ABAC evaluator land in
// follow-up slices — see INVENTORY-authorization-policy-service.md.
package models

import (
	"time"

	"github.com/google/uuid"
)

// CedarPolicy mirrors `cedar_policies` rows. The `Source` field is the
// raw Cedar policy text — every write must round-trip through
// libs/authz-cedar-go (parse + strict schema validation) before being
// persisted, so corrupt rows can't leak into the active PolicySet.
type CedarPolicy struct {
	ID          string    `json:"id"`
	Version     int32     `json:"version"`
	Source      string    `json:"source"`
	Description *string   `json:"description"`
	Active      bool      `json:"active"`
	CreatedBy   uuid.UUID `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateCedarPolicyRequest is the body of POST /api/v1/cedar-policies.
//
// Wire-format invariants:
//   - `id` must be non-empty (it's the Cedar PolicyID; carried byte-for-byte
//     into the in-memory PolicySet).
//   - `source` must parse + validate against the bundled Cedar schema in
//     strict mode. The handler delegates this check to libs/authz-cedar-go.
//   - `version` defaults to 1 when omitted; subsequent writes via the
//     PATCH endpoint bump it.
type CreateCedarPolicyRequest struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	Description *string `json:"description,omitempty"`
	Active      *bool   `json:"active,omitempty"`
	Version     *int32  `json:"version,omitempty"`
}

// UpdateCedarPolicyRequest mirrors the PATCH semantics — every field
// optional; nil preserves the current value. Bumping `source` always
// re-validates against the schema before swap.
type UpdateCedarPolicyRequest struct {
	Source      *string `json:"source,omitempty"`
	Description *string `json:"description,omitempty"`
	Active      *bool   `json:"active,omitempty"`
}

// ListResponse is the canonical envelope for list endpoints. Matches
// the foundation-slice convention (organizations + enrollments).
type ListResponse[T any] struct {
	Items []T `json:"items"`
}
