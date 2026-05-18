package models

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/libs/core-models/rid"
)

// OntologyProjectRole mirrors the Rust `OntologyProjectRole` enum:
// `discoverer < viewer < editor < owner`. The wire spelling is the
// snake_case lowercase form. SG.6 (2026-05-17) added Discoverer as
// the lattice floor: a user with Discoverer can find the project
// (search, share-with-me) but cannot read its contents.
type OntologyProjectRole string

const (
	OntologyProjectRoleDiscoverer OntologyProjectRole = "discoverer"
	OntologyProjectRoleViewer     OntologyProjectRole = "viewer"
	OntologyProjectRoleEditor     OntologyProjectRole = "editor"
	OntologyProjectRoleOwner      OntologyProjectRole = "owner"
)

const (
	DefaultProjectSpaceRID       = "ri.compass.main.folder.default-space"
	FolderResourceType           = "FOLDER"
	FolderTrashStatusNotTrashed  = "NOT_TRASHED"
	FolderTrashStatusDirectTrash = "DIRECTLY_TRASHED"
)

// Rank returns the lattice rank: discoverer=1 < viewer=2 < editor=3 < owner=4.
func (r OntologyProjectRole) Rank() uint8 {
	switch r {
	case OntologyProjectRoleDiscoverer:
		return 1
	case OntologyProjectRoleViewer:
		return 2
	case OntologyProjectRoleEditor:
		return 3
	case OntologyProjectRoleOwner:
		return 4
	}
	return 0
}

// ParseOntologyProjectRole resolves a wire string to an
// OntologyProjectRole, returning an error for unknown values.
func ParseOntologyProjectRole(value string) (OntologyProjectRole, error) {
	switch strings.TrimSpace(value) {
	case string(OntologyProjectRoleDiscoverer):
		return OntologyProjectRoleDiscoverer, nil
	case string(OntologyProjectRoleViewer):
		return OntologyProjectRoleViewer, nil
	case string(OntologyProjectRoleEditor):
		return OntologyProjectRoleEditor, nil
	case string(OntologyProjectRoleOwner):
		return OntologyProjectRoleOwner, nil
	}
	return "", fmt.Errorf("ontology_project_role '%s' is not supported; expected one of: discoverer, viewer, editor, owner", value)
}

// ProjectRIDFromID builds the stable Compass RID for an OpenFoundry project.
func ProjectRIDFromID(id uuid.UUID) string {
	return rid.MustNewUUID("compass", rid.DefaultInstance, "project", id).String()
}

// FolderRIDFromID builds the stable Compass RID for an OpenFoundry folder.
func FolderRIDFromID(id uuid.UUID) string {
	return rid.MustNewUUID("compass", rid.DefaultInstance, "folder", id).String()
}

// OntologyProject mirrors `models::project::OntologyProject` (Rust).
//
// SG.6 (2026-05-17) extended the shape with:
//   - DefaultRole: the role applied when a user discovers the project
//     without an explicit grant.
//   - PointOfContactUserID / PointOfContactEmail: where access
//     requests go.
//   - References: a JSONB array of {kind, id} pointers to sibling
//     projects / resources used by this project.
type OntologyProject struct {
	ID                                  uuid.UUID                  `json:"id"`
	RID                                 string                     `json:"rid"`
	Slug                                string                     `json:"slug"`
	DisplayName                         string                     `json:"display_name"`
	Description                         string                     `json:"description"`
	WorkspaceSlug                       *string                    `json:"workspace_slug"`
	OwnerID                             uuid.UUID                  `json:"owner_id"`
	DefaultRole                         OntologyProjectRole        `json:"default_role"`
	PointOfContactUserID                *uuid.UUID                 `json:"point_of_contact_user_id,omitempty"`
	PointOfContactEmail                 *string                    `json:"point_of_contact_email,omitempty"`
	References                          []OntologyProjectReference `json:"references"`
	MarkingRIDs                         []string                   `json:"marking_rids"`
	PropagateViewRequirementsEnabled    bool                       `json:"propagate_view_requirements_enabled"`
	PropagateViewRequirementsDisabledAt *time.Time                 `json:"propagate_view_requirements_disabled_at,omitempty"`
	CreatedAt                           time.Time                  `json:"created_at"`
	UpdatedAt                           time.Time                  `json:"updated_at"`
}

