package workspace

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo wraps the SQL surface for favorites + recents.
type Repo struct{ Pool *pgxpool.Pool }

// ─── Favorites ──────────────────────────────────────────────────────

// CreateFavorite is idempotent — re-favoriting the same resource
// returns the existing row (mirrors Rust ON CONFLICT … DO UPDATE).
func (r *Repo) CreateFavorite(ctx context.Context, userID uuid.UUID, kind ResourceKind, resourceID uuid.UUID, groupID *uuid.UUID, displayOrder *int) (*UserFavorite, error) {
	if err := r.ensureFavoriteGroupBelongsToUser(ctx, userID, groupID); err != nil {
		return nil, err
	}
	order := 0
	if displayOrder != nil {
		order = *displayOrder
	} else {
		next, err := r.nextFavoriteDisplayOrder(ctx, userID, groupID)
		if err != nil {
			return nil, err
		}
		order = next
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO user_favorites (user_id, resource_kind, resource_id, group_id, display_order, updated_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (user_id, resource_kind, resource_id) DO UPDATE
		     SET group_id = EXCLUDED.group_id,
		         display_order = EXCLUDED.display_order,
		         updated_at = NOW()
		 RETURNING user_id, resource_kind, resource_id, COALESCE(group_id::text, ''),
		           display_order, created_at, updated_at`,
		userID, string(kind), resourceID, groupIDParam(groupID), order)
	f := &UserFavorite{}
	var k string
	var groupText string
	if err := row.Scan(&f.UserID, &k, &f.ResourceID, &groupText, &f.DisplayOrder, &f.CreatedAt, &f.UpdatedAt); err != nil {
		return nil, err
	}
	f.ResourceKind = ResourceKind(k)
	f.GroupID = parseOptionalUUID(groupText)
	return f, nil
}

// ListFavoritesByUser optionally filters on a single resource_kind.
func (r *Repo) ListFavoritesByUser(ctx context.Context, userID uuid.UUID, kind ResourceKind, limit int) ([]UserFavorite, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}
	var (
		rows pgxRowsLike
		err  error
	)
	if kind == "" {
		rows, err = r.Pool.Query(ctx,
			`SELECT f.user_id, f.resource_kind, f.resource_id, COALESCE(f.group_id::text, ''),
			        f.display_order, f.created_at, f.updated_at
			   FROM user_favorites f
			   LEFT JOIN user_favorite_groups g
			     ON g.id = f.group_id AND g.user_id = f.user_id
			  WHERE f.user_id = $1
			  ORDER BY CASE WHEN f.group_id IS NULL THEN 0 ELSE 1 END,
			           COALESCE(g.display_order, 2147483647),
			           f.display_order,
			           f.created_at DESC
			  LIMIT $2`, userID, limit)
	} else {
		rows, err = r.Pool.Query(ctx,
			`SELECT f.user_id, f.resource_kind, f.resource_id, COALESCE(f.group_id::text, ''),
			        f.display_order, f.created_at, f.updated_at
			   FROM user_favorites f
			   LEFT JOIN user_favorite_groups g
			     ON g.id = f.group_id AND g.user_id = f.user_id
			  WHERE f.user_id = $1 AND f.resource_kind = $2
			  ORDER BY CASE WHEN f.group_id IS NULL THEN 0 ELSE 1 END,
			           COALESCE(g.display_order, 2147483647),
			           f.display_order,
			           f.created_at DESC
			  LIMIT $3`, userID, string(kind), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UserFavorite, 0)
	for rows.Next() {
		var f UserFavorite
		var k string
		var groupText string
		if err := rows.Scan(&f.UserID, &k, &f.ResourceID, &groupText, &f.DisplayOrder, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.ResourceKind = ResourceKind(k)
		f.GroupID = parseOptionalUUID(groupText)
		out = append(out, f)
	}
	return out, rows.Err()
}

// CreateFavoriteGroup creates or reuses a named group in the caller's profile.
func (r *Repo) CreateFavoriteGroup(ctx context.Context, userID uuid.UUID, name string, displayOrder *int) (*FavoriteGroup, error) {
	name = strings.TrimSpace(name)
	order := 0
	if displayOrder != nil {
		order = *displayOrder
	} else {
		next, err := r.nextFavoriteGroupDisplayOrder(ctx, userID)
		if err != nil {
			return nil, err
		}
		order = next
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO user_favorite_groups (user_id, name, display_order, updated_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (user_id, name) DO UPDATE
		     SET updated_at = NOW()
		 RETURNING id, user_id, name, display_order, created_at, updated_at`,
		userID, name, order)
	var g FavoriteGroup
	if err := row.Scan(&g.ID, &g.UserID, &g.Name, &g.DisplayOrder, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

// ListFavoriteGroupsByUser returns groups in display order.
func (r *Repo) ListFavoriteGroupsByUser(ctx context.Context, userID uuid.UUID) ([]FavoriteGroup, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, user_id, name, display_order, created_at, updated_at
		   FROM user_favorite_groups
		  WHERE user_id = $1
		  ORDER BY display_order, name, created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]FavoriteGroup, 0)
	for rows.Next() {
		var g FavoriteGroup
		if err := rows.Scan(&g.ID, &g.UserID, &g.Name, &g.DisplayOrder, &g.CreatedAt, &g.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// UpdateFavoriteOrder moves favorites between groups and persists their
// user-visible ordering.
func (r *Repo) UpdateFavoriteOrder(ctx context.Context, userID uuid.UUID, items []FavoriteOrderItem) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, item := range items {
		if err := r.ensureFavoriteGroupBelongsToUser(ctx, userID, item.GroupID); err != nil {
			return err
		}
		kind, err := ParseResourceKind(item.ResourceKind)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`UPDATE user_favorites
			    SET group_id = $4,
			        display_order = $5,
			        updated_at = NOW()
			  WHERE user_id = $1 AND resource_kind = $2 AND resource_id = $3`,
			userID, string(kind), item.ResourceID, groupIDParam(item.GroupID), item.DisplayOrder)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UpdateFavoriteGroupsOrder persists the order of the caller's groups.
func (r *Repo) UpdateFavoriteGroupsOrder(ctx context.Context, userID uuid.UUID, groups []FavoriteGroupOrderItem) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, group := range groups {
		_, err = tx.Exec(ctx,
			`UPDATE user_favorite_groups
			    SET display_order = $3,
			        updated_at = NOW()
			  WHERE user_id = $1 AND id = $2`,
			userID, group.ID, group.DisplayOrder)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteFavorite returns false when no row was affected (404 mapping).
func (r *Repo) DeleteFavorite(ctx context.Context, userID uuid.UUID, kind ResourceKind, resourceID uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx,
		`DELETE FROM user_favorites
		 WHERE user_id = $1 AND resource_kind = $2 AND resource_id = $3`,
		userID, string(kind), resourceID)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func (r *Repo) ensureFavoriteGroupBelongsToUser(ctx context.Context, userID uuid.UUID, groupID *uuid.UUID) error {
	if groupID == nil {
		return nil
	}
	var ok bool
	err := r.Pool.QueryRow(ctx,
		`SELECT EXISTS (
		    SELECT 1 FROM user_favorite_groups WHERE user_id = $1 AND id = $2
		)`, userID, *groupID).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return ErrFavoriteGroupNotFound
	}
	return nil
}

func (r *Repo) nextFavoriteDisplayOrder(ctx context.Context, userID uuid.UUID, groupID *uuid.UUID) (int, error) {
	var next int
	err := r.Pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(display_order), 0) + 1000
		   FROM user_favorites
		  WHERE user_id = $1
		    AND group_id IS NOT DISTINCT FROM $2::uuid`,
		userID, groupIDParam(groupID)).Scan(&next)
	return next, err
}

func (r *Repo) nextFavoriteGroupDisplayOrder(ctx context.Context, userID uuid.UUID) (int, error) {
	var next int
	err := r.Pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(display_order), 0) + 1000
		   FROM user_favorite_groups
		  WHERE user_id = $1`, userID).Scan(&next)
	return next, err
}

func groupIDParam(groupID *uuid.UUID) any {
	if groupID == nil {
		return nil
	}
	return *groupID
}

func parseOptionalUUID(raw string) *uuid.UUID {
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return &id
}

// ─── Recents ────────────────────────────────────────────────────────

// RecordAccess inserts a new resource_access_log row. Best-effort —
// callers should not fail their request if this errors.
func (r *Repo) RecordAccess(ctx context.Context, userID uuid.UUID, kind ResourceKind, resourceID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO resource_access_log (user_id, resource_kind, resource_id, accessed_at)
		 VALUES ($1, $2, $3, $4)`,
		userID, string(kind), resourceID, time.Now().UTC())
	return err
}

// ListRecentsByUser returns the most recent unique (kind, id) rows for
// `userID`, optionally filtered to a single kind. Results are filtered
// to resources still visible in the caller's accessible projects so
// permission revocations disappear from the personalized recents list.
func (r *Repo) ListRecentsByUser(ctx context.Context, userID uuid.UUID, kind ResourceKind, limit int, accessibleProjectIDs []uuid.UUID) ([]RecentEntry, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	if len(accessibleProjectIDs) == 0 {
		return []RecentEntry{}, nil
	}
	var (
		rows pgxRowsLike
		err  error
	)
	if kind == "" {
		rows, err = r.Pool.Query(ctx,
			listRecentsSQL("", 2, 3),
			userID, accessibleProjectIDs, limit)
	} else {
		rows, err = r.Pool.Query(ctx,
			listRecentsSQL("AND resource_kind = $2", 3, 4),
			userID, string(kind), accessibleProjectIDs, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RecentEntry, 0)
	for rows.Next() {
		var e RecentEntry
		var k string
		if err := rows.Scan(&k, &e.ResourceID, &e.LastAccessedAt); err != nil {
			return nil, err
		}
		e.ResourceKind = ResourceKind(k)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func listRecentsSQL(kindPredicate string, projectsParam int, limitParam int) string {
	return `
WITH latest AS (
	SELECT DISTINCT ON (resource_kind, resource_id)
	       resource_kind, resource_id, accessed_at AS last_accessed_at
	  FROM resource_access_log
	 WHERE user_id = $1 ` + kindPredicate + `
	 ORDER BY resource_kind, resource_id, accessed_at DESC
)
SELECT resource_kind, resource_id, last_accessed_at
  FROM latest l
 WHERE ` + recentVisiblePredicate(projectsParam) + `
 ORDER BY last_accessed_at DESC, resource_kind ASC, resource_id ASC
 LIMIT $` + strconv.Itoa(limitParam)
}

func recentVisiblePredicate(projectsParam int) string {
	projectIDs := "$" + strconv.Itoa(projectsParam) + "::uuid[]"
	return `
	(
		l.resource_kind = 'ontology_project'
		AND EXISTS (
			SELECT 1
			  FROM ontology_projects p
			 WHERE p.id = l.resource_id
			   AND p.id = ANY(` + projectIDs + `)
			   AND p.is_deleted = FALSE
		)
	)
	OR (
		l.resource_kind = 'ontology_folder'
		AND EXISTS (
			SELECT 1
			  FROM ontology_project_folders f
			 WHERE f.id = l.resource_id
			   AND f.project_id = ANY(` + projectIDs + `)
			   AND f.is_deleted = FALSE
		)
	)
	OR (
		l.resource_kind = 'ontology_resource_binding'
		AND EXISTS (
			SELECT 1
			  FROM ontology_project_resources r
			 WHERE r.resource_id = l.resource_id
			   AND r.project_id = ANY(` + projectIDs + `)
			   AND r.is_deleted = FALSE
		)
	)
	OR (
		l.resource_kind NOT IN ('ontology_project', 'ontology_folder', 'ontology_resource_binding')
		AND EXISTS (
			SELECT 1
			  FROM ontology_project_resources r
			 WHERE r.resource_kind = l.resource_kind
			   AND r.resource_id = l.resource_id
			   AND r.project_id = ANY(` + projectIDs + `)
			   AND r.is_deleted = FALSE
		)
	)
	OR (
		l.resource_kind NOT IN ('ontology_project', 'ontology_folder', 'ontology_resource_binding')
		AND EXISTS (
			SELECT 1
			  FROM compass_resource_search_index idx
			 WHERE idx.resource_rid = CASE l.resource_kind
				WHEN 'dataset' THEN 'ri.foundry.main.dataset.' || l.resource_id::text
				WHEN 'pipeline' THEN 'ri.foundry.main.pipeline.' || l.resource_id::text
				WHEN 'query' THEN 'ri.foundry.main.query.' || l.resource_id::text
				WHEN 'notebook' THEN 'ri.foundry.main.notebook.' || l.resource_id::text
				WHEN 'app' THEN 'ri.foundry.main.app.' || l.resource_id::text
				WHEN 'dashboard' THEN 'ri.foundry.main.dashboard.' || l.resource_id::text
				WHEN 'report' THEN 'ri.foundry.main.report.' || l.resource_id::text
				WHEN 'model' THEN 'ri.foundry.main.model.' || l.resource_id::text
				WHEN 'workflow' THEN 'ri.foundry.main.workflow.' || l.resource_id::text
				ELSE 'ri.openfoundry.main.resource.' || l.resource_id::text
			END
			   AND idx.owning_project_id = ANY(` + projectIDs + `)
			   AND idx.is_deleted = FALSE
		)
	)`
}

// pgxRowsLike narrows the pgx.Rows surface used in this package so
// tests can stub it without pulling in pgxpool.
type pgxRowsLike interface {
	Next() bool
	Scan(...any) error
	Close()
	Err() error
}
