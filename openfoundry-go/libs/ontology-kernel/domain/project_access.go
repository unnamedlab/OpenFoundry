// Project-scoped access controls used by every kernel handler that
// needs to enforce project membership.
//
// Mirrors `libs/ontology-kernel/src/domain/project_access.rs`.

package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// OntologyResourceKind mirrors `enum OntologyResourceKind`.
type OntologyResourceKind string

const (
	OntologyResourceKindObjectType         OntologyResourceKind = "object_type"
	OntologyResourceKindLinkType           OntologyResourceKind = "link_type"
	OntologyResourceKindInterface          OntologyResourceKind = "interface"
	OntologyResourceKindSharedPropertyType OntologyResourceKind = "shared_property_type"
	OntologyResourceKindActionType         OntologyResourceKind = "action_type"
	OntologyResourceKindFunctionPackage    OntologyResourceKind = "function_package"
	OntologyResourceKindRule               OntologyResourceKind = "rule"
	OntologyResourceKindObjectSet          OntologyResourceKind = "object_set"
)

// AsStr mirrors `impl OntologyResourceKind::as_str(self)`.
func (k OntologyResourceKind) AsStr() string { return string(k) }

// TableName mirrors the private `fn table_name`. Used by
// [LoadResourceOwnerID] to interpolate the right table into the SQL.
// Returns an empty string for unknown kinds — the caller should
// have validated via [ParseOntologyResourceKind] before reaching
// here.
func (k OntologyResourceKind) TableName() string {
	switch k {
	case OntologyResourceKindObjectType:
		return "object_types"
	case OntologyResourceKindLinkType:
		return "link_types"
	case OntologyResourceKindInterface:
		return "ontology_interfaces"
	case OntologyResourceKindSharedPropertyType:
		return "shared_property_types"
	case OntologyResourceKindActionType:
		return "action_types"
	case OntologyResourceKindFunctionPackage:
		return "ontology_function_packages"
	case OntologyResourceKindRule:
		return "ontology_rules"
	case OntologyResourceKindObjectSet:
		return "ontology_object_sets"
	}
	return ""
}

// ParseOntologyResourceKind mirrors `TryFrom<&str>` — trims, then
// rejects unknown tokens with the verbatim Rust error string.
func ParseOntologyResourceKind(value string) (OntologyResourceKind, error) {
	switch strings.TrimSpace(value) {
	case "object_type":
		return OntologyResourceKindObjectType, nil
	case "link_type":
		return OntologyResourceKindLinkType, nil
	case "interface":
		return OntologyResourceKindInterface, nil
	case "shared_property_type":
		return OntologyResourceKindSharedPropertyType, nil
	case "action_type":
		return OntologyResourceKindActionType, nil
	case "function_package":
		return OntologyResourceKindFunctionPackage, nil
	case "rule":
		return OntologyResourceKindRule, nil
	case "object_set":
		return OntologyResourceKindObjectSet, nil
	default:
		return "", fmt.Errorf("resource_kind '%s' is not supported; expected one of: object_type, link_type, interface, shared_property_type, action_type, function_package, rule, object_set", value)
	}
}

// ClaimsWorkspaceSlug mirrors `pub fn claims_workspace_slug`. Pulls
// the workspace slug from the session_scope first; falls back to the
// `workspace` attribute, then `default_workspace`. Returns "" when
// nothing resolves — the caller treats empty as None.
func ClaimsWorkspaceSlug(claims *authmw.Claims) string {
	if claims == nil {
		return ""
	}
	if claims.SessionScope != nil && claims.SessionScope.Workspace != nil {
		if v := strings.TrimSpace(*claims.SessionScope.Workspace); v != "" {
			return v
		}
	}
	if v := claimsAttributeString(claims, "workspace"); v != "" {
		return v
	}
	return claimsAttributeString(claims, "default_workspace")
}

