// Package domain holds the cross-handler invariants for
// tenancy-organizations-service: ontology project access checks and
// JWT-driven tenant resolution.
//
// Public surface mirrors the Rust `src/domain/*.rs` modules verbatim
// — function signatures, error strings and SQL stay byte-exact so that
// downstream services see identical behaviour regardless of which
// language emitted the response.
package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// OntologyResourceKind enumerates the resources the ontology project
// access checker knows how to scope. Values match the wire spelling
// (snake_case) used by the Rust enum.
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

// String returns the canonical wire spelling (snake_case).
func (k OntologyResourceKind) String() string { return string(k) }

// TableName returns the physical table the resource lives in. Used
// when loading an owner_id by `(kind, id)`.
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

// ParseOntologyResourceKind matches the Rust `TryFrom<&str>` impl: it
// trims surrounding whitespace, accepts the eight canonical spellings
// and returns the same error message verbatim for unknown values.
func ParseOntologyResourceKind(value string) (OntologyResourceKind, error) {
	switch strings.TrimSpace(value) {
	case string(OntologyResourceKindObjectType):
		return OntologyResourceKindObjectType, nil
	case string(OntologyResourceKindLinkType):
		return OntologyResourceKindLinkType, nil
	case string(OntologyResourceKindInterface):
		return OntologyResourceKindInterface, nil
	case string(OntologyResourceKindSharedPropertyType):
		return OntologyResourceKindSharedPropertyType, nil
	case string(OntologyResourceKindActionType):
		return OntologyResourceKindActionType, nil
	case string(OntologyResourceKindFunctionPackage):
		return OntologyResourceKindFunctionPackage, nil
	case string(OntologyResourceKindRule):
		return OntologyResourceKindRule, nil
	case string(OntologyResourceKindObjectSet):
		return OntologyResourceKindObjectSet, nil
	}
	return "", fmt.Errorf(
		"resource_kind '%s' is not supported; expected one of: object_type, link_type, interface, shared_property_type, action_type, function_package, rule, object_set",
		strings.TrimSpace(value),
	)
}

