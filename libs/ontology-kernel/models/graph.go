package models

import (
	"encoding/json"

	"github.com/google/uuid"
)

// GraphQuery mirrors `libs/ontology-kernel/src/models/graph.rs`
// `struct GraphQuery`.
type GraphQuery struct {
	RootObjectID *uuid.UUID `json:"root_object_id,omitempty"`
	RootTypeID   *uuid.UUID `json:"root_type_id,omitempty"`
	Depth        *int       `json:"depth,omitempty"`
	Limit        *int       `json:"limit,omitempty"`
}

// GraphNode mirrors `struct GraphNode`. `metadata` is a passthrough
// JSON value to mirror Rust's `serde_json::Value`.
type GraphNode struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Label          string          `json:"label"`
	SecondaryLabel *string         `json:"secondary_label"`
	Color          *string         `json:"color"`
	Route          *string         `json:"route"`
	Metadata       json.RawMessage `json:"metadata"`
}

// GraphEdge mirrors `struct GraphEdge`.
type GraphEdge struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Source   string          `json:"source"`
	Target   string          `json:"target"`
	Label    string          `json:"label"`
	Metadata json.RawMessage `json:"metadata"`
}

// GraphSummary mirrors `struct GraphSummary`. `BTreeMap<String, usize>`
// in Rust serialises with sorted keys; `map[string]int` in Go ditto via
// `encoding/json` which already sorts map keys lexicographically.
type GraphSummary struct {
	Scope               string         `json:"scope"`
	NodeKinds           map[string]int `json:"node_kinds"`
	EdgeKinds           map[string]int `json:"edge_kinds"`
	ObjectTypes         map[string]int `json:"object_types"`
	Markings            map[string]int `json:"markings"`
	RootNeighborCount   int            `json:"root_neighbor_count"`
	MaxHopsReached      int            `json:"max_hops_reached"`
	BoundaryCrossings   int            `json:"boundary_crossings"`
	SensitiveObjects    int            `json:"sensitive_objects"`
	SensitiveMarkings   []string       `json:"sensitive_markings"`
}

// GraphResponse mirrors `struct GraphResponse`.
type GraphResponse struct {
	Mode         string       `json:"mode"`
	RootObjectID *uuid.UUID   `json:"root_object_id"`
	RootTypeID   *uuid.UUID   `json:"root_type_id"`
	Depth        int          `json:"depth"`
	TotalNodes   int          `json:"total_nodes"`
	TotalEdges   int          `json:"total_edges"`
	Summary      GraphSummary `json:"summary"`
	Nodes        []GraphNode  `json:"nodes"`
	Edges        []GraphEdge  `json:"edges"`
}
