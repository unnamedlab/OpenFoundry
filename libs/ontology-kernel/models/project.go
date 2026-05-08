package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OntologyProjectRole mirrors `enum OntologyProjectRole` —
// snake_case both in serde and sqlx.
type OntologyProjectRole string

const (
	OntologyProjectRoleViewer OntologyProjectRole = "viewer"
	OntologyProjectRoleEditor OntologyProjectRole = "editor"
	OntologyProjectRoleOwner  OntologyProjectRole = "owner"
)

// Rank mirrors `impl OntologyProjectRole::rank(self) -> u8`.
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

// OntologyProject mirrors `struct OntologyProject`.
type OntologyProject struct {
	ID            uuid.UUID `json:"id"             db:"id"`
	Slug          string    `json:"slug"           db:"slug"`
	DisplayName   string    `json:"display_name"   db:"display_name"`
	Description   string    `json:"description"    db:"description"`
	WorkspaceSlug *string   `json:"workspace_slug" db:"workspace_slug"`
	OwnerID       uuid.UUID `json:"owner_id"       db:"owner_id"`
	CreatedAt     time.Time `json:"created_at"     db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"     db:"updated_at"`
}

// OntologyProjectMembership mirrors `struct OntologyProjectMembership`.
type OntologyProjectMembership struct {
	ProjectID uuid.UUID           `json:"project_id" db:"project_id"`
	UserID    uuid.UUID           `json:"user_id"    db:"user_id"`
	Role      OntologyProjectRole `json:"role"       db:"role"`
	CreatedAt time.Time           `json:"created_at" db:"created_at"`
	UpdatedAt time.Time           `json:"updated_at" db:"updated_at"`
}

// OntologyProjectResourceBinding mirrors `struct OntologyProjectResourceBinding`.
type OntologyProjectResourceBinding struct {
	ProjectID    uuid.UUID `json:"project_id"   db:"project_id"`
	ResourceKind string    `json:"resource_kind" db:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"  db:"resource_id"`
	BoundBy      uuid.UUID `json:"bound_by"     db:"bound_by"`
	CreatedAt    time.Time `json:"created_at"   db:"created_at"`
}

// CreateOntologyProjectRequest mirrors `struct CreateOntologyProjectRequest`.
type CreateOntologyProjectRequest struct {
	Slug          string  `json:"slug"`
	DisplayName   *string `json:"display_name,omitempty"`
	Description   *string `json:"description,omitempty"`
	WorkspaceSlug *string `json:"workspace_slug,omitempty"`
}

// UpdateOntologyProjectRequest mirrors `struct UpdateOntologyProjectRequest`.
// `workspace_slug: Option<Option<String>>` three-way semantics.
type UpdateOntologyProjectRequest struct {
	DisplayName   *string       `json:"display_name,omitempty"`
	Description   *string       `json:"description,omitempty"`
	WorkspaceSlug *StringUpdate `json:"-"`
}

// UnmarshalJSON detects key presence for workspace_slug.
func (r *UpdateOntologyProjectRequest) UnmarshalJSON(b []byte) error {
	type alias UpdateOntologyProjectRequest
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if v, ok := raw["workspace_slug"]; ok {
		upd := &StringUpdate{}
		if string(v) != "null" {
			var s string
			if err := json.Unmarshal(v, &s); err != nil {
				return err
			}
			upd.Value = &s
		}
		r.WorkspaceSlug = upd
	}
	return nil
}

// MarshalJSON: emit absent / null / value for workspace_slug.
func (r UpdateOntologyProjectRequest) MarshalJSON() ([]byte, error) {
	type alias UpdateOntologyProjectRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if r.WorkspaceSlug == nil {
		return base, nil
	}
	bag := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &bag); err != nil {
		return nil, err
	}
	if r.WorkspaceSlug.Value == nil {
		bag["workspace_slug"] = json.RawMessage("null")
	} else {
		v, _ := json.Marshal(*r.WorkspaceSlug.Value)
		bag["workspace_slug"] = v
	}
	return json.Marshal(bag)
}

// ListOntologyProjectsQuery mirrors the same struct.
type ListOntologyProjectsQuery struct {
	Search  *string `json:"search,omitempty"`
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
}