// OntologyProjectReference is the typed shape of one entry inside
// `OntologyProject.References`. SG.6: lets a project record that it
// depends on / publishes to another project or resource.
type OntologyProjectReference struct {
	Kind  string    `json:"kind"`
	ID    uuid.UUID `json:"id"`
	Label string    `json:"label,omitempty"`
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
	ProjectID                  uuid.UUID `json:"project_id"`
	ResourceKind               string    `json:"resource_kind"`
	ResourceID                 uuid.UUID `json:"resource_id"`
	BoundBy                    uuid.UUID `json:"bound_by"`
	ViewRequirementMarkingRIDs []string  `json:"view_requirement_marking_rids"`
	CreatedAt                  time.Time `json:"created_at"`
}

// OntologyProjectFolder mirrors `models::project::OntologyProjectFolder`.
type OntologyProjectFolder struct {
	ID                                  uuid.UUID  `json:"id"`
	RID                                 string     `json:"rid"`
	ProjectID                           uuid.UUID  `json:"project_id"`
	ProjectRID                          string     `json:"project_rid"`
	ParentFolderID                      *uuid.UUID `json:"parent_folder_id"`
	ParentFolderRID                     string     `json:"parent_folder_rid"`
	SpaceRID                            string     `json:"space_rid"`
	Type                                string     `json:"type"`
	TrashStatus                         string     `json:"trash_status"`
	InheritsProjectPolicies             bool       `json:"inherits_project_policies"`
	PolicyOverridesAllowed              bool       `json:"policy_overrides_allowed"`
	PropagateViewRequirementsEnabled    bool       `json:"propagate_view_requirements_enabled"`
	PropagateViewRequirementsDisabledAt *time.Time `json:"propagate_view_requirements_disabled_at,omitempty"`
	ViewRequirementMarkingRIDs          []string   `json:"view_requirement_marking_rids"`
	Name                                string     `json:"name"`
	Slug                                string     `json:"slug"`
	Description                         string     `json:"description"`
	CreatedBy                           uuid.UUID  `json:"created_by"`
	CreatedAt                           time.Time  `json:"created_at"`
	UpdatedAt                           time.Time  `json:"updated_at"`
}

// CreateOntologyProjectFolderRequest is the body of POST /projects/:id/folders.
type CreateOntologyProjectFolderRequest struct {
	Name                             string     `json:"name"`
	Description                      *string    `json:"description,omitempty"`
	ParentFolderID                   *uuid.UUID `json:"parent_folder_id,omitempty"`
	ParentFolderRID                  *string    `json:"parent_folder_rid,omitempty"`
	PropagateViewRequirementsEnabled *bool      `json:"propagate_view_requirements_enabled,omitempty"`
	ViewRequirementMarkingRIDs       []string   `json:"view_requirement_marking_rids,omitempty"`
}

// CreateOntologyProjectRequest is the body of POST /projects.
type CreateOntologyProjectRequest struct {
	Slug                             string                               `json:"slug"`
	DisplayName                      *string                              `json:"display_name,omitempty"`
	Description                      *string                              `json:"description,omitempty"`
	WorkspaceSlug                    *string                              `json:"workspace_slug,omitempty"`
	DefaultRole                      *OntologyProjectRole                 `json:"default_role,omitempty"`
	PointOfContactUserID             *uuid.UUID                           `json:"point_of_contact_user_id,omitempty"`
	PointOfContactEmail              *string                              `json:"point_of_contact_email,omitempty"`
	TemplateKey                      *string                              `json:"template_key,omitempty"`
	TemplateVariables                map[string]string                    `json:"template_variables,omitempty"`
	References                       []OntologyProjectReference           `json:"references,omitempty"`
	Folders                          []CreateOntologyProjectFolderRequest `json:"folders,omitempty"`
	FileAccessPresetID               *string                              `json:"file_access_preset_id,omitempty"`
	MarkingRIDs                      []string                             `json:"marking_rids,omitempty"`
	PropagateViewRequirementsEnabled *bool                                `json:"propagate_view_requirements_enabled,omitempty"`
}

// ─── SG.26: project templates ──────────────────────────────────────────

const (
	ProjectTemplatePrincipalUser           = "user"
	ProjectTemplatePrincipalGroup          = "group"
	ProjectTemplatePrincipalProjectCreator = "project_creator"
	ProjectTemplatePrincipalGeneratedGroup = "generated_group"
)

// ProjectTemplateVariable declares a parameter that is resolved when a
// project is created from a template.
type ProjectTemplateVariable struct {
	Key          string  `json:"key"`
	Label        string  `json:"label,omitempty"`
	Description  string  `json:"description,omitempty"`
	DefaultValue *string `json:"default_value,omitempty"`
	Required     bool    `json:"required"`
}