// claimsAttributeString reads a string field from the claims
// `attributes` JSON blob (Rust `claims.attribute(<name>)`).
func claimsAttributeString(claims *authmw.Claims, key string) string {
	if claims == nil || len(claims.Attributes) == 0 {
		return ""
	}
	var attrs map[string]any
	if err := json.Unmarshal(claims.Attributes, &attrs); err != nil {
		return ""
	}
	if v, ok := attrs[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// ListAccessibleProjects mirrors `pub async fn list_accessible_projects`.
// Returns the map of project_id → role available to the caller. Admins
// see every project as Owner; everyone else gets the cascade
// owner > membership.role > workspace-fallback Viewer.
func ListAccessibleProjects(ctx context.Context, db *pgxpool.Pool, claims *authmw.Claims) (map[uuid.UUID]models.OntologyProjectRole, error) {
	if claims == nil {
		return map[uuid.UUID]models.OntologyProjectRole{}, nil
	}

	if claims.HasRole("admin") {
		rows, err := db.Query(ctx,
			`SELECT id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at
               FROM ontology_projects`,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[uuid.UUID]models.OntologyProjectRole{}
		for rows.Next() {
			var p models.OntologyProject
			if err := rows.Scan(
				&p.ID, &p.Slug, &p.DisplayName, &p.Description,
				&p.WorkspaceSlug, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt,
			); err != nil {
				return nil, err
			}
			out[p.ID] = models.OntologyProjectRoleOwner
		}
		return out, rows.Err()
	}

	workspace := ClaimsWorkspaceSlug(claims)
	rows, err := db.Query(ctx,
		`SELECT p.id,
                  p.owner_id,
                  p.workspace_slug,
                  m.role AS membership_role
           FROM ontology_projects p
           LEFT JOIN ontology_project_memberships m
                ON m.project_id = p.id AND m.user_id = $1`,
		claims.Sub,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accessible := map[uuid.UUID]models.OntologyProjectRole{}
	for rows.Next() {
		var (
			id              uuid.UUID
			ownerID         uuid.UUID
			workspaceSlug   *string
			membershipRole  *models.OntologyProjectRole
		)
		if err := rows.Scan(&id, &ownerID, &workspaceSlug, &membershipRole); err != nil {
			return nil, err
		}
		switch {
		case ownerID == claims.Sub:
			accessible[id] = models.OntologyProjectRoleOwner
		case membershipRole != nil:
			accessible[id] = *membershipRole
		case workspaceSlug != nil && workspace != "" && *workspaceSlug == workspace:
			accessible[id] = models.OntologyProjectRoleViewer
		}
	}
	return accessible, rows.Err()
}

// EnsureProjectViewAccess mirrors `pub async fn ensure_project_view_access`.
func EnsureProjectViewAccess(ctx context.Context, db *pgxpool.Pool, claims *authmw.Claims, projectID uuid.UUID) (models.OntologyProjectRole, error) {
	accessible, err := ListAccessibleProjects(ctx, db, claims)
	if err != nil {
		return "", fmt.Errorf("failed to evaluate project access: %s", err)
	}
	role, ok := accessible[projectID]
	if !ok {
		return "", fmt.Errorf("forbidden: current user cannot view resources in this ontology project")
	}
	return role, nil
}

// EnsureProjectEditAccess mirrors `pub async fn ensure_project_edit_access`.
// Caller must be at least Editor.
func EnsureProjectEditAccess(ctx context.Context, db *pgxpool.Pool, claims *authmw.Claims, projectID uuid.UUID) (models.OntologyProjectRole, error) {
	role, err := EnsureProjectViewAccess(ctx, db, claims, projectID)
	if err != nil {
		return "", err
	}
	if role.Rank() >= models.OntologyProjectRoleEditor.Rank() {
		return role, nil
	}
	return "", fmt.Errorf("forbidden: current user cannot edit resources in this ontology project")
}

// LoadResourceProjectID mirrors `pub async fn load_resource_project_id`.
// Returns nil when the resource is not bound to any project.
func LoadResourceProjectID(ctx context.Context, db *pgxpool.Pool, kind OntologyResourceKind, resourceID uuid.UUID) (*uuid.UUID, error) {
	var pid uuid.UUID
	err := db.QueryRow(ctx,
		`SELECT project_id
           FROM ontology_project_resources
           WHERE resource_kind = $1 AND resource_id = $2`,
		kind.AsStr(), resourceID,
	).Scan(&pid)
	if err != nil {
		if isNoRowsError(err) {
			return nil, nil
		}
		return nil, err
	}
	return &pid, nil
}

// LoadResourceProjectMap mirrors `pub async fn load_resource_project_map`.
// Filters in-memory against the `wanted` set (Rust does the same).
func LoadResourceProjectMap(ctx context.Context, db *pgxpool.Pool, kind OntologyResourceKind, resourceIDs []uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	if len(resourceIDs) == 0 {
		return map[uuid.UUID]uuid.UUID{}, nil
	}
	wanted := map[uuid.UUID]bool{}
	for _, id := range resourceIDs {
		wanted[id] = true
	}
	rows, err := db.Query(ctx,
		`SELECT resource_id, project_id
           FROM ontology_project_resources
           WHERE resource_kind = $1`,
		kind.AsStr(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var resID, projID uuid.UUID
		if err := rows.Scan(&resID, &projID); err != nil {
			return nil, err
		}
		if wanted[resID] {
			out[resID] = projID
		}
	}
	return out, rows.Err()
}

// ResourceIsVisible mirrors `pub fn resource_is_visible`. Admin sees
// everything; otherwise the resource must be unbound (project_id =
// nil) OR the caller has an entry in `accessibleProjects`.
func ResourceIsVisible(claims *authmw.Claims, projectID *uuid.UUID, accessibleProjects map[uuid.UUID]models.OntologyProjectRole) bool {
	if claims == nil {
		return false
	}
	if claims.HasRole("admin") {
		return true
	}
	if projectID == nil {
		return true
	}
	_, ok := accessibleProjects[*projectID]
	return ok
}

// EnsureResourceViewAccess mirrors `pub async fn ensure_resource_view_access`.
func EnsureResourceViewAccess(ctx context.Context, db *pgxpool.Pool, claims *authmw.Claims, projectID *uuid.UUID) error {
	if projectID == nil {
		return nil
	}
	if _, err := EnsureProjectViewAccess(ctx, db, claims, *projectID); err != nil {
		return err
	}
	return nil
}

// EnsureResourceManageAccess mirrors `pub async fn ensure_resource_manage_access`.
func EnsureResourceManageAccess(ctx context.Context, db *pgxpool.Pool, claims *authmw.Claims, ownerID uuid.UUID, projectID *uuid.UUID) error {
	if claims == nil {
		return fmt.Errorf("forbidden: missing claims")
	}
	if claims.HasRole("admin") {
		return nil
	}
	if projectID != nil {
		if _, err := EnsureProjectEditAccess(ctx, db, claims, *projectID); err != nil {
			return err
		}
		return nil
	}
	if ownerID == claims.Sub {
		return nil
	}
	return fmt.Errorf("forbidden: only the owner can modify an unscoped ontology resource")
}

// LoadResourceOwnerID mirrors `pub async fn load_resource_owner_id`.
// The Rust source does the same string-interpolation; the kind
// table-name must be sourced from the trusted [OntologyResourceKind]
// enum, never from caller input.
func LoadResourceOwnerID(ctx context.Context, db *pgxpool.Pool, kind OntologyResourceKind, resourceID uuid.UUID) (*uuid.UUID, error) {
	table := kind.TableName()
	if table == "" {
		return nil, fmt.Errorf("failed to load resource owner: unknown resource kind %q", kind)
	}
	var owner uuid.UUID
	err := db.QueryRow(ctx,
		fmt.Sprintf("SELECT owner_id FROM %s WHERE id = $1", table),
		resourceID,
	).Scan(&owner)
	if err != nil {
		if isNoRowsError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load resource owner: %s", err)
	}
	return &owner, nil
}

// isNoRowsError matches pgx.ErrNoRows via errors.Is — same shape
// the iter 7a / 7b repos use.
func isNoRowsError(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