// ListOntologyProjectsResponse mirrors the same struct.
type ListOntologyProjectsResponse struct {
	Data    []OntologyProject `json:"data"`
	Total   int64             `json:"total"`
	Page    int64             `json:"page"`
	PerPage int64             `json:"per_page"`
}

// UpsertOntologyProjectMembershipRequest mirrors the same struct.
type UpsertOntologyProjectMembershipRequest struct {
	UserID uuid.UUID           `json:"user_id"`
	Role   OntologyProjectRole `json:"role"`
}

// ListOntologyProjectMembershipsResponse mirrors the same struct.
type ListOntologyProjectMembershipsResponse struct {
	Data []OntologyProjectMembership `json:"data"`
}

// BindOntologyProjectResourceRequest mirrors the same struct.
type BindOntologyProjectResourceRequest struct {
	ResourceKind string    `json:"resource_kind"`
	ResourceID   uuid.UUID `json:"resource_id"`
}

// ListOntologyProjectResourcesResponse mirrors the same struct.
type ListOntologyProjectResourcesResponse struct {
	Data []OntologyProjectResourceBinding `json:"data"`
}

// OntologyProjectWorkingState mirrors `struct OntologyProjectWorkingState`.
type OntologyProjectWorkingState struct {
	ProjectID uuid.UUID       `json:"project_id" db:"project_id"`
	Changes   json.RawMessage `json:"changes"    db:"changes"`
	UpdatedBy uuid.UUID       `json:"updated_by" db:"updated_by"`
	UpdatedAt time.Time       `json:"updated_at" db:"updated_at"`
}

// ReplaceOntologyProjectWorkingStateRequest mirrors the same struct.
type ReplaceOntologyProjectWorkingStateRequest struct {
	Changes json.RawMessage `json:"changes"`
}

// OntologyProjectBranch mirrors `struct OntologyProjectBranch`.
type OntologyProjectBranch struct {
	ID                  uuid.UUID       `json:"id"                   db:"id"`
	ProjectID           uuid.UUID       `json:"project_id"           db:"project_id"`
	Name                string          `json:"name"                 db:"name"`
	Description         string          `json:"description"          db:"description"`
	Status              string          `json:"status"               db:"status"`
	ProposalID          *uuid.UUID      `json:"proposal_id"          db:"proposal_id"`
	Changes             json.RawMessage `json:"changes"              db:"changes"`
	ConflictResolutions json.RawMessage `json:"conflict_resolutions" db:"conflict_resolutions"`
	EnableIndexing      bool            `json:"enable_indexing"      db:"enable_indexing"`
	CreatedBy           uuid.UUID       `json:"created_by"           db:"created_by"`
	CreatedAt           time.Time       `json:"created_at"           db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"           db:"updated_at"`
	LatestRebasedAt     time.Time       `json:"latest_rebased_at"    db:"latest_rebased_at"`
}

// CreateOntologyProjectBranchRequest mirrors the same struct.
type CreateOntologyProjectBranchRequest struct {
	Name           string          `json:"name"`
	Description    *string         `json:"description,omitempty"`
	Changes        json.RawMessage `json:"changes"`
	EnableIndexing *bool           `json:"enable_indexing,omitempty"`
}

// UpdateOntologyProjectBranchRequest mirrors `struct
// UpdateOntologyProjectBranchRequest`. `proposal_id:
// Option<Option<Uuid>>` three-way semantics.
type UpdateOntologyProjectBranchRequest struct {
	Description         *string         `json:"description,omitempty"`
	Status              *string         `json:"status,omitempty"`
	ProposalID          *UUIDUpdate     `json:"-"`
	Changes             json.RawMessage `json:"changes,omitempty"`
	ConflictResolutions json.RawMessage `json:"conflict_resolutions,omitempty"`
	EnableIndexing      *bool           `json:"enable_indexing,omitempty"`
	LatestRebasedAt     *time.Time      `json:"latest_rebased_at,omitempty"`
}

// UnmarshalJSON detects key presence for proposal_id.
func (r *UpdateOntologyProjectBranchRequest) UnmarshalJSON(b []byte) error {
	type alias UpdateOntologyProjectBranchRequest
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if v, ok := raw["proposal_id"]; ok {
		upd := &UUIDUpdate{}
		if string(v) != "null" {
			var id uuid.UUID
			if err := json.Unmarshal(v, &id); err != nil {
				return err
			}
			upd.Value = &id
		}
		r.ProposalID = upd
	}
	return nil
}

