package models

import (
	"time"

	"github.com/google/uuid"
)

type Permission struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"`
	Resource    string     `json:"resource"`
	Action      string     `json:"action"`
	Description *string    `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Role struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
}

type Group struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"`
	Name        string     `json:"name"`
	Description *string    `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
}

type RoleResponse struct {
	ID            uuid.UUID   `json:"id"`
	TenantID      *uuid.UUID  `json:"tenant_id,omitempty"`
	Name          string      `json:"name"`
	Description   *string     `json:"description"`
	CreatedAt     time.Time   `json:"created_at"`
	PermissionIDs []uuid.UUID `json:"permission_ids"`
	Permissions   []string    `json:"permissions"`
}

type GroupResponse struct {
	ID          uuid.UUID   `json:"id"`
	TenantID    *uuid.UUID  `json:"tenant_id,omitempty"`
	Name        string      `json:"name"`
	Description *string     `json:"description"`
	CreatedAt   time.Time   `json:"created_at"`
	MemberCount int64       `json:"member_count"`
	RoleIDs     []uuid.UUID `json:"role_ids"`
	Roles       []string    `json:"roles"`
}

type CreatePermissionRequest struct {
	Resource    string  `json:"resource"`
	Action      string  `json:"action"`
	Description *string `json:"description,omitempty"`
}

type CreateRoleRequest struct {
	Name          string      `json:"name"`
	Description   *string     `json:"description,omitempty"`
	PermissionIDs []uuid.UUID `json:"permission_ids,omitempty"`
}

type UpdateRoleRequest struct {
	Description   *string     `json:"description,omitempty"`
	PermissionIDs []uuid.UUID `json:"permission_ids,omitempty"`
}

type CreateGroupRequest struct {
	Name        string      `json:"name"`
	Description *string     `json:"description,omitempty"`
	RoleIDs     []uuid.UUID `json:"role_ids,omitempty"`
}

type UpdateGroupRequest struct {
	Description *string     `json:"description,omitempty"`
	RoleIDs     []uuid.UUID `json:"role_ids,omitempty"`
}

type AssignRoleRequest struct {
	RoleID uuid.UUID `json:"role_id"`
}

type UserGroupRequest struct {
	GroupID uuid.UUID `json:"group_id"`
}

// ─── SG.7: role sets, operations, delegation-rank model ─────────────

// Role-set context constants. Stable wire vocabulary — the UI keys
// translations off these strings; renames are wire-breaking.
const (
	RoleSetContextProject         = "project"
	RoleSetContextOntology        = "ontology"
	RoleSetContextRestrictedView  = "restricted_view"
	RoleSetContextPlatformAdmin   = "platform_admin"
)

// RoleSet is a named bundle of roles tied to a resource context.
// SG.7: "Support context-specific role sets for projects, ontology,
// restricted views, and platform administration."
type RoleSet struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    *uuid.UUID `json:"tenant_id,omitempty"`
	Slug        string     `json:"slug"`
	Name        string     `json:"name"`
	Context     string     `json:"context"`
	Description *string    `json:"description,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RoleSetRole is one row in role_set_roles — a role's membership in
// a role set, with the integer rank that governs delegation.
type RoleSetRole struct {
	RoleSetID uuid.UUID `json:"role_set_id"`
	RoleID    uuid.UUID `json:"role_id"`
	RoleName  string    `json:"role_name"`
	Rank      int       `json:"rank"`
	CreatedAt time.Time `json:"created_at"`
}

// RoleSetResponse joins RoleSet with its members (ordered by rank).
// SG.7 admin UI consumes this shape.
type RoleSetResponse struct {
	RoleSet
	Roles []RoleSetRole `json:"roles"`
}

// CreateRoleSetRequest is POST /role-sets.
type CreateRoleSetRequest struct {
	Slug        string  `json:"slug"`
	Name        string  `json:"name"`
	Context     string  `json:"context"`
	Description *string `json:"description,omitempty"`
}

// UpdateRoleSetRequest is PATCH /role-sets/{id}.
type UpdateRoleSetRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// AddRoleToRoleSetRequest is POST /role-sets/{id}/roles.
type AddRoleToRoleSetRequest struct {
	RoleID uuid.UUID `json:"role_id"`
	Rank   int       `json:"rank"`
}

// OperationCatalogEntry is one row from the seeded operation
// catalog. SG.7 GET /operations exposes the full catalog so admin
// UIs can render the role→operation matrix.
type OperationCatalogEntry struct {
	ID          uuid.UUID `json:"id"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description *string   `json:"description,omitempty"`
}

// ListOperationsResponse is GET /operations.
type ListOperationsResponse struct {
	Items []OperationCatalogEntry `json:"items"`
}

// RoleSetGrant describes a (principal, role_set, role) grant —
// returned by the delegation-rank check endpoint. SG.7: "ensure
// role delegation cannot exceed the grantor's own role level".
type RoleSetGrant struct {
	PrincipalID uuid.UUID `json:"principal_id"`
	RoleSetID   uuid.UUID `json:"role_set_id"`
	RoleID      uuid.UUID `json:"role_id"`
	Rank        int       `json:"rank"`
}

// CheckDelegationRequest is POST /role-sets/{id}/delegation:check.
// `grantor_id` defaults to the authenticated subject; `target_role_id`
// is the role the grantor proposes to grant.
type CheckDelegationRequest struct {
	GrantorID    *uuid.UUID `json:"grantor_id,omitempty"`
	TargetRoleID uuid.UUID  `json:"target_role_id"`
}

// CheckDelegationResponse is the answer. Allowed is true when the
// grantor's own rank in the role set is ≥ the target role's rank.
// Reason carries a human-readable string when Allowed is false.
type CheckDelegationResponse struct {
	Allowed         bool       `json:"allowed"`
	GrantorRoleID   *uuid.UUID `json:"grantor_role_id,omitempty"`
	GrantorRank     *int       `json:"grantor_rank,omitempty"`
	TargetRoleID    uuid.UUID  `json:"target_role_id"`
	TargetRank      int        `json:"target_rank"`
	Reason          string     `json:"reason,omitempty"`
}
