package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/model-catalog-service/internal/models"
)

type LifecycleRepo struct {
	Pool *pgxpool.Pool
}

const submissionCols = `id, model_id, version, stage, status, objective_id, release_notes, created_at, updated_at`

func scanSubmission(s scanner) (models.ModelSubmission, error) {
	var m models.ModelSubmission
	err := s.Scan(&m.ID, &m.ModelID, &m.Version, &m.Stage, &m.Status,
		&m.ObjectiveID, &m.ReleaseNotes, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (r *LifecycleRepo) ListSubmissions(ctx context.Context) ([]models.ModelSubmission, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+submissionCols+` FROM model_submissions ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ModelSubmission, 0)
	for rows.Next() {
		m, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *LifecycleRepo) CreateSubmission(ctx context.Context, body models.CreateSubmissionRequest) (models.ModelSubmission, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO model_submissions
                (id, model_id, version, stage, status, objective_id, release_notes)
            VALUES ($1, $2, $3, 'submitted', 'pending', $4, $5)
            RETURNING `+submissionCols,
		uuid.New(), body.ModelID, body.Version, body.ObjectiveID, body.ReleaseNotes)
	return scanSubmission(row)
}

func (r *LifecycleRepo) GetSubmission(ctx context.Context, id uuid.UUID) (*models.ModelSubmission, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+submissionCols+` FROM model_submissions WHERE id = $1`, id)
	m, err := scanSubmission(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *LifecycleRepo) TransitionSubmission(ctx context.Context, id uuid.UUID, body models.TransitionRequest) (*models.ModelSubmission, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE model_submissions
            SET stage = $2, status = $3, updated_at = NOW()
          WHERE id = $1
          RETURNING `+submissionCols,
		id, body.Stage, body.Status)
	m, err := scanSubmission(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Best-effort lifecycle event log; ignore errors so the
	// transition response still returns 200 even if the audit row
	// fails to insert (matches Rust's `let _ = ...`).
	_, _ = r.Pool.Exec(ctx,
		`INSERT INTO model_lifecycle_events (id, submission_id, stage, status, note) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New(), id, body.Stage, body.Status, body.Note)
	return &m, nil
}

const objectiveCols = `id, slug, name, description, success_criteria, created_at`

func scanObjective(s scanner) (models.ModelingObjective, error) {
	var o models.ModelingObjective
	err := s.Scan(&o.ID, &o.Slug, &o.Name, &o.Description, &o.SuccessCriteria, &o.CreatedAt)
	return o, err
}

func (r *LifecycleRepo) ListObjectives(ctx context.Context) ([]models.ModelingObjective, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+objectiveCols+` FROM modeling_objectives ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ModelingObjective, 0)
	for rows.Next() {
		o, err := scanObjective(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *LifecycleRepo) CreateObjective(ctx context.Context, body models.CreateObjectiveRequest) (models.ModelingObjective, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO modeling_objectives (id, slug, name, description, success_criteria)
           VALUES ($1, $2, $3, $4, $5) RETURNING `+objectiveCols,
		uuid.New(), body.Slug, body.Name, body.Description, body.SuccessCriteria)
	return scanObjective(row)
}