// ProjectTemplateFolderSpec defines one folder created during template
// deployment. ParentKey references another template folder in the same
// array, so templates can encode a repeatable hierarchy without binding
// to pre-existing folder UUIDs.
type ProjectTemplateFolderSpec struct {
	Key         string  `json:"key,omitempty"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	ParentKey   *string `json:"parent_key,omitempty"`
}

// ProjectTemplateGeneratedGroup represents a group that should be
// created/bound when the template is deployed. Identity ownership still
// lives in identity-federation-service; this service records the intended
// generated group metadata and binds the resulting group ID to the project.
type ProjectTemplateGeneratedGroup struct {
	Role                   OntologyProjectRole   `json:"role"`
	SlugSuffix             string                `json:"slug_suffix,omitempty"`
	DisplayNameTemplate    string                `json:"display_name_template,omitempty"`
	Description            string                `json:"description,omitempty"`
	ManagesGeneratedRoles  []OntologyProjectRole `json:"manages_generated_roles,omitempty"`
	Requestable            bool                  `json:"requestable"`
	ExternalRequestMessage *string               `json:"external_request_message,omitempty"`
	ExternalRequestURL     *string               `json:"external_request_url,omitempty"`
}

type ProjectTemplateRoleGrant struct {
	PrincipalKind      string               `json:"principal_kind"`
	PrincipalID        *uuid.UUID           `json:"principal_id,omitempty"`
	GeneratedGroupRole *OntologyProjectRole `json:"generated_group_role,omitempty"`
	Role               OntologyProjectRole  `json:"role"`
}

type ProjectTemplateMarking struct {
	MarkingID       *uuid.UUID  `json:"marking_id,omitempty"`
	MarkingRID      *string     `json:"marking_rid,omitempty"`
	DisplayName     string      `json:"display_name"`
	ReasonPrompt    *string     `json:"reason_prompt,omitempty"`
	ReviewerUserIDs []uuid.UUID `json:"reviewer_user_ids,omitempty"`
	CreateIfMissing bool        `json:"create_if_missing"`
	RequiredFor     []string    `json:"required_for,omitempty"`
}

type ProjectTemplateConstraint struct {
	ConstraintID      *uuid.UUID     `json:"constraint_id,omitempty"`
	ConstraintRID     *string        `json:"constraint_rid,omitempty"`
	Name              string         `json:"name"`
	Mode              string         `json:"mode,omitempty"`
	AllowedMarkingIDs []uuid.UUID    `json:"allowed_marking_ids,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
}

type ProjectTemplate struct {
	ID                   uuid.UUID                       `json:"id"`
	Key                  string                          `json:"key"`
	Name                 string                          `json:"name"`
	Description          string                          `json:"description"`
	SpaceSlug            *string                         `json:"space_slug,omitempty"`
	DefaultRole          OntologyProjectRole             `json:"default_role"`
	PointOfContactUserID *uuid.UUID                      `json:"point_of_contact_user_id,omitempty"`
	PointOfContactEmail  *string                         `json:"point_of_contact_email,omitempty"`
	Variables            []ProjectTemplateVariable       `json:"variables"`
	FolderStructure      []ProjectTemplateFolderSpec     `json:"folder_structure"`
	GeneratedGroups      []ProjectTemplateGeneratedGroup `json:"generated_groups"`
	DefaultRoleGrants    []ProjectTemplateRoleGrant      `json:"default_role_grants"`
	Markings             []ProjectTemplateMarking        `json:"markings"`
	Constraints          []ProjectTemplateConstraint     `json:"constraints"`
	GovernanceTags       []string                        `json:"governance_tags"`
	Active               bool                            `json:"active"`
	CreatedBy            *uuid.UUID                      `json:"created_by,omitempty"`
	CreatedAt            time.Time                       `json:"created_at"`
	UpdatedAt            time.Time                       `json:"updated_at"`
}

