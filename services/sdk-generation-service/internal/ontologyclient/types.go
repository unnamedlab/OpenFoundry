// Package ontologyclient fetches typed ontology snapshots for the OSDK
// generator. The DTOs are *local* copies of the relevant fields from
// ontology-definition-service — we do not import that package
// directly (zero cross-service Go imports), the contract lives at the
// HTTP boundary.
package ontologyclient

// OntologySnapshot is the contract the OSDK generator consumes. It is
// a structural subset of ontology-definition-service's catalog,
// pinned to one ontology version. Fields are deliberately string-typed
// rather than enum-typed: the generator collapses property kinds via
// internal/generator/ts.MapType, so the snapshot stays decoupled from
// the property-type taxonomy on the producer side.
type OntologySnapshot struct {
	Version     string                 `json:"version"`
	ObjectTypes []OntologyObjectType   `json:"object_types"`
	LinkTypes   []OntologyLinkType     `json:"link_types"`
	ActionTypes []OntologyActionType   `json:"action_types"`
}

// OntologyObjectType is one object class in the snapshot.
type OntologyObjectType struct {
	Name              string               `json:"name"`
	APIName           string               `json:"api_name"`
	DisplayName       string               `json:"display_name,omitempty"`
	Description       string               `json:"description,omitempty"`
	PrimaryKeyProperty string              `json:"primary_key_property,omitempty"`
	Properties        []OntologyProperty   `json:"properties"`
}

// OntologyProperty is one column on an object type. PropertyType is
// the upstream wire vocabulary ("string", "integer", "datetime",
// "geo_point", "timeseries", "array<string>" …); the generator owns
// the mapping to TS/Python/Java.
type OntologyProperty struct {
	Name         string `json:"name"`
	PropertyType string `json:"property_type"`
	Required     bool   `json:"required"`
	Description  string `json:"description,omitempty"`
}

// OntologyLinkType is one directed link between two object types.
// SourceObjectType / TargetObjectType reference the OntologyObjectType
// .Name field — link wiring happens in the generator so the snapshot
// stays cheap to fetch and parse.
type OntologyLinkType struct {
	Name              string `json:"name"`
	APIName           string `json:"api_name"`
	SourceObjectType  string `json:"source_object_type"`
	TargetObjectType  string `json:"target_object_type"`
	Cardinality       string `json:"cardinality"`
	Label             string `json:"label,omitempty"`
	ReverseLabel      string `json:"reverse_label,omitempty"`
}

// OntologyActionType is one applyable action. The generator emits one
// async function per ActionType under `client.actions.<name>()`.
type OntologyActionType struct {
	Name        string                    `json:"name"`
	APIName     string                    `json:"api_name"`
	DisplayName string                    `json:"display_name,omitempty"`
	Description string                    `json:"description,omitempty"`
	Parameters  []OntologyActionParameter `json:"parameters"`
}

// OntologyActionParameter is one typed argument to an ActionType.
type OntologyActionParameter struct {
	Name         string `json:"name"`
	PropertyType string `json:"property_type"`
	Required     bool   `json:"required"`
	Description  string `json:"description,omitempty"`
}
