// Package workspace holds shared primitives for the B3 Workspace HTTP
// surface (favorites, recents, sharing).
//
// All workspace handlers parse a `resource_kind` URL/body field. This
// package owns the canonical enum so adding a new kind only requires
// updating one file.
package workspace

import (
	"fmt"
	"strings"
)

// ResourceKind enumerates every resource the workspace surface knows
// about. Adding a new kind requires:
//
//  1. Adding a constant here.
//  2. Updating ParseResourceKind / IsValid.
//  3. Wiring trash/sharing/move handlers for the new kind.
type ResourceKind string

const (
	// Ontology workspace (owned by ontology-definition-service).
	ResourceOntologyProject         ResourceKind = "ontology_project"
	ResourceOntologyFolder          ResourceKind = "ontology_folder"
	ResourceOntologyResourceBinding ResourceKind = "ontology_resource_binding"
	// Other workspace surfaces. Sharing / favorites / recents accept
	// these but trash/move are delegated to the resource-owning service.
	ResourceDataset   ResourceKind = "dataset"
	ResourcePipeline  ResourceKind = "pipeline"
	ResourceQuery     ResourceKind = "query"
	ResourceNotebook  ResourceKind = "notebook"
	ResourceApp       ResourceKind = "app"
	ResourceDashboard ResourceKind = "dashboard"
	ResourceReport    ResourceKind = "report"
	ResourceModel     ResourceKind = "model"
	ResourceWorkflow  ResourceKind = "workflow"
	ResourceOther     ResourceKind = "other"
)

// allKinds preserves the canonical wire spelling so error messages list
// every accepted value (Rust impl returns the same set).
var allKinds = []ResourceKind{
	ResourceOntologyProject, ResourceOntologyFolder, ResourceOntologyResourceBinding,
	ResourceDataset, ResourcePipeline, ResourceQuery, ResourceNotebook, ResourceApp,
	ResourceDashboard, ResourceReport, ResourceModel, ResourceWorkflow, ResourceOther,
}

// ParseResourceKind parses a wire string into a ResourceKind. Mirrors
// the Rust ResourceKind::parse — note the legacy aliases:
//
//   - "project"          → ontology_project
//   - "folder"           → ontology_folder
//   - "resource_binding" → ontology_resource_binding
func ParseResourceKind(value string) (ResourceKind, error) {
	switch strings.TrimSpace(value) {
	case "ontology_project", "project":
		return ResourceOntologyProject, nil
	case "ontology_folder", "folder":
		return ResourceOntologyFolder, nil
	case "ontology_resource_binding", "resource_binding":
		return ResourceOntologyResourceBinding, nil
	case string(ResourceDataset):
		return ResourceDataset, nil
	case string(ResourcePipeline):
		return ResourcePipeline, nil
	case string(ResourceQuery):
		return ResourceQuery, nil
	case string(ResourceNotebook):
		return ResourceNotebook, nil
	case string(ResourceApp):
		return ResourceApp, nil
	case string(ResourceDashboard):
		return ResourceDashboard, nil
	case string(ResourceReport):
		return ResourceReport, nil
	case string(ResourceModel):
		return ResourceModel, nil
	case string(ResourceWorkflow):
		return ResourceWorkflow, nil
	case string(ResourceOther):
		return ResourceOther, nil
	}
	return "", fmt.Errorf("resource_kind '%s' is not supported; expected one of: %s",
		value, joinKinds())
}

// IsValid reports whether a kind value matches a canonical spelling.
func (k ResourceKind) IsValid() bool {
	for _, c := range allKinds {
		if c == k {
			return true
		}
	}
	return false
}

func joinKinds() string {
	out := make([]string, 0, len(allKinds))
	for _, k := range allKinds {
		out = append(out, string(k))
	}
	return strings.Join(out, ", ")
}
