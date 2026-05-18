// Package resource contains the Compass-style resource type registry shared by
// OpenFoundry services.
package resource

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/core-models/rid"
)

// TypeID is the stable registry id for a Compass resource type.
type TypeID string

const (
	TypeUnknown             TypeID = "UNKNOWN_RESOURCE_TYPE"
	TypeCompassProject      TypeID = "COMPASS_PROJECT"
	TypeCompassFolder       TypeID = "COMPASS_FOLDER"
	TypeFoundryDataset      TypeID = "FOUNDRY_DATASET"
	TypeFoundryPipeline     TypeID = "FOUNDRY_PIPELINE"
	TypeFoundryBuild        TypeID = "FOUNDRY_BUILD"
	TypeFoundryJob          TypeID = "FOUNDRY_JOB"
	TypeFoundrySchedule     TypeID = "FOUNDRY_SCHEDULE"
	TypeFoundryQuery        TypeID = "FOUNDRY_QUERY"
	TypeFoundrySource       TypeID = "FOUNDRY_SOURCE"
	TypeFoundryVirtualTable TypeID = "FOUNDRY_VIRTUAL_TABLE"
	TypeStreamsStream       TypeID = "STREAMS_STREAM"
	TypeOntologyObjectType  TypeID = "ONTOLOGY_OBJECT_TYPE"
	TypeOntologyActionType  TypeID = "ONTOLOGY_ACTION_TYPE"
	TypeWorkshopApp         TypeID = "WORKSHOP_APP"
	TypeWorkshopDashboard   TypeID = "WORKSHOP_DASHBOARD"
	TypeReportReport        TypeID = "REPORT_REPORT"
	TypeNotepadDocument     TypeID = "NOTEPAD_NOTEPAD"
	TypeNotebookNotebook    TypeID = "NOTEBOOK_NOTEBOOK"
	TypeModelsModel         TypeID = "MODELS_MODEL"
	TypeFoundryWorkflow     TypeID = "FOUNDRY_WORKFLOW"
)

// Action is an operation Compass can expose for a resource type.
type Action string

const (
	ActionMove    Action = "move"
	ActionRename  Action = "rename"
	ActionTrash   Action = "trash"
	ActionRestore Action = "restore"
	ActionShare   Action = "share"
)

var (
	typeIDPattern  = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	actionRegistry = map[Action]struct{}{
		ActionMove:    {},
		ActionRename:  {},
		ActionTrash:   {},
		ActionRestore: {},
		ActionShare:   {},
	}
)

// RIDNamespace maps RID service/type components to a resource type.
// Instance is intentionally ignored so the same type can exist in multiple
// clusters or tenants.
type RIDNamespace struct {
	Service      string `json:"service"`
	ResourceType string `json:"resource_type"`
}

// ReferenceTarget declares one kind of resource a type may depend on.
type ReferenceTarget struct {
	Relationship string `json:"relationship"`
	TargetType   TypeID `json:"target_type"`
	Required     bool   `json:"required,omitempty"`
}

// TypeDefinition is one central registry entry for a resource type.
type TypeDefinition struct {
	ID                 TypeID            `json:"id"`
	DisplayName        string            `json:"display_name"`
	OwningService      string            `json:"owning_service"`
	RIDService         string            `json:"rid_service"`
	RIDResourceType    string            `json:"rid_resource_type"`
	RIDAliases         []RIDNamespace    `json:"rid_aliases,omitempty"`
	DefaultIcon        string            `json:"default_icon"`
	SupportedActions   []Action          `json:"supported_actions"`
	OpenAppURLTemplate string            `json:"open_app_url_template"`
	ReferenceTargets   []ReferenceTarget `json:"reference_targets,omitempty"`
}

// Supports reports whether the resource type exposes an action.
func (d TypeDefinition) Supports(action Action) bool {
	for _, candidate := range d.SupportedActions {
		if candidate == action {
			return true
		}
	}
	return false
}

