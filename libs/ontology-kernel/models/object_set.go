package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ObjectSetPolicy mirrors `libs/ontology-kernel/src/models/object_set.rs`
// `struct ObjectSetPolicy`. `allowed_markings` and `deny_guest_sessions`
// are `#[serde(default)]`.
type ObjectSetPolicy struct {
	AllowedMarkings           []string   `json:"allowed_markings"`
	MinimumClearance          *string    `json:"minimum_clearance"`
	DenyGuestSessions         bool       `json:"deny_guest_sessions"`
	RequiredRestrictedViewID  *uuid.UUID `json:"required_restricted_view_id"`
}

// UnmarshalJSON applies the Rust `#[serde(default)]` defaults — empty
// slice for `allowed_markings`, `false` for `deny_guest_sessions`.
func (p *ObjectSetPolicy) UnmarshalJSON(b []byte) error {
	type alias ObjectSetPolicy
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*p = ObjectSetPolicy(raw)
	if p.AllowedMarkings == nil {
		p.AllowedMarkings = []string{}
	}
	return nil
}

// MarshalJSON ensures `allowed_markings` always serialises as `[]`
// rather than `null`, matching serde for `Vec<String>`.
func (p ObjectSetPolicy) MarshalJSON() ([]byte, error) {
	if p.AllowedMarkings == nil {
		p.AllowedMarkings = []string{}
	}
	type alias ObjectSetPolicy
	return json.Marshal(alias(p))
}

// ObjectSetFilter mirrors `struct ObjectSetFilter`. `value` is
// `#[serde(default)]` so an absent key yields a JSON null.
type ObjectSetFilter struct {
	Field    string          `json:"field"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value"`
}

// ObjectSetTraversal mirrors `struct ObjectSetTraversal`.
type ObjectSetTraversal struct {
	Direction           string     `json:"direction"`
	LinkTypeID          *uuid.UUID `json:"link_type_id"`
	TargetObjectTypeID  *uuid.UUID `json:"target_object_type_id"`
	MaxHops             int32      `json:"max_hops"`
}

// ObjectSetJoin mirrors `struct ObjectSetJoin`.
type ObjectSetJoin struct {
	SecondaryObjectTypeID uuid.UUID `json:"secondary_object_type_id"`
	LeftField             string    `json:"left_field"`
	RightField            string    `json:"right_field"`
	JoinKind              string    `json:"join_kind"`
}

// ObjectSetDefinition mirrors `struct ObjectSetDefinition`.
type ObjectSetDefinition struct {
	ID                   uuid.UUID            `json:"id"`
	Name                 string               `json:"name"`
	Description          string               `json:"description"`
	BaseObjectTypeID     uuid.UUID            `json:"base_object_type_id"`
	Filters              []ObjectSetFilter    `json:"filters"`
	Traversals           []ObjectSetTraversal `json:"traversals"`
	Join                 *ObjectSetJoin       `json:"join"`
	Projections          []string             `json:"projections"`
	WhatIfLabel          *string              `json:"what_if_label"`
	Policy               ObjectSetPolicy      `json:"policy"`
	MaterializedSnapshot json.RawMessage      `json:"materialized_snapshot"`
	MaterializedAt       *time.Time           `json:"materialized_at"`
	MaterializedRowCount int32                `json:"materialized_row_count"`
	OwnerID              uuid.UUID            `json:"owner_id"`
	CreatedAt            time.Time            `json:"created_at"`
	UpdatedAt            time.Time            `json:"updated_at"`
}

// CreateObjectSetRequest mirrors `struct CreateObjectSetRequest`. The
// fields `description`, `filters`, `traversals`, `projections`, `policy`
// are `#[serde(default)]` in Rust.
type CreateObjectSetRequest struct {
	Name             string               `json:"name"`
	Description      string               `json:"description"`
	BaseObjectTypeID uuid.UUID            `json:"base_object_type_id"`
	Filters          []ObjectSetFilter    `json:"filters"`
	Traversals       []ObjectSetTraversal `json:"traversals"`
	Join             *ObjectSetJoin       `json:"join,omitempty"`
	Projections      []string             `json:"projections"`
	WhatIfLabel      *string              `json:"what_if_label,omitempty"`
	Policy           ObjectSetPolicy      `json:"policy"`
}

// UnmarshalJSON applies Rust `#[serde(default)]` defaults.
func (r *CreateObjectSetRequest) UnmarshalJSON(b []byte) error {
	type alias CreateObjectSetRequest
	var raw alias
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*r = CreateObjectSetRequest(raw)
	if r.Filters == nil {
		r.Filters = []ObjectSetFilter{}
	}
	if r.Traversals == nil {
		r.Traversals = []ObjectSetTraversal{}
	}
	if r.Projections == nil {
		r.Projections = []string{}
	}
	if r.Policy.AllowedMarkings == nil {
		r.Policy.AllowedMarkings = []string{}
	}
	return nil
}

// UpdateObjectSetRequest mirrors `struct UpdateObjectSetRequest`.
type UpdateObjectSetRequest struct {
	Name             *string               `json:"name,omitempty"`
	Description      *string               `json:"description,omitempty"`
	BaseObjectTypeID *uuid.UUID            `json:"base_object_type_id,omitempty"`
	Filters          *[]ObjectSetFilter    `json:"filters,omitempty"`
	Traversals       *[]ObjectSetTraversal `json:"traversals,omitempty"`
	Join             *ObjectSetJoin        `json:"join,omitempty"`
	Projections      *[]string             `json:"projections,omitempty"`
	WhatIfLabel      *string               `json:"what_if_label,omitempty"`
	Policy           *ObjectSetPolicy      `json:"policy,omitempty"`
}

// EvaluateObjectSetRequest mirrors `struct EvaluateObjectSetRequest`.
type EvaluateObjectSetRequest struct {
	Limit *int `json:"limit,omitempty"`
}

// ObjectSetEvaluationResponse mirrors `struct ObjectSetEvaluationResponse`.
type ObjectSetEvaluationResponse struct {
	ObjectSet              ObjectSetDefinition `json:"object_set"`
	TotalBaseMatches       int                 `json:"total_base_matches"`
	TotalRows              int                 `json:"total_rows"`
	TraversalNeighborCount int                 `json:"traversal_neighbor_count"`
	Rows                   []json.RawMessage   `json:"rows"`
	GeneratedAt            time.Time           `json:"generated_at"`
	Materialized           bool                `json:"materialized"`
}

// ListObjectSetsResponse mirrors `struct ListObjectSetsResponse`.
// `next_token` carries `skip_serializing_if = "Option::is_none"`.
type ListObjectSetsResponse struct {
	Data      []ObjectSetDefinition `json:"data"`
	NextToken *string               `json:"next_token,omitempty"`
}
