// Package markings holds the wire types projected by the iceberg
// markings endpoints. Pure DTOs — SQL projections live in `internal/repo`.
package markings

import "github.com/google/uuid"

// MarkingProjection is one row in a markings response: the canonical id,
// its display name and the human description seeded from
// `iceberg_marking_names`.
type MarkingProjection struct {
	MarkingID   uuid.UUID `json:"marking_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
}

// NamespaceMarkings is the body of GET .../namespaces/{ns}/markings.
//
// Namespaces are flat: there is no inheritance source above them today,
// so `effective` and `explicit` always carry the same set.
type NamespaceMarkings struct {
	Effective []MarkingProjection `json:"effective"`
	Explicit  []MarkingProjection `json:"explicit"`
}

// TableMarkings is the body of GET .../tables/{tbl}/markings.
//
// Effective is the union of explicit + inherited; inherited is
// snapshotted from the namespace at table-creation time and survives
// later namespace mutations per Foundry doc semantics.
type TableMarkings struct {
	Effective              []MarkingProjection `json:"effective"`
	Explicit               []MarkingProjection `json:"explicit"`
	InheritedFromNamespace []MarkingProjection `json:"inherited_from_namespace"`
}

// Names projects a marking list down to its display names, preserving
// the iteration order. Used when we need to enforce clearance against
// the bearer principal's `iceberg-clearance:<name>` scopes.
func Names(items []MarkingProjection) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Name)
	}
	return out
}