// OpenURL expands the definition's URL template for a concrete RID string.
func (d TypeDefinition) OpenURL(resourceRID string) (string, error) {
	parsed, err := rid.Parse(resourceRID)
	if err != nil {
		return "", err
	}
	replacer := strings.NewReplacer(
		"{rid}", parsed.String(),
		"{service}", parsed.Service,
		"{instance}", parsed.Instance,
		"{type}", parsed.ResourceType,
		"{locator}", parsed.Locator,
	)
	return replacer.Replace(d.OpenAppURLTemplate), nil
}

func (d TypeDefinition) validate() error {
	if d.ID == "" || !typeIDPattern.MatchString(string(d.ID)) {
		return fmt.Errorf("resource type id %q must match %s", d.ID, typeIDPattern.String())
	}
	if strings.TrimSpace(d.DisplayName) == "" {
		return fmt.Errorf("resource type %s missing display name", d.ID)
	}
	if strings.TrimSpace(d.OwningService) == "" {
		return fmt.Errorf("resource type %s missing owning service", d.ID)
	}
	if strings.TrimSpace(d.DefaultIcon) == "" {
		return fmt.Errorf("resource type %s missing default icon", d.ID)
	}
	if strings.TrimSpace(d.OpenAppURLTemplate) == "" || !strings.Contains(d.OpenAppURLTemplate, "{rid}") {
		return fmt.Errorf("resource type %s open-app URL template must include {rid}", d.ID)
	}
	if _, err := rid.New(d.RIDService, rid.DefaultInstance, d.RIDResourceType, "0"); err != nil {
		return fmt.Errorf("resource type %s invalid RID namespace: %w", d.ID, err)
	}
	for _, alias := range d.RIDAliases {
		if _, err := rid.New(alias.Service, rid.DefaultInstance, alias.ResourceType, "0"); err != nil {
			return fmt.Errorf("resource type %s invalid RID alias namespace: %w", d.ID, err)
		}
	}
	for _, action := range d.SupportedActions {
		if _, ok := actionRegistry[action]; !ok {
			return fmt.Errorf("resource type %s unsupported action %q", d.ID, action)
		}
	}
	for _, target := range d.ReferenceTargets {
		if strings.TrimSpace(target.Relationship) == "" {
			return fmt.Errorf("resource type %s reference target missing relationship", d.ID)
		}
		if target.TargetType == "" {
			return fmt.Errorf("resource type %s reference target missing target_type", d.ID)
		}
	}
	return nil
}

func (d TypeDefinition) namespaces() []RIDNamespace {
	out := []RIDNamespace{{Service: d.RIDService, ResourceType: d.RIDResourceType}}
	out = append(out, d.RIDAliases...)
	return out
}

// Registry is an immutable in-process registry of resource type definitions.
type Registry struct {
	byID        map[TypeID]TypeDefinition
	byNamespace map[RIDNamespace]TypeID
	placeholder TypeDefinition
}

// NewRegistry validates and builds a registry. Duplicate ids or RID namespaces
// are rejected at construction time, which makes adding a new type an explicit
// registry change.
func NewRegistry(definitions ...TypeDefinition) (*Registry, error) {
	out := &Registry{
		byID:        make(map[TypeID]TypeDefinition, len(definitions)),
		byNamespace: make(map[RIDNamespace]TypeID, len(definitions)),
		placeholder: PlaceholderDefinition(),
	}
	for _, definition := range definitions {
		if err := definition.validate(); err != nil {
			return nil, err
		}
		if _, exists := out.byID[definition.ID]; exists {
			return nil, fmt.Errorf("duplicate resource type id %s", definition.ID)
		}
		definition.SupportedActions = append([]Action(nil), definition.SupportedActions...)
		definition.RIDAliases = append([]RIDNamespace(nil), definition.RIDAliases...)
		definition.ReferenceTargets = append([]ReferenceTarget(nil), definition.ReferenceTargets...)
		out.byID[definition.ID] = definition
		for _, namespace := range definition.namespaces() {
			if existing, exists := out.byNamespace[namespace]; exists {
				return nil, fmt.Errorf("RID namespace ri.%s.<instance>.%s already registered to %s", namespace.Service, namespace.ResourceType, existing)
			}
			out.byNamespace[namespace] = definition.ID
		}
	}
	for _, definition := range out.byID {
		for _, target := range definition.ReferenceTargets {
			if _, exists := out.byID[target.TargetType]; !exists {
				return nil, fmt.Errorf("resource type %s reference target %s is not registered", definition.ID, target.TargetType)
			}
		}
	}
	return out, nil
}