type CreateProjectTemplateRequest struct {
	Key                  string                          `json:"key"`
	Name                 string                          `json:"name"`
	Description          string                          `json:"description,omitempty"`
	SpaceSlug            *string                         `json:"space_slug,omitempty"`
	DefaultRole          *OntologyProjectRole            `json:"default_role,omitempty"`
	PointOfContactUserID *uuid.UUID                      `json:"point_of_contact_user_id,omitempty"`
	PointOfContactEmail  *string                         `json:"point_of_contact_email,omitempty"`
	Variables            []ProjectTemplateVariable       `json:"variables,omitempty"`
	FolderStructure      []ProjectTemplateFolderSpec     `json:"folder_structure,omitempty"`
	GeneratedGroups      []ProjectTemplateGeneratedGroup `json:"generated_groups,omitempty"`
	DefaultRoleGrants    []ProjectTemplateRoleGrant      `json:"default_role_grants,omitempty"`
	Markings             []ProjectTemplateMarking        `json:"markings,omitempty"`
	Constraints          []ProjectTemplateConstraint     `json:"constraints,omitempty"`
	GovernanceTags       []string                        `json:"governance_tags,omitempty"`
	Active               *bool                           `json:"active,omitempty"`
}

type ListProjectTemplatesResponse struct {
	Data []ProjectTemplate `json:"data"`
}

type ProjectTemplateValidationCheck struct {
	Key         string `json:"key"`
	Allowed     bool   `json:"allowed"`
	Description string `json:"description"`
}

type ProjectTemplateValidationResult struct {
	Allowed            bool                             `json:"allowed"`
	MissingPermissions []string                         `json:"missing_permissions"`
	Checks             []ProjectTemplateValidationCheck `json:"checks"`
}

type ProjectTemplateGeneratedGroupResult struct {
	GroupID               uuid.UUID             `json:"group_id"`
	Role                  OntologyProjectRole   `json:"role"`
	Slug                  string                `json:"slug"`
	DisplayName           string                `json:"display_name"`
	Description           string                `json:"description,omitempty"`
	ManagesGeneratedRoles []OntologyProjectRole `json:"manages_generated_roles,omitempty"`
	Requestable           bool                  `json:"requestable"`
	ExternalRequestURL    *string               `json:"external_request_url,omitempty"`
}

type ProjectTemplateAppliedMarking struct {
	MarkingID       uuid.UUID   `json:"marking_id"`
	MarkingRID      string      `json:"marking_rid"`
	DisplayName     string      `json:"display_name"`
	ReasonPrompt    *string     `json:"reason_prompt,omitempty"`
	ReviewerUserIDs []uuid.UUID `json:"reviewer_user_ids,omitempty"`
	Created         bool        `json:"created"`
}

type ProjectTemplateApplication struct {
	ID                 uuid.UUID                             `json:"id"`
	TemplateID         uuid.UUID                             `json:"template_id"`
	TemplateKey        string                                `json:"template_key"`
	ProjectID          uuid.UUID                             `json:"project_id"`
	AppliedBy          uuid.UUID                             `json:"applied_by"`
	Variables          map[string]string                     `json:"variables"`
	GeneratedGroups    []ProjectTemplateGeneratedGroupResult `json:"generated_groups"`
	AppliedMarkings    []ProjectTemplateAppliedMarking       `json:"applied_markings"`
	AppliedConstraints []ProjectTemplateConstraint           `json:"applied_constraints"`
	Validation         ProjectTemplateValidationResult       `json:"validation"`
	CreatedAt          time.Time                             `json:"created_at"`
}

type ListProjectTemplateApplicationsResponse struct {
	Data []ProjectTemplateApplication `json:"data"`
}

// UpdateOntologyProjectRequest is the body of PATCH /projects/:id.
//
// Rust's `workspace_slug: Option<Option<String>>` is collapsed to `*string`
// here, matching `UpdateOrganizationRequest`'s convention in models.go: the
// triple-state (absent / null / set) is interpreted at the handler layer.
type UpdateOntologyProjectRequest struct {
	DisplayName                      *string                     `json:"display_name,omitempty"`
	Description                      *string                     `json:"description,omitempty"`
	WorkspaceSlug                    *string                     `json:"workspace_slug,omitempty"`
	DefaultRole                      *OntologyProjectRole        `json:"default_role,omitempty"`
	PointOfContactUserID             **uuid.UUID                 `json:"point_of_contact_user_id,omitempty"`
	PointOfContactEmail              **string                    `json:"point_of_contact_email,omitempty"`
	References                       *[]OntologyProjectReference `json:"references,omitempty"`
	PropagateViewRequirementsEnabled *bool                       `json:"propagate_view_requirements_enabled,omitempty"`
}

// UpdateProjectFolderPropagationRequest is the body of PATCH
// /projects/:id/folders/:folder_id/propagate-view-requirements.
type UpdateProjectFolderPropagationRequest struct {
	Enabled                    *bool    `json:"enabled"`
	ViewRequirementMarkingRIDs []string `json:"view_requirement_marking_rids,omitempty"`
}