// ClaimsWorkspaceSlug resolves the active workspace slug from JWT
// claims, preferring (in order) the session scope, the
// `workspace` attribute, and the `default_workspace` attribute. The
// final value is trimmed; a whitespace-only or empty result is
// reported as nil.
func ClaimsWorkspaceSlug(claims *authmw.Claims) *string {
	if claims == nil {
		return nil
	}

	var raw string
	var found bool

	if claims.SessionScope != nil && claims.SessionScope.Workspace != nil {
		raw = *claims.SessionScope.Workspace
		found = true
	}
	if !found {
		if v, ok := claims.Attribute("workspace"); ok {
			if s, isStr := v.(string); isStr {
				raw = s
				found = true
			}
		}
	}
	if !found {
		if v, ok := claims.Attribute("default_workspace"); ok {
			if s, isStr := v.(string); isStr {
				raw = s
				found = true
			}
		}
	}
	if !found {
		return nil
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ListAccessibleProjects returns every ontology project the subject
// can see, paired with its effective role.
//
// Admins see every project as Owner; otherwise membership wins over
// ownership wins over workspace match (which yields `viewer`). Same
// precedence ordering as the Rust implementation.
func ListAccessibleProjects(
	ctx context.Context,
	db *pgxpool.Pool,
	claims *authmw.Claims,
) (map[uuid.UUID]models.OntologyProjectRole, error) {
	if claims.HasRole("admin") {
		rows, err := db.Query(ctx,
			`SELECT id, slug, display_name, description, workspace_slug, owner_id, created_at, updated_at
			 FROM ontology_projects`,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		out := make(map[uuid.UUID]models.OntologyProjectRole)
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
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return out, nil
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

	accessible := make(map[uuid.UUID]models.OntologyProjectRole)
	for rows.Next() {
		var (
			id             uuid.UUID
			ownerID        uuid.UUID
			workspaceSlug  *string
			membershipRole *string
		)
		if err := rows.Scan(&id, &ownerID, &workspaceSlug, &membershipRole); err != nil {
			return nil, err
		}

		var role models.OntologyProjectRole
		switch {
		case ownerID == claims.Sub:
			role = models.OntologyProjectRoleOwner
		case membershipRole != nil:
			parsed, parseErr := models.ParseOntologyProjectRole(*membershipRole)
			if parseErr != nil {
				continue
			}
			role = parsed
		case sameWorkspace(workspaceSlug, workspace):
			role = models.OntologyProjectRoleViewer
		default:
			continue
		}
		accessible[id] = role
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accessible, nil
}

// EnsureProjectViewAccess confirms the subject may view `projectID` and
// returns the effective role. Mirrors the Rust error messages verbatim.
func EnsureProjectViewAccess(
	ctx context.Context,
	db *pgxpool.Pool,
	claims *authmw.Claims,
	projectID uuid.UUID,
) (models.OntologyProjectRole, error) {
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

// EnsureProjectEditAccess confirms the subject may edit `projectID`.
// View access is required and the effective role must be at least
// editor in the lattice.
func EnsureProjectEditAccess(
	ctx context.Context,
	db *pgxpool.Pool,
	claims *authmw.Claims,
	projectID uuid.UUID,
) (models.OntologyProjectRole, error) {
	role, err := EnsureProjectViewAccess(ctx, db, claims, projectID)
	if err != nil {
		return "", err
	}
	if role.Rank() >= models.OntologyProjectRoleEditor.Rank() {
		return role, nil
	}
	return "", fmt.Errorf("forbidden: current user cannot edit resources in this ontology project")
}

// LoadResourceProjectID returns the project a single ontology resource
// is bound to, or nil when the resource is unscoped.
func LoadResourceProjectID(
	ctx context.Context,
	db *pgxpool.Pool,
	resourceKind OntologyResourceKind,
	resourceID uuid.UUID,
) (*uuid.UUID, error) {
	row := db.QueryRow(ctx,
		`SELECT project_id
		 FROM ontology_project_resources
		 WHERE resource_kind = $1 AND resource_id = $2`,
		string(resourceKind), resourceID,
	)
	var projectID uuid.UUID
	if err := row.Scan(&projectID); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &projectID, nil
}

// LoadResourceProjectMap returns the `(resource_id → project_id)`
// mapping for every resource of `resourceKind` whose id appears in
// `resourceIDs`. The query mirrors the Rust impl: scan all rows for
// the kind and filter in memory by the requested set, so the
// behaviour stays identical when the table is small enough that the
// whole-table scan is the cheaper plan.
func LoadResourceProjectMap(
	ctx context.Context,
	db *pgxpool.Pool,
	resourceKind OntologyResourceKind,
	resourceIDs []uuid.UUID,
) (map[uuid.UUID]uuid.UUID, error) {
	if len(resourceIDs) == 0 {
		return map[uuid.UUID]uuid.UUID{}, nil
	}

	wanted := make(map[uuid.UUID]struct{}, len(resourceIDs))
	for _, id := range resourceIDs {
		wanted[id] = struct{}{}
	}

	rows, err := db.Query(ctx,
		`SELECT resource_id, project_id
		 FROM ontology_project_resources
		 WHERE resource_kind = $1`,
		string(resourceKind),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[uuid.UUID]uuid.UUID)
	for rows.Next() {
		var (
			resourceID uuid.UUID
			projectID  uuid.UUID
		)
		if err := rows.Scan(&resourceID, &projectID); err != nil {
			return nil, err
		}
		if _, ok := wanted[resourceID]; ok {
			out[resourceID] = projectID
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ResourceIsVisible decides whether a resource bound to `projectID`
// (nil = unscoped) is visible to `claims` given the precomputed
// accessible-project map. Admins see everything; unscoped resources
// are visible to everyone.
func ResourceIsVisible(
	claims *authmw.Claims,
	projectID *uuid.UUID,
	accessibleProjects map[uuid.UUID]models.OntologyProjectRole,
) bool {
	if claims.HasRole("admin") {
		return true
	}
	if projectID == nil {
		return true
	}
	_, ok := accessibleProjects[*projectID]
	return ok
}

// EnsureResourceViewAccess is the per-row complement of
// `EnsureProjectViewAccess`: it short-circuits when the resource is
// unscoped and otherwise delegates to the project-level check.
func EnsureResourceViewAccess(
	ctx context.Context,
	db *pgxpool.Pool,
	claims *authmw.Claims,
	projectID *uuid.UUID,
) error {
	if projectID == nil {
		return nil
	}
	if _, err := EnsureProjectViewAccess(ctx, db, claims, *projectID); err != nil {
		return err
	}
	return nil
}

// EnsureResourceManageAccess checks the subject's authority to mutate
// a resource. Admins always pass. A scoped resource defers to project
// edit access. Unscoped resources require ownership; everyone else is
// denied with the canonical "only the owner can modify" message.
func EnsureResourceManageAccess(
	ctx context.Context,
	db *pgxpool.Pool,
	claims *authmw.Claims,
	ownerID uuid.UUID,
	projectID *uuid.UUID,
) error {
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

// LoadResourceOwnerID looks up the `owner_id` column of a single
// ontology resource by `(kind, id)`. Returns nil + nil error when the
// row is missing, mirroring the Rust `Option` return.
func LoadResourceOwnerID(
	ctx context.Context,
	db *pgxpool.Pool,
	resourceKind OntologyResourceKind,
	resourceID uuid.UUID,
) (*uuid.UUID, error) {
	table := resourceKind.TableName()
	if table == "" {
		return nil, fmt.Errorf("failed to load resource owner: unknown resource kind '%s'", resourceKind)
	}
	query := fmt.Sprintf("SELECT owner_id FROM %s WHERE id = $1", table)
	row := db.QueryRow(ctx, query, resourceID)
	var ownerID uuid.UUID
	if err := row.Scan(&ownerID); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to load resource owner: %s", err)
	}
	return &ownerID, nil
}

// sameWorkspace mirrors `row.workspace_slug.as_ref() == workspace.as_ref()`
// — both nil compares equal, otherwise the underlying strings must match.
func sameWorkspace(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// isNoRows reports whether `err` is the pgx "no rows in result set"
// sentinel — used so empty-result lookups translate to a `nil`
// pointer instead of a hard failure.
func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
