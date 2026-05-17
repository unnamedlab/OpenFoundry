package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	MarkingCategoryVisibilityVisible = "visible"
	MarkingCategoryVisibilityHidden  = "hidden"

	MarkingCategoryPrincipalUser  = "user"
	MarkingCategoryPrincipalGroup = "group"

	MarkingCategoryPermissionAdministrator = "administrator"
	MarkingCategoryPermissionViewer        = "viewer"

	MarkingCategoryAuditCreated           = "category.created"
	MarkingCategoryAuditUpdated           = "category.updated"
	MarkingCategoryAuditPermissionGranted = "category.permission_granted"
	MarkingCategoryAuditPermissionRevoked = "category.permission_revoked"
	MarkingCategoryAuditDeleteBlocked     = "category.delete_blocked"

	MarkingPermissionAdministrator = "administrator"
	MarkingPermissionRemover       = "remover"
	MarkingPermissionApplier       = "applier"
	MarkingPermissionMember        = "member"

	MarkingAuditCreated             = "marking.created"
	MarkingAuditUpdated             = "marking.updated"
	MarkingAuditPermissionGranted   = "marking.permission_granted"
	MarkingAuditPermissionRevoked   = "marking.permission_revoked"
	MarkingAuditDeleteBlocked       = "marking.delete_blocked"
	MarkingAuditCategoryMoveBlocked = "marking.category_move_blocked"

	ResourceMarkingAuditApplied      = "resource_marking.applied"
	ResourceMarkingAuditApplyDenied  = "resource_marking.apply_denied"
	ResourceMarkingAuditRemoved      = "resource_marking.removed"
	ResourceMarkingAuditRemoveDenied = "resource_marking.remove_denied"

	ResourceMarkingRelationHierarchy = "hierarchy"
	ResourceMarkingRelationLineage   = "lineage"

	EffectiveResourceMarkingSourceDirect    = "direct"
	EffectiveResourceMarkingSourceHierarchy = "hierarchy"
	EffectiveResourceMarkingSourceLineage   = "lineage"
	EffectiveResourceMarkingSourceMixed     = "mixed"

	ResourceMarkingRequiredForResourceAccess = "resource_access"
	ResourceMarkingRequiredForDataAccess     = "data_access"

	ResourceAccessRequirementStatusPassed        = "passed"
	ResourceAccessRequirementStatusFailed        = "failed"
	ResourceAccessRequirementStatusNotApplicable = "not_applicable"

	ResourceAccessRequirementOrganization    = "organization"
	ResourceAccessRequirementRole            = "role"
	ResourceAccessRequirementScopedSession   = "scoped_session"
	ResourceAccessRequirementResourceMarking = "resource_marking"
	ResourceAccessRequirementDataMarking     = "data_marking"

	MarkingBuildStatusApplied = "applied"
	MarkingBuildStatusBlocked = "blocked"
	MarkingBuildStatusDryRun  = "dry_run"
)

// MarkingCategory is the SG.11 administrative container. Actual marking
// rows and membership enforcement arrive in SG.12-SG.15.
type MarkingCategory struct {
	ID             uuid.UUID       `json:"id"`
	TenantID       *uuid.UUID      `json:"tenant_id,omitempty"`
	Slug           string          `json:"slug"`
	DisplayName    string          `json:"display_name"`
	Description    string          `json:"description"`
	Visibility     string          `json:"visibility"`
	OrganizationID *uuid.UUID      `json:"organization_id,omitempty"`
	Metadata       json.RawMessage `json:"metadata"`
	CreatedBy      uuid.UUID       `json:"created_by"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

type MarkingCategoryPermission struct {
	CategoryID    uuid.UUID `json:"category_id"`
	PrincipalKind string    `json:"principal_kind"`
	PrincipalID   uuid.UUID `json:"principal_id"`
	Permission    string    `json:"permission"`
	GrantedBy     uuid.UUID `json:"granted_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type MarkingCategoryResponse struct {
	MarkingCategory
	Permissions []MarkingCategoryPermission `json:"permissions"`
}

type MarkingCategoryPrincipal struct {
	PrincipalKind string    `json:"principal_kind"`
	PrincipalID   uuid.UUID `json:"principal_id"`
}