// MustNewRegistry is NewRegistry for package-level definitions.
func MustNewRegistry(definitions ...TypeDefinition) *Registry {
	out, err := NewRegistry(definitions...)
	if err != nil {
		panic(err)
	}
	return out
}

// Lookup returns a registered type by id.
func (r *Registry) Lookup(id TypeID) (TypeDefinition, bool) {
	if r == nil {
		return PlaceholderDefinition(), false
	}
	definition, ok := r.byID[id]
	if !ok {
		return r.placeholder, false
	}
	return copyDefinition(definition), true
}

// Resolve returns a registered type or the placeholder for unknown ids.
func (r *Registry) Resolve(id TypeID) TypeDefinition {
	definition, _ := r.Lookup(id)
	return definition
}

// LookupRID resolves a resource type from a RID's service/type namespace.
func (r *Registry) LookupRID(resourceRID string) (TypeDefinition, bool, error) {
	parsed, err := rid.Parse(resourceRID)
	if err != nil {
		return PlaceholderDefinition(), false, err
	}
	definition, ok := r.LookupNamespace(parsed.Service, parsed.ResourceType)
	return definition, ok, nil
}

// ResolveRID returns the registered type for a RID or the placeholder when the
// RID is valid but its namespace is not registered.
func (r *Registry) ResolveRID(resourceRID string) (TypeDefinition, bool) {
	definition, ok, err := r.LookupRID(resourceRID)
	if err != nil {
		return PlaceholderDefinition(), false
	}
	return definition, ok
}

// LookupNamespace resolves a type from RID service/type components.
func (r *Registry) LookupNamespace(service, resourceType string) (TypeDefinition, bool) {
	if r == nil {
		return PlaceholderDefinition(), false
	}
	id, ok := r.byNamespace[RIDNamespace{Service: service, ResourceType: resourceType}]
	if !ok {
		return r.placeholder, false
	}
	return r.Lookup(id)
}

