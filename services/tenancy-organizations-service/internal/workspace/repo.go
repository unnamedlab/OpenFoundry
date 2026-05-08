package workspace

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo wraps the SQL surface for favorites + recents.
type Repo struct{ Pool *pgxpool.Pool }

// ─── Favorites ──────────────────────────────────────────────────────

// CreateFavorite is idempotent — re-favoriting the same resource
// returns the existing row (mirrors Rust ON CONFLICT … DO UPDATE).
func (r *Repo) CreateFavorite(ctx context.Context, userID uuid.UUID, kind ResourceKind, resourceID uuid.UUID) (*UserFavorite, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO user_favorites (user_id, resource_kind, resource_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (user_id, resource_kind, resource_id) DO UPDATE
		     SET created_at = user_favorites.created_at
		 RETURNING user_id, resource_kind, resource_id, created_at`,
		userID, string(kind), resourceID)
	f := &UserFavorite{}
	var k string
	if err := row.Scan(&f.UserID, &k, &f.ResourceID, &f.CreatedAt); err != nil {
		return nil, err
	}
	f.ResourceKind = ResourceKind(k)
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
			`SELECT user_id, resource_kind, resource_id, created_at
			 FROM user_favorites WHERE user_id = $1
			 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	} else {
		rows, err = r.Pool.Query(ctx,
			`SELECT user_id, resource_kind, resource_id, created_at
			 FROM user_favorites WHERE user_id = $1 AND resource_kind = $2
			 ORDER BY created_at DESC LIMIT $3`, userID, string(kind), limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]UserFavorite, 0)
	for rows.Next() {
		var f UserFavorite
		var k string
		if err := rows.Scan(&f.UserID, &k, &f.ResourceID, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.ResourceKind = ResourceKind(k)
		out = append(out, f)
	}
	return out, rows.Err()
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

// ListRecentsByUser returns the most recent unique (kind, id) rows
// for `userID`, optionally filtered to a single kind. The DISTINCT ON
// matches the Rust SQL exactly so the dedup semantics are preserved.
func (r *Repo) ListRecentsByUser(ctx context.Context, userID uuid.UUID, kind ResourceKind, limit int) ([]RecentEntry, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	var (
		rows pgxRowsLike
		err  error
	)
	if kind == "" {
		rows, err = r.Pool.Query(ctx,
			`SELECT DISTINCT ON (resource_kind, resource_id)
			        resource_kind, resource_id, accessed_at AS last_accessed_at
			 FROM resource_access_log
			 WHERE user_id = $1
			 ORDER BY resource_kind, resource_id, accessed_at DESC
			 LIMIT $2`, userID, limit)
	} else {
		rows, err = r.Pool.Query(ctx,
			`SELECT DISTINCT ON (resource_kind, resource_id)
			        resource_kind, resource_id, accessed_at AS last_accessed_at
			 FROM resource_access_log
			 WHERE user_id = $1 AND resource_kind = $2
			 ORDER BY resource_kind, resource_id, accessed_at DESC
			 LIMIT $3`, userID, string(kind), limit)
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
	// Re-sort by recency so the response order is stable for the UI
	// (DISTINCT ON requires sorting by the dedup key first).
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastAccessedAt.After(out[j].LastAccessedAt)
	})
	return out, nil
}

// pgxRowsLike narrows the pgx.Rows surface used in this package so
// tests can stub it without pulling in pgxpool.
type pgxRowsLike interface {
	Next() bool
	Scan(...any) error
	Close()
	Err() error
}