type CreateMarkingCategoryRequest struct {
	Slug           string                     `json:"slug"`
	DisplayName    string                     `json:"display_name"`
	Description    string                     `json:"description,omitempty"`
	Visibility     string                     `json:"visibility,omitempty"`
	OrganizationID *uuid.UUID                 `json:"organization_id,omitempty"`
	Metadata       json.RawMessage            `json:"metadata,omitempty"`
	Administrators []MarkingCategoryPrincipal `json:"administrators,omitempty"`
	Viewers        []MarkingCategoryPrincipal `json:"viewers,omitempty"`
}

type UpdateMarkingCategoryRequest struct {
	DisplayName    *string         `json:"display_name,omitempty"`
	Description    *string         `json:"description,omitempty"`
	Visibility     *string         `json:"visibility,omitempty"`
	OrganizationID *uuid.UUID      `json:"organization_id,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
}

type UpsertMarkingCategoryPermissionRequest struct {
	PrincipalKind string    `json:"principal_kind"`
	PrincipalID   uuid.UUID `json:"principal_id"`
	Permission    string    `json:"permission"`
}

type MarkingCategoryAuditEvent struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      *uuid.UUID      `json:"tenant_id,omitempty"`
	CategoryID    *uuid.UUID      `json:"category_id,omitempty"`
	ActorID       uuid.UUID       `json:"actor_id"`
	Action        string          `json:"action"`
	PrincipalKind *string         `json:"principal_kind,omitempty"`
	PrincipalID   *uuid.UUID      `json:"principal_id,omitempty"`
	Permission    *string         `json:"permission,omitempty"`
	BeforeState   json.RawMessage `json:"before_state"`
	AfterState    json.RawMessage `json:"after_state"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

// Marking is the stable, category-scoped mandatory access-control
// primitive. CategoryID is immutable after creation.
type Marking struct {
	ID          uuid.UUID       `json:"id"`
	TenantID    *uuid.UUID      `json:"tenant_id,omitempty"`
	CategoryID  uuid.UUID       `json:"category_id"`
	Slug        string          `json:"slug"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
	CreatedBy   uuid.UUID       `json:"created_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type MarkingPermission struct {
	MarkingID     uuid.UUID `json:"marking_id"`
	PrincipalKind string    `json:"principal_kind"`
	PrincipalID   uuid.UUID `json:"principal_id"`
	Permission    string    `json:"permission"`
	GrantedBy     uuid.UUID `json:"granted_by"`
	CreatedAt     time.Time `json:"created_at"`
}

type MarkingResponse struct {
	Marking
	Permissions      []MarkingPermission `json:"permissions"`
	MetadataRedacted bool                `json:"metadata_redacted,omitempty"`
}

type MarkingPrincipal struct {
	PrincipalKind string    `json:"principal_kind"`
	PrincipalID   uuid.UUID `json:"principal_id"`
}

type CreateMarkingRequest struct {
	ID             *uuid.UUID         `json:"id,omitempty"`
	Slug           string             `json:"slug"`
	DisplayName    string             `json:"display_name"`
	Description    string             `json:"description,omitempty"`
	Metadata       json.RawMessage    `json:"metadata,omitempty"`
	Administrators []MarkingPrincipal `json:"administrators,omitempty"`
	Removers       []MarkingPrincipal `json:"removers,omitempty"`
	Appliers       []MarkingPrincipal `json:"appliers,omitempty"`
	Members        []MarkingPrincipal `json:"members,omitempty"`
}

type UpdateMarkingRequest struct {
	DisplayName *string         `json:"display_name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

type UpsertMarkingPermissionRequest struct {
	PrincipalKind string    `json:"principal_kind"`
	PrincipalID   uuid.UUID `json:"principal_id"`
	Permission    string    `json:"permission"`
}

type MoveMarkingCategoryRequest struct {
	TargetCategoryID uuid.UUID `json:"target_category_id"`
}

type MarkingAuditEvent struct {
	ID            uuid.UUID       `json:"id"`
	TenantID      *uuid.UUID      `json:"tenant_id,omitempty"`
	CategoryID    *uuid.UUID      `json:"category_id,omitempty"`
	MarkingID     *uuid.UUID      `json:"marking_id,omitempty"`
	ActorID       uuid.UUID       `json:"actor_id"`
	Action        string          `json:"action"`
	PrincipalKind *string         `json:"principal_kind,omitempty"`
	PrincipalID   *uuid.UUID      `json:"principal_id,omitempty"`
	Permission    *string         `json:"permission,omitempty"`
	BeforeState   json.RawMessage `json:"before_state"`
	AfterState    json.RawMessage `json:"after_state"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     time.Time       `json:"created_at"`
}

type MarkingPermissionCheckRequest struct {
	PrincipalID                   *uuid.UUID  `json:"principal_id,omitempty"`
	GroupIDs                      []uuid.UUID `json:"group_ids,omitempty"`
	ResourceUpdateMarkingsAllowed bool        `json:"resource_update_markings_allowed,omitempty"`
	ExpandAccessAllowed           bool        `json:"expand_access_allowed,omitempty"`
}

type MarkingPermissionCheckResponse struct {
	MarkingID                     uuid.UUID `json:"marking_id"`
	PrincipalID                   uuid.UUID `json:"principal_id"`
	CanManage                     bool      `json:"can_manage"`
	CanApply                      bool      `json:"can_apply"`
	CanRemove                     bool      `json:"can_remove"`
	IsMember                      bool      `json:"is_member"`
	CanAccessMarkedData           bool      `json:"can_access_marked_data"`
	ResourceUpdateMarkingsAllowed bool      `json:"resource_update_markings_allowed"`
	ExpandAccessAllowed           bool      `json:"expand_access_allowed"`
	CanApplyToResource            bool      `json:"can_apply_to_resource"`
	CanRemoveFromResource         bool      `json:"can_remove_from_resource"`
	Reasons                       []string  `json:"reasons"`
}

type ResourceMarking struct {
	ID           uuid.UUID       `json:"id"`
	TenantID     *uuid.UUID      `json:"tenant_id,omitempty"`
	ResourceKind string          `json:"resource_kind"`
	ResourceID   string          `json:"resource_id"`
	MarkingID    uuid.UUID       `json:"marking_id"`
	SourceKind   string          `json:"source_kind"`
	Metadata     json.RawMessage `json:"metadata"`
	AppliedBy    uuid.UUID       `json:"applied_by"`
	AppliedAt    time.Time       `json:"applied_at"`
}

type ApplyResourceMarkingRequest struct {
	ResourceKind                  string          `json:"resource_kind"`
	ResourceID                    string          `json:"resource_id"`
	MarkingID                     uuid.UUID       `json:"marking_id"`
	ResourceUpdateMarkingsAllowed bool            `json:"resource_update_markings_allowed"`
	Metadata                      json.RawMessage `json:"metadata,omitempty"`
}

type RemoveResourceMarkingRequest struct {
	ResourceKind                  string    `json:"resource_kind"`
	ResourceID                    string    `json:"resource_id"`
	MarkingID                     uuid.UUID `json:"marking_id"`
	ResourceUpdateMarkingsAllowed bool      `json:"resource_update_markings_allowed"`
	ExpandAccessAllowed           bool      `json:"expand_access_allowed,omitempty"`
	Reason                        string    `json:"reason,omitempty"`
}

type ResourceMarkingMutationResponse struct {
	Allowed         bool                           `json:"allowed"`
	ResourceMarking *ResourceMarking               `json:"resource_marking,omitempty"`
	PermissionCheck MarkingPermissionCheckResponse `json:"permission_check"`
}

type ResourceMarkingEdge struct {
	ID                 uuid.UUID       `json:"id"`
	TenantID           *uuid.UUID      `json:"tenant_id,omitempty"`
	SourceResourceKind string          `json:"source_resource_kind"`
	SourceResourceID   string          `json:"source_resource_id"`
	TargetResourceKind string          `json:"target_resource_kind"`
	TargetResourceID   string          `json:"target_resource_id"`
	RelationKind       string          `json:"relation_kind"`
	Metadata           json.RawMessage `json:"metadata"`
	CreatedBy          uuid.UUID       `json:"created_by"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

type UpsertResourceMarkingEdgeRequest struct {
	SourceResourceKind string          `json:"source_resource_kind"`
	SourceResourceID   string          `json:"source_resource_id"`
	TargetResourceKind string          `json:"target_resource_kind"`
	TargetResourceID   string          `json:"target_resource_id"`
	RelationKind       string          `json:"relation_kind"`
	Metadata           json.RawMessage `json:"metadata,omitempty"`
}

type DeleteResourceMarkingEdgeRequest struct {
	SourceResourceKind string `json:"source_resource_kind"`
	SourceResourceID   string `json:"source_resource_id"`
	TargetResourceKind string `json:"target_resource_kind"`
	TargetResourceID   string `json:"target_resource_id"`
	RelationKind       string `json:"relation_kind"`
}

type ResourceMarkingPathHop struct {
	ResourceKind string `json:"resource_kind"`
	ResourceID   string `json:"resource_id"`
	RelationKind string `json:"relation_kind,omitempty"`
}

type EffectiveResourceMarkingSource struct {
	SourceKind              string                   `json:"source_kind"`
	RequiredFor             string                   `json:"required_for"`
	SourceResourceKind      string                   `json:"source_resource_kind"`
	SourceResourceID        string                   `json:"source_resource_id"`
	DirectResourceMarkingID uuid.UUID                `json:"direct_resource_marking_id"`
	RelationKinds           []string                 `json:"relation_kinds,omitempty"`
	Path                    []ResourceMarkingPathHop `json:"path"`
	Metadata                json.RawMessage          `json:"metadata"`
}

type EffectiveResourceMarking struct {
	MarkingID   uuid.UUID                        `json:"marking_id"`
	MarkingName string                           `json:"marking_name"`
	RequiredFor []string                         `json:"required_for"`
	Sources     []EffectiveResourceMarkingSource `json:"sources"`
}

type EffectiveResourceMarkingsResponse struct {
	ResourceKind string                     `json:"resource_kind"`
	ResourceID   string                     `json:"resource_id"`
	Items        []EffectiveResourceMarking `json:"items"`
	CheckedAt    time.Time                  `json:"checked_at"`
}

type ResourceAccessCheckRequest struct {
	PrincipalID            *uuid.UUID  `json:"principal_id,omitempty"`
	GroupIDs               []uuid.UUID `json:"group_ids,omitempty"`
	ResourceKind           string      `json:"resource_kind"`
	ResourceID             string      `json:"resource_id"`
	RequiredOrganizationID *uuid.UUID  `json:"required_organization_id,omitempty"`
	UserOrganizationIDs    []uuid.UUID `json:"user_organization_ids,omitempty"`
	RoleSatisfied          *bool       `json:"role_satisfied,omitempty"`
	RoleLabel              string      `json:"role_label,omitempty"`
	RoleDetail             string      `json:"role_detail,omitempty"`
	MaxDepth               *int        `json:"max_depth,omitempty"`
}

type ResourceAccessRequirementResult struct {
	Kind      string   `json:"kind"`
	Label     string   `json:"label"`
	Status    string   `json:"status"`
	Satisfied bool     `json:"satisfied"`
	Required  []string `json:"required,omitempty"`
	Present   []string `json:"present,omitempty"`
	Missing   []string `json:"missing,omitempty"`
	Detail    string   `json:"detail"`
	Sources   []string `json:"sources,omitempty"`
}

type ResourceAccessMarkingResult struct {
	MarkingID                uuid.UUID                        `json:"marking_id"`
	MarkingName              string                           `json:"marking_name"`
	RequiredFor              []string                         `json:"required_for"`
	Satisfied                bool                             `json:"satisfied"`
	MembershipSatisfied      bool                             `json:"membership_satisfied"`
	ScopedSessionSatisfied   bool                             `json:"scoped_session_satisfied"`
	ScopedSessionRequirement bool                             `json:"scoped_session_requirement"`
	MissingFor               []string                         `json:"missing_for,omitempty"`
	Sources                  []EffectiveResourceMarkingSource `json:"sources"`
}

type ResourceAccessCheckResponse struct {
	PrincipalID                uuid.UUID                         `json:"principal_id"`
	ResourceKind               string                            `json:"resource_kind"`
	ResourceID                 string                            `json:"resource_id"`
	ResourceAccessAllowed      bool                              `json:"resource_access_allowed"`
	DataAccessAllowed          bool                              `json:"data_access_allowed"`
	AccessRequirements         []ResourceAccessRequirementResult `json:"access_requirements"`
	AdditionalDataRequirements []ResourceAccessRequirementResult `json:"additional_data_requirements"`
	EffectiveMarkings          []EffectiveResourceMarking        `json:"effective_markings"`
	MarkingResults             []ResourceAccessMarkingResult     `json:"marking_results"`
	CheckedAt                  time.Time                         `json:"checked_at"`
}

type MarkingBuildResourceRef struct {
	ResourceKind string `json:"resource_kind"`
	ResourceID   string `json:"resource_id"`
}

type MarkingDiffEntry struct {
	MarkingID   uuid.UUID `json:"marking_id"`
	MarkingName string    `json:"marking_name"`
	RequiredFor []string  `json:"required_for"`
}

type MarkingBuildBlockedRemoval struct {
	OutputResource MarkingBuildResourceRef        `json:"output_resource"`
	MarkingID      uuid.UUID                      `json:"marking_id"`
	MarkingName    string                         `json:"marking_name"`
	RequiredFor    []string                       `json:"required_for"`
	Permission     MarkingPermissionCheckResponse `json:"permission"`
}

type MarkingBuildOutputDiff struct {
	OutputResource MarkingBuildResourceRef    `json:"output_resource"`
	Before         []EffectiveResourceMarking `json:"before"`
	After          []EffectiveResourceMarking `json:"after"`
	Added          []MarkingDiffEntry         `json:"added"`
	Removed        []MarkingDiffEntry         `json:"removed"`
	Unchanged      []MarkingDiffEntry         `json:"unchanged"`
}

type PublishMarkingBuildRequest struct {
	BuildID                        string                    `json:"build_id,omitempty"`
	TransactionID                  string                    `json:"transaction_id,omitempty"`
	InputResources                 []MarkingBuildResourceRef `json:"input_resources"`
	OutputResources                []MarkingBuildResourceRef `json:"output_resources"`
	ReplaceExistingLineageToOutput bool                      `json:"replace_existing_lineage_to_output"`
	DryRun                         bool                      `json:"dry_run,omitempty"`
	GroupIDs                       []uuid.UUID               `json:"group_ids,omitempty"`
	ResourceUpdateMarkingsAllowed  bool                      `json:"resource_update_markings_allowed"`
	ExpandAccessAllowed            bool                      `json:"expand_access_allowed,omitempty"`
	Reason                         string                    `json:"reason,omitempty"`
	Metadata                       json.RawMessage           `json:"metadata,omitempty"`
}

type PublishMarkingBuildResponse struct {
	Allowed         bool                         `json:"allowed"`
	Applied         bool                         `json:"applied"`
	DryRun          bool                         `json:"dry_run"`
	BuildID         string                       `json:"build_id,omitempty"`
	TransactionID   string                       `json:"transaction_id,omitempty"`
	OutputDiffs     []MarkingBuildOutputDiff     `json:"output_diffs"`
	BlockedRemovals []MarkingBuildBlockedRemoval `json:"blocked_removals,omitempty"`
	CheckedAt       time.Time                    `json:"checked_at"`
}

type MarkingBuildEvent struct {
	ID                 uuid.UUID       `json:"id"`
	TenantID           *uuid.UUID      `json:"tenant_id,omitempty"`
	BuildID            string          `json:"build_id,omitempty"`
	TransactionID      string          `json:"transaction_id,omitempty"`
	OutputResourceKind string          `json:"output_resource_kind"`
	OutputResourceID   string          `json:"output_resource_id"`
	ActorID            uuid.UUID       `json:"actor_id"`
	Status             string          `json:"status"`
	Reason             string          `json:"reason,omitempty"`
	InputResources     json.RawMessage `json:"input_resources"`
	BeforeState        json.RawMessage `json:"before_state"`
	AfterState         json.RawMessage `json:"after_state"`
	Diff               json.RawMessage `json:"diff"`
	Metadata           json.RawMessage `json:"metadata"`
	CreatedAt          time.Time       `json:"created_at"`
}
