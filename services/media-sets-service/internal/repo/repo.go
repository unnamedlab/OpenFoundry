// Package repo holds SQL queries + embedded migrations for media-sets-service.
package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

type Repo struct{ Pool *pgxpool.Pool }

const mediaSetSelect = `SELECT rid, project_rid, name, schema, allowed_mime_types,
	transaction_policy, retention_seconds, virtual, source_rid, markings,
	created_at, created_by FROM media_sets`

// MintRID generates a new media set RID using the Foundry convention.
func MintRID() string {
	return "ri.foundry.main.media_set." + uuid.New().String()
}

func (r *Repo) ListMediaSets(ctx context.Context, projectRID string) ([]models.MediaSet, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if projectRID != "" {
		rows, err = r.Pool.Query(ctx, mediaSetSelect+` WHERE project_rid = $1 ORDER BY created_at DESC LIMIT 500`, projectRID)
	} else {
		rows, err = r.Pool.Query(ctx, mediaSetSelect+` ORDER BY created_at DESC LIMIT 500`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MediaSet, 0)
	for rows.Next() {
		v, err := scanMediaSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetMediaSet(ctx context.Context, rid string) (*models.MediaSet, error) {
	row := r.Pool.QueryRow(ctx, mediaSetSelect+` WHERE rid = $1`, rid)
	v, err := scanMediaSet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateMediaSet(ctx context.Context, body *models.CreateMediaSetRequest, createdBy string) (*models.MediaSet, error) {
	rid := MintRID()
	mimes := body.AllowedMimeTypes
	if mimes == nil {
		mimes = []string{}
	}
	policy := "TRANSACTIONLESS"
	if body.TransactionPolicy != nil && *body.TransactionPolicy != "" {
		policy = *body.TransactionPolicy
	}
	retention := int64(0)
	if body.RetentionSeconds != nil {
		retention = *body.RetentionSeconds
	}
	virtual := false
	if body.Virtual != nil {
		virtual = *body.Virtual
	}
	markings := body.Markings
	if markings == nil {
		markings = []string{}
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO media_sets
		    (rid, project_rid, name, schema, allowed_mime_types,
		     transaction_policy, retention_seconds, virtual, source_rid,
		     markings, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING rid, project_rid, name, schema, allowed_mime_types,
		           transaction_policy, retention_seconds, virtual, source_rid,
		           markings, created_at, created_by`,
		rid, body.ProjectRID, strings.TrimSpace(body.Name), body.Schema,
		mimes, policy, retention, virtual, body.SourceRID, markings, createdBy,
	)
	return scanMediaSet(row)
}

func (r *Repo) UpdateMediaSet(ctx context.Context, rid string, body *models.UpdateMediaSetRequest) (*models.MediaSet, error) {
	current, err := r.GetMediaSet(ctx, rid)
	if err != nil || current == nil {
		return current, err
	}
	mimes := current.AllowedMimeTypes
	if body.AllowedMimeTypes != nil {
		mimes = body.AllowedMimeTypes
	}
	retention := current.RetentionSeconds
	if body.RetentionSeconds != nil {
		retention = *body.RetentionSeconds
	}
	markings := current.Markings
	if body.Markings != nil {
		markings = body.Markings
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE media_sets SET
		    allowed_mime_types = $2, retention_seconds = $3, markings = $4
		  WHERE rid = $1
		  RETURNING rid, project_rid, name, schema, allowed_mime_types,
		            transaction_policy, retention_seconds, virtual, source_rid,
		            markings, created_at, created_by`,
		rid, mimes, retention, markings,
	)
	return scanMediaSet(row)
}

func (r *Repo) DeleteMediaSet(ctx context.Context, rid string) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM media_sets WHERE rid = $1`, rid)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

// UpdateRetentionSeconds rewrites the retention window on a media set
// and returns the previous value + the new row. The reaper consumes
// the previous value to decide whether the change reduces the window
// (and therefore needs an immediate sweep).
//
// Returns (nil, nil, nil) when the row does not exist.
func (r *Repo) UpdateRetentionSeconds(ctx context.Context, rid string, newSeconds int64) (previous int64, updated *models.MediaSet, err error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE media_sets SET retention_seconds = $2
		  WHERE rid = $1
		 RETURNING (
		     SELECT retention_seconds FROM media_sets WHERE rid = $1
		 ) AS previous,
		   rid, project_rid, name, schema, allowed_mime_types,
		   transaction_policy, retention_seconds, virtual, source_rid,
		   markings, created_at, created_by`,
		rid, newSeconds,
	)
	v := &models.MediaSet{}
	if err := row.Scan(&previous,
		&v.RID, &v.ProjectRID, &v.Name, &v.Schema,
		&v.AllowedMimeTypes, &v.TransactionPolicy, &v.RetentionSeconds,
		&v.Virtual, &v.SourceRID, &v.Markings, &v.CreatedAt, &v.CreatedBy,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	if v.AllowedMimeTypes == nil {
		v.AllowedMimeTypes = []string{}
	}
	if v.Markings == nil {
		v.Markings = []string{}
	}
	return previous, v, nil
}

type rowLikeT interface{ Scan(...any) error }

func scanMediaSet(r rowLikeT) (*models.MediaSet, error) {
	v := &models.MediaSet{}
	if err := r.Scan(&v.RID, &v.ProjectRID, &v.Name, &v.Schema,
		&v.AllowedMimeTypes, &v.TransactionPolicy, &v.RetentionSeconds,
		&v.Virtual, &v.SourceRID, &v.Markings, &v.CreatedAt, &v.CreatedBy); err != nil {
		return nil, err
	}
	if v.AllowedMimeTypes == nil {
		v.AllowedMimeTypes = []string{}
	}
	if v.Markings == nil {
		v.Markings = []string{}
	}
	return v, nil
}
