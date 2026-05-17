// role_sets.go: SG.7 — role sets, operations catalog, and the
// delegation-rank helper that lets handlers reject grants that
// exceed the grantor's own rank.

package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── role-sets ────────────────────────────────────────────────────────

func (r *Repo) ListRoleSets(ctx context.Context, tenantID *uuid.UUID, contextFilter string) ([]models.RoleSetResponse, error) {
	pred, args := tenantPredicate("rs", tenantID, 1)
	query := `SELECT rs.id, rs.tenant_id, rs.slug, rs.name, rs.context, rs.description,
	                 rs.created_at, rs.updated_at
	          FROM role_sets rs
	          WHERE ` + pred
	if contextFilter != "" {
		args = append(args, contextFilter)
		query += fmt.Sprintf(" AND rs.context = $%d", len(args))
	}
	query += " ORDER BY rs.context, rs.slug"
	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sets := make([]models.RoleSet, 0)
	for rows.Next() {
		s := models.RoleSet{}
		if err := rows.Scan(&s.ID, &s.TenantID, &s.Slug, &s.Name, &s.Context,
			&s.Description, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		sets = append(sets, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]models.RoleSetResponse, 0, len(sets))
	for _, s := range sets {
		members, err := r.ListRoleSetRoles(ctx, s.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, models.RoleSetResponse{RoleSet: s, Roles: members})
	}
	return out, nil
}

func (r *Repo) GetRoleSet(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) (*models.RoleSetResponse, error) {
	pred, args := tenantPredicate("rs", tenantID, 1)
	args = append(args, id)
	query := `SELECT rs.id, rs.tenant_id, rs.slug, rs.name, rs.context, rs.description,
	                 rs.created_at, rs.updated_at
	          FROM role_sets rs
	          WHERE ` + pred + fmt.Sprintf(" AND rs.id = $%d", len(args))
	row := r.Pool.QueryRow(ctx, query, args...)
	s := models.RoleSet{}
	if err := row.Scan(&s.ID, &s.TenantID, &s.Slug, &s.Name, &s.Context,
		&s.Description, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	members, err := r.ListRoleSetRoles(ctx, id)
	if err != nil {
		return nil, err
	}
	return &models.RoleSetResponse{RoleSet: s, Roles: members}, nil
}

func (r *Repo) GetRoleSetBySlug(ctx context.Context, tenantID *uuid.UUID, slug string) (*models.RoleSetResponse, error) {
	pred, args := tenantPredicate("rs", tenantID, 1)
	args = append(args, slug)
	query := `SELECT rs.id, rs.tenant_id, rs.slug, rs.name, rs.context, rs.description,
	                 rs.created_at, rs.updated_at
	          FROM role_sets rs
	          WHERE ` + pred + fmt.Sprintf(" AND rs.slug = $%d", len(args))
	row := r.Pool.QueryRow(ctx, query, args...)
	s := models.RoleSet{}
	if err := row.Scan(&s.ID, &s.TenantID, &s.Slug, &s.Name, &s.Context,
		&s.Description, &s.CreatedAt, &s.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	members, err := r.ListRoleSetRoles(ctx, s.ID)
	if err != nil {
		return nil, err
	}
	return &models.RoleSetResponse{RoleSet: s, Roles: members}, nil
}

// CreateRoleSet inserts a new role set scoped to `tenantID`.
func (r *Repo) CreateRoleSet(ctx context.Context, tenantID *uuid.UUID, body *models.CreateRoleSetRequest) (*models.RoleSetResponse, error) {
	if !isValidRoleSetContext(body.Context) {
		return nil, fmt.Errorf("context must be project, ontology, restricted_view, or platform_admin")
	}
	id := uuid.New()
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO role_sets (id, tenant_id, slug, name, context, description)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, tenantID, strings.TrimSpace(body.Slug), strings.TrimSpace(body.Name),
		body.Context, body.Description,
	)
	if err != nil {
		return nil, fmt.Errorf("insert role_set: %w", err)
	}
	return r.GetRoleSet(ctx, tenantID, id)
}

func (r *Repo) UpdateRoleSet(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID, body *models.UpdateRoleSetRequest) (*models.RoleSetResponse, error) {
	current, err := r.GetRoleSet(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}
	name := current.Name
	if body.Name != nil {
		name = *body.Name
	}
	desc := current.Description
	if body.Description != nil {
		desc = body.Description
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE role_sets SET name = $2, description = $3, updated_at = NOW()
		 WHERE id = $1`,
		id, name, desc,
	); err != nil {
		return nil, err
	}
	return r.GetRoleSet(ctx, tenantID, id)
}

func (r *Repo) DeleteRoleSet(ctx context.Context, tenantID *uuid.UUID, id uuid.UUID) error {
	pred, args := tenantPredicate("role_sets", tenantID, 1)
	args = append(args, id)
	query := `DELETE FROM role_sets WHERE ` + pred +
		fmt.Sprintf(" AND id = $%d", len(args))
	_, err := r.Pool.Exec(ctx, query, args...)
	return err
}

// ─── role-set roles ──────────────────────────────────────────────────

func (r *Repo) ListRoleSetRoles(ctx context.Context, roleSetID uuid.UUID) ([]models.RoleSetRole, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT rsr.role_set_id, rsr.role_id, roles.name, rsr.rank, rsr.created_at
		 FROM role_set_roles rsr
		 INNER JOIN roles ON roles.id = rsr.role_id
		 WHERE rsr.role_set_id = $1
		 ORDER BY rsr.rank ASC`,
		roleSetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.RoleSetRole, 0)
	for rows.Next() {
		m := models.RoleSetRole{}
		if err := rows.Scan(&m.RoleSetID, &m.RoleID, &m.RoleName, &m.Rank, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Repo) AddRoleToRoleSet(ctx context.Context, roleSetID uuid.UUID, body *models.AddRoleToRoleSetRequest) (*models.RoleSetRole, error) {
	if body.Rank <= 0 {
		return nil, fmt.Errorf("rank must be a positive integer")
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO role_set_roles (role_set_id, role_id, rank)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (role_set_id, role_id) DO UPDATE SET rank = EXCLUDED.rank
		 RETURNING role_set_id, role_id,
		   (SELECT name FROM roles WHERE id = role_set_roles.role_id),
		   rank, created_at`,
		roleSetID, body.RoleID, body.Rank,
	)
	out := &models.RoleSetRole{}
	if err := row.Scan(&out.RoleSetID, &out.RoleID, &out.RoleName, &out.Rank, &out.CreatedAt); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Repo) RemoveRoleFromRoleSet(ctx context.Context, roleSetID, roleID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM role_set_roles WHERE role_set_id = $1 AND role_id = $2`,
		roleSetID, roleID,
	)
	return err
}

// ─── operation catalog ──────────────────────────────────────────────

func (r *Repo) ListOperationCatalog(ctx context.Context, tenantID *uuid.UUID) ([]models.OperationCatalogEntry, error) {
	pred, args := tenantPredicate("permissions", tenantID, 1)
	rows, err := r.Pool.Query(ctx,
		`SELECT id, resource, action, description
		 FROM permissions
		 WHERE `+pred+`
		 ORDER BY resource, action`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.OperationCatalogEntry, 0)
	for rows.Next() {
		e := models.OperationCatalogEntry{}
		if err := rows.Scan(&e.ID, &e.Resource, &e.Action, &e.Description); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── delegation rank check ──────────────────────────────────────────

// HighestRankInRoleSet returns the highest rank a user holds inside a
// role set, via direct user_roles, role_set_roles join, and group
// membership. Returns nil when the user holds no role in the set.
//
// SG.7: this is the primitive that powers the delegation check.
// Handlers call this for the grantor; the target role's rank is the
// rank associated with body.RoleID. The grant is allowed iff
// grantorRank ≥ targetRank.
func (r *Repo) HighestRankInRoleSet(ctx context.Context, roleSetID, userID uuid.UUID) (*int, *uuid.UUID, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT rsr.role_id, rsr.rank
		 FROM role_set_roles rsr
		 WHERE rsr.role_set_id = $1
		   AND (
		     rsr.role_id IN (SELECT role_id FROM user_roles WHERE user_id = $2)
		     OR rsr.role_id IN (
		       SELECT gr.role_id
		       FROM group_roles gr
		       INNER JOIN group_members gm ON gm.group_id = gr.group_id
		       WHERE gm.user_id = $2
		     )
		   )
		 ORDER BY rsr.rank DESC
		 LIMIT 1`,
		roleSetID, userID,
	)
	var roleID uuid.UUID
	var rank int
	if err := row.Scan(&roleID, &rank); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	return &rank, &roleID, nil
}

// RankOfRoleInSet returns the rank of `roleID` inside `roleSetID`, or
// nil when the role is not a member of the set.
func (r *Repo) RankOfRoleInSet(ctx context.Context, roleSetID, roleID uuid.UUID) (*int, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT rank FROM role_set_roles
		 WHERE role_set_id = $1 AND role_id = $2`,
		roleSetID, roleID,
	)
	var rank int
	if err := row.Scan(&rank); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rank, nil
}

// CheckDelegation answers "may `grantor` grant `target` in
// `roleSet`?" SG.7 invariant: grantor must hold a role with rank ≥
// the target role's rank in the same set.
func (r *Repo) CheckDelegation(ctx context.Context, roleSetID, grantor, targetRole uuid.UUID) (*models.CheckDelegationResponse, error) {
	targetRank, err := r.RankOfRoleInSet(ctx, roleSetID, targetRole)
	if err != nil {
		return nil, err
	}
	if targetRank == nil {
		return nil, fmt.Errorf("target role %s is not a member of role set %s", targetRole, roleSetID)
	}
	grantorRank, grantorRoleID, err := r.HighestRankInRoleSet(ctx, roleSetID, grantor)
	if err != nil {
		return nil, err
	}
	resp := &models.CheckDelegationResponse{
		TargetRoleID:  targetRole,
		TargetRank:    *targetRank,
		GrantorRoleID: grantorRoleID,
		GrantorRank:   grantorRank,
	}
	if grantorRank == nil {
		resp.Allowed = false
		resp.Reason = "grantor holds no role in this role set"
		return resp, nil
	}
	if *grantorRank >= *targetRank {
		resp.Allowed = true
		return resp, nil
	}
	resp.Allowed = false
	resp.Reason = fmt.Sprintf("grantor rank %d is below target rank %d", *grantorRank, *targetRank)
	return resp, nil
}

// ─── helpers ────────────────────────────────────────────────────────

func isValidRoleSetContext(c string) bool {
	switch c {
	case models.RoleSetContextProject,
		models.RoleSetContextOntology,
		models.RoleSetContextRestrictedView,
		models.RoleSetContextPlatformAdmin:
		return true
	default:
		return false
	}
}
