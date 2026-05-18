package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
)

// ─── Builds ─────────────────────────────────────────────────────────

// CreateBuild inserts a new build in `queued` state.
func (r *Repo) CreateBuild(ctx context.Context, b *domain.SDKBuild, includeObjects, includeActions []string) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.Status == "" {
		b.Status = domain.StatusQueued
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	includeObjectsJSON, err := jsonOrEmpty(includeObjects)
	if err != nil {
		return err
	}
	includeActionsJSON, err := jsonOrEmpty(includeActions)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx, `
		INSERT INTO sdk_builds
		    (id, tenant_id, ontology_version, target, status, artifact_uri, error_message, requested_by,
		     include_object_types, include_action_types, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, b.ID, b.TenantID, b.OntologyVersion, string(b.Target), string(b.Status), b.ArtifactURI, b.ErrorMessage, b.RequestedBy,
		includeObjectsJSON, includeActionsJSON, b.CreatedAt)
	return err
}

// GetBuild returns the build by id. Returns (nil, nil) when missing —
// matches the existing repo style on jobs/publications.
func (r *Repo) GetBuild(ctx context.Context, id uuid.UUID) (*domain.SDKBuild, error) {
	row := r.Pool.QueryRow(ctx, `
		SELECT id, tenant_id, ontology_version, target, status, artifact_uri, error_message, requested_by,
		       created_at, finished_at
		FROM sdk_builds WHERE id = $1
	`, id)
	b := &domain.SDKBuild{}
	var target, status string
	var finishedAt *time.Time
	if err := row.Scan(&b.ID, &b.TenantID, &b.OntologyVersion, &target, &status, &b.ArtifactURI, &b.ErrorMessage,
		&b.RequestedBy, &b.CreatedAt, &finishedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	b.Target = domain.Target(target)
	b.Status = domain.Status(status)
	b.FinishedAt = finishedAt
	return b, nil
}

// ListBuildsFilter is what handlers pass to ListBuilds. Zero values
// mean "no filter for this field".
type ListBuildsFilter struct {
	TenantID uuid.UUID
	Target   domain.Target
	Status   domain.Status
	Limit    int
}

// ListBuilds returns the most recent builds matching the filter.
func (r *Repo) ListBuilds(ctx context.Context, f ListBuildsFilter) ([]domain.SDKBuild, error) {
	query := `SELECT id, tenant_id, ontology_version, target, status, artifact_uri, error_message,
	                 requested_by, created_at, finished_at
	          FROM sdk_builds`
	args := []any{}
	wheres := []string{}
	if f.TenantID != uuid.Nil {
		args = append(args, f.TenantID)
		wheres = append(wheres, fmt.Sprintf("tenant_id = $%d", len(args)))
	}
	if f.Target != "" {
		args = append(args, string(f.Target))
		wheres = append(wheres, fmt.Sprintf("target = $%d", len(args)))
	}
	if f.Status != "" {
		args = append(args, string(f.Status))
		wheres = append(wheres, fmt.Sprintf("status = $%d", len(args)))
	}
	if len(wheres) > 0 {
		query += " WHERE "
		for i, w := range wheres {
			if i > 0 {
				query += " AND "
			}
			query += w
		}
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT %d", limit)

	rows, err := r.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.SDKBuild, 0)
	for rows.Next() {
		b := domain.SDKBuild{}
		var target, status string
		var finishedAt *time.Time
		if err := rows.Scan(&b.ID, &b.TenantID, &b.OntologyVersion, &target, &status, &b.ArtifactURI,
			&b.ErrorMessage, &b.RequestedBy, &b.CreatedAt, &finishedAt); err != nil {
			return nil, err
		}
		b.Target = domain.Target(target)
		b.Status = domain.Status(status)
		b.FinishedAt = finishedAt
		out = append(out, b)
	}
	return out, rows.Err()
}

// FinishBuild marks the build done. status is "succeeded" or "failed".
func (r *Repo) FinishBuild(ctx context.Context, id uuid.UUID, status domain.Status, artifactURI, errMsg string) error {
	now := time.Now().UTC()
	_, err := r.Pool.Exec(ctx, `
		UPDATE sdk_builds
		SET status = $2, artifact_uri = $3, error_message = $4, finished_at = $5
		WHERE id = $1
	`, id, string(status), artifactURI, errMsg, now)
	return err
}

func jsonOrEmpty(in []string) ([]byte, error) {
	if len(in) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(in)
}