// All returns all registered definitions sorted by id.
func (r *Registry) All() []TypeDefinition {
	if r == nil {
		return nil
	}
	out := make([]TypeDefinition, 0, len(r.byID))
	for _, definition := range r.byID {
		out = append(out, copyDefinition(definition))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// PlaceholderDefinition is the fallback renderer contract for unknown types.
func PlaceholderDefinition() TypeDefinition {
	return TypeDefinition{
		ID:                 TypeUnknown,
		DisplayName:        "Unknown resource",
		OwningService:      "unknown",
		RIDService:         "unknown",
		RIDResourceType:    "resource",
		DefaultIcon:        "object",
		SupportedActions:   nil,
		OpenAppURLTemplate: "/search?q={rid}",
	}
}

func copyDefinition(definition TypeDefinition) TypeDefinition {
	definition.SupportedActions = append([]Action(nil), definition.SupportedActions...)
	definition.RIDAliases = append([]RIDNamespace(nil), definition.RIDAliases...)
	definition.ReferenceTargets = append([]ReferenceTarget(nil), definition.ReferenceTargets...)
	return definition
}

var defaultRegistry = MustNewRegistry(DefaultDefinitions()...)

// DefaultRegistry returns the platform resource type registry.
func DefaultRegistry() *Registry {
	return defaultRegistry
}

// DefaultDefinitions returns the built-in Compass resource type definitions.
func DefaultDefinitions() []TypeDefinition {
	common := []Action{ActionMove, ActionRename, ActionTrash, ActionRestore, ActionShare}
	readOnlyExecution := []Action{ActionShare}
	return []TypeDefinition{
		{
			ID:                 TypeCompassProject,
			DisplayName:        "Project",
			OwningService:      "ontology-definition-service",
			RIDService:         "compass",
			RIDResourceType:    "project",
			RIDAliases:         []RIDNamespace{{Service: "openfoundry", ResourceType: "project"}},
			DefaultIcon:        "project",
			SupportedActions:   common,
			OpenAppURLTemplate: "/projects/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "contains", TargetType: TypeCompassFolder},
				{Relationship: "contains", TargetType: TypeFoundryDataset},
				{Relationship: "contains", TargetType: TypeFoundryPipeline},
				{Relationship: "references", TargetType: TypeCompassProject},
			},
		},
		{
			ID:                 TypeCompassFolder,
			DisplayName:        "Folder",
			OwningService:      "ontology-definition-service",
			RIDService:         "compass",
			RIDResourceType:    "folder",
			RIDAliases:         []RIDNamespace{{Service: "openfoundry", ResourceType: "folder"}},
			DefaultIcon:        "folder",
			SupportedActions:   common,
			OpenAppURLTemplate: "/projects?folder_rid={rid}",
		},
		{
			ID:                 TypeFoundryDataset,
			DisplayName:        "Dataset",
			OwningService:      "dataset-versioning-service",
			RIDService:         "foundry",
			RIDResourceType:    "dataset",
			DefaultIcon:        "database",
			SupportedActions:   common,
			OpenAppURLTemplate: "/datasets/{rid}",
		},
		{
			ID:                 TypeFoundryPipeline,
			DisplayName:        "Pipeline",
			OwningService:      "pipeline-build-service",
			RIDService:         "foundry",
			RIDResourceType:    "pipeline",
			DefaultIcon:        "graph",
			SupportedActions:   common,
			OpenAppURLTemplate: "/pipelines/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "reads", TargetType: TypeFoundryDataset},
				{Relationship: "writes", TargetType: TypeFoundryDataset},
			},
		},
		{
			ID:                 TypeFoundryBuild,
			DisplayName:        "Build",
			OwningService:      "pipeline-build-service",
			RIDService:         "foundry",
			RIDResourceType:    "build",
			DefaultIcon:        "run",
			SupportedActions:   readOnlyExecution,
			OpenAppURLTemplate: "/builds/{rid}",
		},
		{
			ID:                 TypeFoundryJob,
			DisplayName:        "Job",
			OwningService:      "pipeline-build-service",
			RIDService:         "foundry",
			RIDResourceType:    "job",
			DefaultIcon:        "list",
			SupportedActions:   readOnlyExecution,
			OpenAppURLTemplate: "/builds/jobs/{rid}",
		},
		{
			ID:                 TypeFoundrySchedule,
			DisplayName:        "Schedule",
			OwningService:      "pipeline-build-service",
			RIDService:         "foundry",
			RIDResourceType:    "schedule",
			DefaultIcon:        "clock",
			SupportedActions:   []Action{ActionRename, ActionTrash, ActionRestore, ActionShare},
			OpenAppURLTemplate: "/schedules/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "runs", TargetType: TypeFoundryPipeline},
				{Relationship: "runs", TargetType: TypeFoundryWorkflow},
			},
		},
		{
			ID:                 TypeFoundryQuery,
			DisplayName:        "Query",
			OwningService:      "query-engine-service",
			RIDService:         "foundry",
			RIDResourceType:    "query",
			DefaultIcon:        "query",
			SupportedActions:   common,
			OpenAppURLTemplate: "/queries/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "reads", TargetType: TypeFoundryDataset},
				{Relationship: "reads", TargetType: TypeFoundryVirtualTable},
			},
		},
		{
			ID:                 TypeFoundrySource,
			DisplayName:        "Source",
			OwningService:      "connector-management-service",
			RIDService:         "foundry",
			RIDResourceType:    "source",
			DefaultIcon:        "plug",
			SupportedActions:   common,
			OpenAppURLTemplate: "/data-connection/sources/{rid}",
		},
		{
			ID:                 TypeFoundryVirtualTable,
			DisplayName:        "Virtual table",
			OwningService:      "connector-management-service",
			RIDService:         "foundry",
			RIDResourceType:    "virtual-table",
			DefaultIcon:        "table",
			SupportedActions:   common,
			OpenAppURLTemplate: "/virtual-tables/{rid}",
		},
		{
			ID:                 TypeStreamsStream,
			DisplayName:        "Stream",
			OwningService:      "ingestion-replication-service",
			RIDService:         "streams",
			RIDResourceType:    "stream",
			DefaultIcon:        "activity",
			SupportedActions:   []Action{ActionRename, ActionTrash, ActionRestore, ActionShare},
			OpenAppURLTemplate: "/streaming/{rid}",
		},
		{
			ID:                 TypeOntologyObjectType,
			DisplayName:        "Object type",
			OwningService:      "ontology-definition-service",
			RIDService:         "ontology",
			RIDResourceType:    "object-type",
			DefaultIcon:        "cube",
			SupportedActions:   common,
			OpenAppURLTemplate: "/ontology/{rid}",
		},
		{
			ID:                 TypeOntologyActionType,
			DisplayName:        "Action type",
			OwningService:      "ontology-actions-service",
			RIDService:         "ontology",
			RIDResourceType:    "action-type",
			DefaultIcon:        "run",
			SupportedActions:   common,
			OpenAppURLTemplate: "/action-types/{rid}",
		},
		{
			ID:                 TypeWorkshopApp,
			DisplayName:        "Application",
			OwningService:      "application-composition-service",
			RIDService:         "foundry",
			RIDResourceType:    "app",
			DefaultIcon:        "app",
			SupportedActions:   common,
			OpenAppURLTemplate: "/apps/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "embeds", TargetType: TypeFoundryDataset},
				{Relationship: "embeds", TargetType: TypeOntologyObjectType},
				{Relationship: "invokes", TargetType: TypeOntologyActionType},
				{Relationship: "embeds", TargetType: TypeReportReport},
				{Relationship: "embeds", TargetType: TypeWorkshopDashboard},
			},
		},
		{
			ID:                 TypeWorkshopDashboard,
			DisplayName:        "Dashboard",
			OwningService:      "application-composition-service",
			RIDService:         "foundry",
			RIDResourceType:    "dashboard",
			DefaultIcon:        "graph",
			SupportedActions:   common,
			OpenAppURLTemplate: "/dashboards/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "reads", TargetType: TypeFoundryQuery},
				{Relationship: "reads", TargetType: TypeFoundryDataset},
			},
		},
		{
			ID:                 TypeReportReport,
			DisplayName:        "Report",
			OwningService:      "report-service",
			RIDService:         "foundry",
			RIDResourceType:    "report",
			DefaultIcon:        "document",
			SupportedActions:   common,
			OpenAppURLTemplate: "/reports/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "reads", TargetType: TypeFoundryQuery},
				{Relationship: "reads", TargetType: TypeFoundryDataset},
			},
		},
		{
			ID:                 TypeNotepadDocument,
			DisplayName:        "Notepad",
			OwningService:      "application-composition-service",
			RIDService:         "foundry",
			RIDResourceType:    "notepad",
			DefaultIcon:        "document",
			SupportedActions:   common,
			OpenAppURLTemplate: "/notepad/{rid}",
		},
		{
			ID:                 TypeNotebookNotebook,
			DisplayName:        "Notebook",
			OwningService:      "notebook-runtime-service",
			RIDService:         "foundry",
			RIDResourceType:    "notebook",
			DefaultIcon:        "code",
			SupportedActions:   common,
			OpenAppURLTemplate: "/notebooks/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "reads", TargetType: TypeFoundryDataset},
				{Relationship: "writes", TargetType: TypeFoundryDataset},
			},
		},
		{
			ID:                 TypeModelsModel,
			DisplayName:        "Model",
			OwningService:      "model-catalog-service",
			RIDService:         "foundry",
			RIDResourceType:    "model",
			DefaultIcon:        "cube",
			SupportedActions:   common,
			OpenAppURLTemplate: "/ml?model={rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "trained_from", TargetType: TypeFoundryDataset},
			},
		},
		{
			ID:                 TypeFoundryWorkflow,
			DisplayName:        "Workflow",
			OwningService:      "workflow-orchestration-service",
			RIDService:         "foundry",
			RIDResourceType:    "workflow",
			DefaultIcon:        "list",
			SupportedActions:   common,
			OpenAppURLTemplate: "/workflows/{rid}",
			ReferenceTargets: []ReferenceTarget{
				{Relationship: "runs", TargetType: TypeFoundryPipeline},
				{Relationship: "reads", TargetType: TypeFoundryDataset},
			},
		},
	}
}