// ViewRequirementPropagationJob is the progress record for CMP.19
// background re-propagation after a project/folder propagation policy
// change.
type ViewRequirementPropagationJob struct {
	ID                  uuid.UUID  `json:"id"`
	ProjectID           uuid.UUID  `json:"project_id"`
	ParentResourceKind  string     `json:"parent_resource_kind"`
	ParentResourceID    uuid.UUID  `json:"parent_resource_id"`
	ParentResourceRID   string     `json:"parent_resource_rid"`
	InitiatedBy         uuid.UUID  `json:"initiated_by"`
	Status              string     `json:"status"`
	TargetMarkingRIDs   []string   `json:"target_marking_rids"`
	PreviousMarkingRIDs []string   `json:"previous_marking_rids"`
	TotalFolders        int        `json:"total_folders"`
	ProcessedFolders    int        `json:"processed_folders"`
	ChangedFolders      int        `json:"changed_folders"`
	TotalResources      int        `json:"total_resources"`
	ProcessedResources  int        `json:"processed_resources"`
	ChangedResources    int        `json:"changed_resources"`
	ErrorMessage        *string    `json:"error_message,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ListViewRequirementPropagationJobsResponse struct {
	Data []ViewRequirementPropagationJob `json:"data"`
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

// ─── SG.6: group memberships, access requests, group setup ──────────────

// OntologyProjectGroupMembership grants a project role to a group.
// SG.6: "Recommend group-based project roles".
type OntologyProjectGroupMembership struct {
	ProjectID uuid.UUID           `json:"project_id"`
	GroupID   uuid.UUID           `json:"group_id"`
	Role      OntologyProjectRole `json:"role"`
	GrantedBy *uuid.UUID          `json:"granted_by,omitempty"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// UpsertProjectGroupMembershipRequest is the body of PUT
// /projects/:id/group-memberships.
type UpsertProjectGroupMembershipRequest struct {
	GroupID uuid.UUID           `json:"group_id"`
	Role    OntologyProjectRole `json:"role"`
}

// ListOntologyProjectGroupMembershipsResponse is the body of GET
// /projects/:id/group-memberships.
type ListOntologyProjectGroupMembershipsResponse struct {
	Data []OntologyProjectGroupMembership `json:"data"`
}

// OntologyProjectAccessRequest mirrors one row in
// ontology_project_access_requests. SG.6: "Ensure file/folder
// requests inside a project resolve to project-level access
// requests".
type OntologyProjectAccessRequest struct {
	ID                  uuid.UUID                  `json:"id"`
	ProjectID           uuid.UUID                  `json:"project_id"`
	RequestedBy         uuid.UUID                  `json:"requested_by"`
	RequestType         string                     `json:"request_type"`
	RequestedForUserIDs []uuid.UUID                `json:"requested_for_user_ids"`
	RequestedRole       OntologyProjectRole        `json:"requested_role"`
	Reason              string                     `json:"reason"`
	ScopeResourceKind   *string                    `json:"scope_resource_kind,omitempty"`
	ScopeResourceID     *uuid.UUID                 `json:"scope_resource_id,omitempty"`
	Status              string                     `json:"status"`
	DecidedBy           *uuid.UUID                 `json:"decided_by,omitempty"`
	DecisionReason      *string                    `json:"decision_reason,omitempty"`
	CreatedAt           time.Time                  `json:"created_at"`
	DecidedAt           *time.Time                 `json:"decided_at,omitempty"`
	CompletedAt         *time.Time                 `json:"completed_at,omitempty"`
	Tasks               []ProjectAccessRequestTask `json:"tasks,omitempty"`
}

// Stable wire constants for access-request status. Renames are
// wire-breaking — the UI keys translations off these.
const (
	ProjectAccessRequestStatusPending          = "pending"
	ProjectAccessRequestStatusApproved         = "approved"
	ProjectAccessRequestStatusDenied           = "denied"
	ProjectAccessRequestStatusCancelled        = "cancelled"
	ProjectAccessRequestStatusChangesRequested = "changes_requested"
	ProjectAccessRequestStatusActionRequired   = "action_required"
	ProjectAccessRequestStatusCompleted        = "completed"
)

const (
	ProjectAccessRequestTypeProjectAccess           = "project_access"
	ProjectAccessRequestTypeAdditionalProjectAccess = "additional_project_access"
)

const (
	ProjectAccessRequestTaskGroupMembership      = "group_membership"
	ProjectAccessRequestTaskProjectRole          = "project_role"
	ProjectAccessRequestTaskMarkingAccess        = "marking_access"
	ProjectAccessRequestTaskExternalGroupHandoff = "external_group_handoff"
)

const (
	ProjectAccessRequestTaskStatusReview         = "review"
	ProjectAccessRequestTaskStatusApproved       = "approved"
	ProjectAccessRequestTaskStatusRejected       = "rejected"
	ProjectAccessRequestTaskStatusActionRequired = "action_required"
	ProjectAccessRequestTaskStatusCompleted      = "completed"
)

const (
	ProjectAccessGroupKindInternal  = "internal"
	ProjectAccessGroupKindExternal  = "external"
	ProjectAccessGroupKindRuleBased = "rule_based"
)

// CreateProjectAccessRequestRequest is the body of POST
// /projects/:id/access-requests. ScopeResourceKind +
// ScopeResourceID let a user explain "I'm asking because I can't
// open this folder/file/object" while the grant decision still
// happens at the project level.
type CreateProjectAccessRequestRequest struct {
	RequestType             *string                              `json:"request_type,omitempty"`
	RequestedForUserIDs     []uuid.UUID                          `json:"requested_for_user_ids,omitempty"`
	RequestedRole           OntologyProjectRole                  `json:"requested_role"`
	Reason                  *string                              `json:"reason,omitempty"`
	ScopeResourceKind       *string                              `json:"scope_resource_kind,omitempty"`
	ScopeResourceID         *uuid.UUID                           `json:"scope_resource_id,omitempty"`
	GroupMembershipRequests []ProjectGroupMembershipRequestInput `json:"group_membership_requests,omitempty"`
	ProjectRoleRequests     []ProjectRoleRequestInput            `json:"project_role_requests,omitempty"`
	MarkingAccessRequests   []ProjectMarkingAccessRequestInput   `json:"marking_access_requests,omitempty"`
}

type ProjectGroupMembershipRequestInput struct {
	GroupID      uuid.UUID            `json:"group_id"`
	TargetUserID *uuid.UUID           `json:"target_user_id,omitempty"`
	Role         *OntologyProjectRole `json:"role,omitempty"`
}

type ProjectRoleRequestInput struct {
	TargetUserID *uuid.UUID          `json:"target_user_id,omitempty"`
	Role         OntologyProjectRole `json:"role"`
}

type ProjectMarkingAccessRequestInput struct {
	MarkingID       uuid.UUID   `json:"marking_id"`
	MarkingName     *string     `json:"marking_name,omitempty"`
	TargetUserID    *uuid.UUID  `json:"target_user_id,omitempty"`
	Reason          *string     `json:"reason,omitempty"`
	ReviewerUserIDs []uuid.UUID `json:"reviewer_user_ids,omitempty"`
}

// DecideProjectAccessRequestRequest is the body of POST
// /projects/:id/access-requests/:request_id/decision.
type DecideProjectAccessRequestRequest struct {
	Decision string  `json:"decision"` // "approved" | "denied"
	Reason   *string `json:"reason,omitempty"`
}

// ListOntologyProjectAccessRequestsResponse is the body of GET
// /projects/:id/access-requests.
type ListOntologyProjectAccessRequestsResponse struct {
	Data []OntologyProjectAccessRequest `json:"data"`
}

// EnsureProjectAccessGroupsResponse is the body of POST
// /projects/:id/access-groups:bootstrap. SG.6 group-setup shortcut:
// creates / refreshes the viewer / editor / owner group memberships
// at the project level.
type EnsureProjectAccessGroupsResponse struct {
	Viewer *OntologyProjectGroupMembership `json:"viewer"`
	Editor *OntologyProjectGroupMembership `json:"editor"`
	Owner  *OntologyProjectGroupMembership `json:"owner"`
}

// EnsureProjectAccessGroupsRequest is the body of the same
// endpoint. Either supply explicit group IDs (preferred) or leave
// them unset for the handler to refuse with a 400 — auto-creation
// of groups lives in identity-federation-service.
type EnsureProjectAccessGroupsRequest struct {
	ViewerGroupID *uuid.UUID `json:"viewer_group_id,omitempty"`
	EditorGroupID *uuid.UUID `json:"editor_group_id,omitempty"`
	OwnerGroupID  *uuid.UUID `json:"owner_group_id,omitempty"`
}

type ProjectAccessRequestTask struct {
	ID                     uuid.UUID            `json:"id"`
	RequestID              uuid.UUID            `json:"request_id"`
	ProjectID              uuid.UUID            `json:"project_id"`
	TaskType               string               `json:"task_type"`
	TargetUserID           uuid.UUID            `json:"target_user_id"`
	RequestedRole          *OntologyProjectRole `json:"requested_role,omitempty"`
	GroupID                *uuid.UUID           `json:"group_id,omitempty"`
	MarkingID              *uuid.UUID           `json:"marking_id,omitempty"`
	MarkingName            *string              `json:"marking_name,omitempty"`
	Reason                 string               `json:"reason"`
	Status                 string               `json:"status"`
	ReviewerUserIDs        []uuid.UUID          `json:"reviewer_user_ids"`
	ExternalRequestMessage *string              `json:"external_request_message,omitempty"`
	ExternalRequestURL     *string              `json:"external_request_url,omitempty"`
	DecidedBy              *uuid.UUID           `json:"decided_by,omitempty"`
	DecisionReason         *string              `json:"decision_reason,omitempty"`
	CreatedAt              time.Time            `json:"created_at"`
	DecidedAt              *time.Time           `json:"decided_at,omitempty"`
	InvokedAt              *time.Time           `json:"invoked_at,omitempty"`
}

type ProjectAccessRequestGroupSetting struct {
	ProjectID                uuid.UUID            `json:"project_id"`
	GroupID                  uuid.UUID            `json:"group_id"`
	GroupDisplayName         *string              `json:"group_display_name,omitempty"`
	GroupKind                string               `json:"group_kind"`
	RequestRole              *OntologyProjectRole `json:"request_role,omitempty"`
	ReviewerUserIDs          []uuid.UUID          `json:"reviewer_user_ids"`
	CustomForm               map[string]any       `json:"custom_form"`
	ExternalRequestMessage   *string              `json:"external_request_message,omitempty"`
	ExternalRequestURL       *string              `json:"external_request_url,omitempty"`
	ExcludedFromRequestForms bool                 `json:"excluded_from_request_forms"`
	UpdatedBy                *uuid.UUID           `json:"updated_by,omitempty"`
	CreatedAt                time.Time            `json:"created_at"`
	UpdatedAt                time.Time            `json:"updated_at"`
}

type UpsertProjectAccessRequestGroupSettingRequest struct {
	GroupDisplayName         *string              `json:"group_display_name,omitempty"`
	GroupKind                *string              `json:"group_kind,omitempty"`
	RequestRole              *OntologyProjectRole `json:"request_role,omitempty"`
	ReviewerUserIDs          []uuid.UUID          `json:"reviewer_user_ids,omitempty"`
	CustomForm               map[string]any       `json:"custom_form,omitempty"`
	ExternalRequestMessage   *string              `json:"external_request_message,omitempty"`
	ExternalRequestURL       *string              `json:"external_request_url,omitempty"`
	ExcludedFromRequestForms *bool                `json:"excluded_from_request_forms,omitempty"`
}

type ProjectRequiredMarking struct {
	ProjectID       uuid.UUID   `json:"project_id"`
	MarkingID       uuid.UUID   `json:"marking_id"`
	MarkingName     string      `json:"marking_name"`
	ReasonPrompt    *string     `json:"reason_prompt,omitempty"`
	ReviewerUserIDs []uuid.UUID `json:"reviewer_user_ids"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

type UpsertProjectRequiredMarkingRequest struct {
	MarkingName     string      `json:"marking_name"`
	ReasonPrompt    *string     `json:"reason_prompt,omitempty"`
	ReviewerUserIDs []uuid.UUID `json:"reviewer_user_ids,omitempty"`
}

type ProjectAccessRequestFormGroup struct {
	GroupID                uuid.UUID           `json:"group_id"`
	Role                   OntologyProjectRole `json:"role"`
	GroupDisplayName       *string             `json:"group_display_name,omitempty"`
	GroupKind              string              `json:"group_kind"`
	ReviewerUserIDs        []uuid.UUID         `json:"reviewer_user_ids"`
	CustomForm             map[string]any      `json:"custom_form"`
	ExternalRequestMessage *string             `json:"external_request_message,omitempty"`
	ExternalRequestURL     *string             `json:"external_request_url,omitempty"`
}

type ProjectAccessRequestFormResponse struct {
	ProjectID           uuid.UUID                       `json:"project_id"`
	RequesterID         uuid.UUID                       `json:"requester_id"`
	ProjectOwnerID      uuid.UUID                       `json:"project_owner_id"`
	DefaultRole         OntologyProjectRole             `json:"default_role"`
	Groups              []ProjectAccessRequestFormGroup `json:"groups"`
	RequiredMarkings    []ProjectRequiredMarking        `json:"required_markings"`
	DirectRoleReviewers []uuid.UUID                     `json:"direct_role_reviewers"`
}

// ─── SG.8: role inheritance & direct grants ─────────────────────────

// Stable wire constants for grant scope / principal vocabulary.
const (
	ProjectGrantScopeProject   = "project"
	ProjectGrantScopeFolder    = "folder"
	ProjectGrantPrincipalUser  = "user"
	ProjectGrantPrincipalGroup = "group"
)

// ProjectResourceGrant mirrors one row in
// ontology_project_resource_grants. SG.8: direct grants at the
// project or folder scope; sub-folder/sub-resource inherits.
type ProjectResourceGrant struct {
	ID            uuid.UUID           `json:"id"`
	ProjectID     uuid.UUID           `json:"project_id"`
	ScopeKind     string              `json:"scope_kind"`
	ScopeID       *uuid.UUID          `json:"scope_id,omitempty"`
	PrincipalKind string              `json:"principal_kind"`
	PrincipalID   uuid.UUID           `json:"principal_id"`
	Role          OntologyProjectRole `json:"role"`
	GrantedBy     *uuid.UUID          `json:"granted_by,omitempty"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
}

// CreateProjectResourceGrantRequest is POST /projects/{id}/resource-grants.
type CreateProjectResourceGrantRequest struct {
	ScopeKind     string              `json:"scope_kind"`
	ScopeID       *uuid.UUID          `json:"scope_id,omitempty"`
	PrincipalKind string              `json:"principal_kind"`
	PrincipalID   uuid.UUID           `json:"principal_id"`
	Role          OntologyProjectRole `json:"role"`
}

// ListProjectResourceGrantsResponse is the body of GET
// /projects/{id}/resource-grants.
type ListProjectResourceGrantsResponse struct {
	Data []ProjectResourceGrant `json:"data"`
}

// EffectiveAccessSourceKind names the categories of grant the
// resolver can attribute a role to. Stable wire vocabulary — the
// admin UI keys translations off these strings; renames are
// wire-breaking.
const (
	EffectiveAccessSourceProjectOwner           = "project_owner"
	EffectiveAccessSourceProjectDefault         = "project_default_role"
	EffectiveAccessSourceProjectUserMembership  = "project_user_membership"
	EffectiveAccessSourceProjectGroupMembership = "project_group_membership"
	EffectiveAccessSourceDirectUserGrant        = "direct_user_grant"
	EffectiveAccessSourceDirectGroupGrant       = "direct_group_grant"
	EffectiveAccessSourceFolderUserGrant        = "folder_user_grant"
	EffectiveAccessSourceFolderGroupGrant       = "folder_group_grant"
	EffectiveAccessSourceAdminRole              = "admin_role"
	EffectiveAccessSourceWorkspaceMatch         = "workspace_match"
)

// EffectiveAccessSource is one row in the breakdown returned by the
// effective-access resolver. SG.8: lets an admin see *why* a user
// resolves to a given role without leaking unrelated resource data.
type EffectiveAccessSource struct {
	Kind        string              `json:"kind"`
	Role        OntologyProjectRole `json:"role"`
	GrantID     *uuid.UUID          `json:"grant_id,omitempty"`
	PrincipalID *uuid.UUID          `json:"principal_id,omitempty"`
	GroupID     *uuid.UUID          `json:"group_id,omitempty"`
	FolderID    *uuid.UUID          `json:"folder_id,omitempty"`
	Detail      string              `json:"detail,omitempty"`
}

// EffectiveAccessResponse is the answer of GET
// /projects/{id}/effective-access. Sources are ordered highest rank
// first so the first row is the role that wins.
type EffectiveAccessResponse struct {
	UserID       uuid.UUID               `json:"user_id"`
	ProjectID    uuid.UUID               `json:"project_id"`
	ScopeKind    string                  `json:"scope_kind"`
	ScopeID      *uuid.UUID              `json:"scope_id,omitempty"`
	ResolvedRole *OntologyProjectRole    `json:"resolved_role,omitempty"`
	Sources      []EffectiveAccessSource `json:"sources"`
	CheckedAt    time.Time               `json:"checked_at"`
}
