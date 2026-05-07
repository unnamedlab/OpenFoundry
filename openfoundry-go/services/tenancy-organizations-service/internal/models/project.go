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
