package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// OntologyProjectRole mirrors the Rust `OntologyProjectRole` enum:
// `viewer < editor < owner`. The wire spelling is the snake_case lowercase
// form, byte-exact with the Rust `serde(rename_all = "snake_case")` repr.
type OntologyProjectRole string

const (
	OntologyProjectRoleViewer OntologyProjectRole = "viewer"
	OntologyProjectRoleEditor OntologyProjectRole = "editor"
	OntologyProjectRoleOwner  OntologyProjectRole = "owner"
)

// Rank returns the lattice rank: viewer=1 < editor=2 < owner=3.
func (r OntologyProjectRole) Rank() uint8 {
	switch r {
	case OntologyProjectRoleViewer:
		return 1
	case OntologyProjectRoleEditor:
		return 2
	case OntologyProjectRoleOwner:
		return 3
	}
	return 0
}

// ParseOntologyProjectRole resolves a wire string to an
// OntologyProjectRole, returning an error for unknown values.
func ParseOntologyProjectRole(value string) (OntologyProjectRole, error) {
	switch strings.TrimSpace(value) {
	case string(OntologyProjectRoleViewer):
		return OntologyProjectRoleViewer, nil
	case string(OntologyProjectRoleEditor):
		return OntologyProjectRoleEditor, nil
	case string(OntologyProjectRoleOwner):
		return OntologyProjectRoleOwner, nil
	}
	return "", fmt.Errorf("ontology_project_role '%s' is not supported; expected one of: viewer, editor, owner", value)
}

// OntologyProject mirrors `models::project::OntologyProject` (Rust).
type OntologyProject struct {
	ID            uuid.UUID `json:"id"`
	Slug          string    `json:"slug"`
	DisplayName   string    `json:"display_name"`
	Description   string    `json:"description"`
	WorkspaceSlug *string   `json:"workspace_slug"`
	OwnerID       uuid.UUID `json:"owner_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// OntologyProjectMembership mirrors `models::project::OntologyProjectMembership`.
type OntologyProjectMembership struct {
	ProjectID uuid.UUID           `json:"project_id"`
	UserID    uuid.UUID           `json:"user_id"`
	Role      OntologyProjectRole `json:"role"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// OntologyProjectResourceBinding mirrors `models::project::OntologyProjectResourceBinding`.
type OntologyProjectResourceBinding struct {
	ProjectID    uuid.UUID `json:"project_id"`
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
	BoundBy      uuid.UUID `json:"bound_by"`
	CreatedAt    time.Time `json:"created_at"`
}

// OntologyProjectFolder mirrors `models::project::OntologyProjectFolder`.
type OntologyProjectFolder struct {
	ID             uuid.UUID  `json:"id"`
	ProjectID      uuid.UUID  `json:"project_id"`
	ParentFolderID *uuid.UUID `json:"parent_folder_id"`
	Name           string     `json:"name"`
	Slug           string     `json:"slug"`
	Description    string     `json:"description"`
	CreatedBy      uuid.UUID  `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// CreateOntologyProjectFolderRequest is the body of POST /projects/:id/folders.
type CreateOntologyProjectFolderRequest struct {
	Name           string     `json:"name"`
	Description    *string    `json:"description,omitempty"`
	ParentFolderID *uuid.UUID `json:"parent_folder_id,omitempty"`
}

// CreateOntologyProjectRequest is the body of POST /projects.
type CreateOntologyProjectRequest struct {
	Slug          string                               `json:"slug"`
	DisplayName   *string                              `json:"display_name,omitempty"`
	Description   *string                              `json:"description,omitempty"`
	WorkspaceSlug *string                              `json:"workspace_slug,omitempty"`
	Folders       []CreateOntologyProjectFolderRequest `json:"folders,omitempty"`
}

// UpdateOntologyProjectRequest is the body of PATCH /projects/:id.
//
// Rust's `workspace_slug: Option<Option<String>>` is collapsed to `*string`
// here, matching `UpdateOrganizationRequest`'s convention in models.go: the
// triple-state (absent / null / set) is interpreted at the handler layer.
type UpdateOntologyProjectRequest struct {
	DisplayName   *string `json:"display_name,omitempty"`
	Description   *string `json:"description,omitempty"`
	WorkspaceSlug *string `json:"workspace_slug,omitempty"`
}

// ListOntologyProjectsQuery is the query string for GET /projects.
type ListOntologyProjectsQuery struct {
	Search  *string `json:"search,omitempty"`
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
}

// ListOntologyProjectsResponse is the body of GET /projects.
type ListOntologyProjectsResponse struct {
	Data    []OntologyProject `json:"data"`
	Total   int64             `json:"total"`
	Page    int64             `json:"page"`
	PerPage int64             `json:"per_page"`
}

// UpsertOntologyProjectMembershipRequest is the body of PUT /projects/:id/memberships.
type UpsertOntologyProjectMembershipRequest struct {
	UserID uuid.UUID           `json:"user_id"`
	Role   OntologyProjectRole `json:"role"`
}

// ListOntologyProjectMembershipsResponse is the body of GET /projects/:id/memberships.
type ListOntologyProjectMembershipsResponse struct {
	Data []OntologyProjectMembership `json:"data"`
}

// BindOntologyProjectResourceRequest is the body of POST /projects/:id/resources.
type BindOntologyProjectResourceRequest struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
}

// ListOntologyProjectResourcesResponse is the body of GET /projects/:id/resources.
type ListOntologyProjectResourcesResponse struct {
	Data []OntologyProjectResourceBinding `json:"data"`
}

// ListOntologyProjectFoldersResponse is the body of GET /projects/:id/folders.
type ListOntologyProjectFoldersResponse struct {
	Data []OntologyProjectFolder `json:"data"`
}