// MarshalJSON: emit absent / null / value for proposal_id.
func (r UpdateOntologyProjectBranchRequest) MarshalJSON() ([]byte, error) {
	type alias UpdateOntologyProjectBranchRequest
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if r.ProposalID == nil {
		return base, nil
	}
	bag := map[string]json.RawMessage{}
	if err := json.Unmarshal(base, &bag); err != nil {
		return nil, err
	}
	if r.ProposalID.Value == nil {
		bag["proposal_id"] = json.RawMessage("null")
	} else {
		v, _ := json.Marshal(*r.ProposalID.Value)
		bag["proposal_id"] = v
	}
	return json.Marshal(bag)
}

// ListOntologyProjectBranchesResponse mirrors the same struct.
type ListOntologyProjectBranchesResponse struct {
	Data []OntologyProjectBranch `json:"data"`
}

// OntologyProjectProposal mirrors `struct OntologyProjectProposal`.
type OntologyProjectProposal struct {
	ID          uuid.UUID       `json:"id"          db:"id"`
	ProjectID   uuid.UUID       `json:"project_id"  db:"project_id"`
	BranchID    uuid.UUID       `json:"branch_id"   db:"branch_id"`
	Title       string          `json:"title"       db:"title"`
	Description string          `json:"description" db:"description"`
	Status      string          `json:"status"      db:"status"`
	ReviewerIDs json.RawMessage `json:"reviewer_ids" db:"reviewer_ids"`
	Tasks       json.RawMessage `json:"tasks"       db:"tasks"`
	Comments    json.RawMessage `json:"comments"    db:"comments"`
	CreatedBy   uuid.UUID       `json:"created_by"  db:"created_by"`
	CreatedAt   time.Time       `json:"created_at"  db:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"  db:"updated_at"`
}

// CreateOntologyProjectProposalRequest mirrors the same struct.
type CreateOntologyProjectProposalRequest struct {
	BranchID    uuid.UUID       `json:"branch_id"`
	Title       string          `json:"title"`
	Description *string         `json:"description,omitempty"`
	ReviewerIDs json.RawMessage `json:"reviewer_ids,omitempty"`
	Tasks       json.RawMessage `json:"tasks"`
	Comments    json.RawMessage `json:"comments,omitempty"`
}

// UpdateOntologyProjectProposalRequest mirrors the same struct.
type UpdateOntologyProjectProposalRequest struct {
	Title       *string         `json:"title,omitempty"`
	Description *string         `json:"description,omitempty"`
	Status      *string         `json:"status,omitempty"`
	ReviewerIDs json.RawMessage `json:"reviewer_ids,omitempty"`
	Tasks       json.RawMessage `json:"tasks,omitempty"`
	Comments    json.RawMessage `json:"comments,omitempty"`
}

// ListOntologyProjectProposalsResponse mirrors the same struct.
type ListOntologyProjectProposalsResponse struct {
	Data []OntologyProjectProposal `json:"data"`
}

// OntologyProjectMigration mirrors `struct OntologyProjectMigration`.
type OntologyProjectMigration struct {
	ID                uuid.UUID       `json:"id"                 db:"id"`
	ProjectID         uuid.UUID       `json:"project_id"         db:"project_id"`
	SourceProjectID   uuid.UUID       `json:"source_project_id"  db:"source_project_id"`
	TargetProjectID   uuid.UUID       `json:"target_project_id"  db:"target_project_id"`
	Resources         json.RawMessage `json:"resources"          db:"resources"`
	SubmittedAt       time.Time       `json:"submitted_at"       db:"submitted_at"`
	Status            string          `json:"status"             db:"status"`
	Note              string          `json:"note"               db:"note"`
	SubmittedBy       uuid.UUID       `json:"submitted_by"       db:"submitted_by"`
}

// CreateOntologyProjectMigrationRequest mirrors the same struct.
type CreateOntologyProjectMigrationRequest struct {
	SourceProjectID uuid.UUID       `json:"source_project_id"`
	TargetProjectID uuid.UUID       `json:"target_project_id"`
	Resources       json.RawMessage `json:"resources"`
	Note            *string         `json:"note,omitempty"`
}

// ListOntologyProjectMigrationsResponse mirrors the same struct.
type ListOntologyProjectMigrationsResponse struct {
	Data []OntologyProjectMigration `json:"data"`
}
