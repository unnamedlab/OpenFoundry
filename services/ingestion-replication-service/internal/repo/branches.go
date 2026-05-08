package repo

// IRF-8 — Stream branches CRUD. Mirrors
// services/ingestion-replication-service/src/event_streaming/handlers/branches.rs
// at the data-layer level.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain/streambranch"
)

const branchSelect = `SELECT id, stream_id, name, parent_branch_id, status,
		head_sequence_no, dataset_branch_id, description, created_by,
		created_at, archived_at
	FROM streaming_stream_branches`

// ListBranches returns every branch owned by streamID, ordered by
// creation time ascending — same ordering as the Rust handler.
func (r *Repo) ListBranches(ctx context.Context, streamID uuid.UUID) ([]streambranch.StreamBranch, error) {
	rows, err := r.Pool.Query(ctx, branchSelect+`
		WHERE stream_id = $1
		ORDER BY created_at ASC`, streamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]streambranch.StreamBranch, 0)
	for rows.Next() {
		v, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// GetBranchByName returns nil when the branch does not exist.
func (r *Repo) GetBranchByName(ctx context.Context, streamID uuid.UUID, name string) (*streambranch.StreamBranch, error) {
	row := r.Pool.QueryRow(ctx, branchSelect+`
		WHERE stream_id = $1 AND name = $2`, streamID, name)
	v, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// CreateBranch inserts a fresh branch row for streamID. parent (when
// non-nil) MUST already belong to the same stream — the handler checks
// this before calling. The function does not validate the name; callers
// must enforce the alphanumeric/'-/_' charset before calling.
func (r *Repo) CreateBranch(
	ctx context.Context,
	streamID uuid.UUID,
	name, createdBy string,
	parent *uuid.UUID,
	datasetBranchID *string,
	description string,
) (*streambranch.StreamBranch, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO streaming_stream_branches (
			id, stream_id, name, parent_branch_id, status, head_sequence_no,
			dataset_branch_id, description, created_by
		) VALUES ($1, $2, $3, $4, 'active', 0, $5, $6, $7)
		RETURNING id, stream_id, name, parent_branch_id, status,
		         head_sequence_no, dataset_branch_id, description,
		         created_by, created_at, archived_at`,
		id, streamID, name, parent, datasetBranchID, description, createdBy,
	)
	return scanBranch(row)
}

// DeleteBranch hard-deletes a branch row. Caller is responsible for
// guarding "main" branch + non-empty/active branches.
func (r *Repo) DeleteBranch(ctx context.Context, branchID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx,
		`DELETE FROM streaming_stream_branches WHERE id = $1`, branchID)
	return err
}

// MergeBranches advances the target's head_sequence_no and flips the
// source row to status='merged' inside a single transaction.
func (r *Repo) MergeBranches(ctx context.Context, sourceID, targetID uuid.UUID, mergedSequenceNo int64) error {
	tx, err := r.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE streaming_stream_branches
		    SET head_sequence_no = $2
		  WHERE id = $1`, targetID, mergedSequenceNo); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE streaming_stream_branches
		    SET status = 'merged'
		  WHERE id = $1`, sourceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ArchiveBranch flips the branch to status='archived', stamps
// archived_at, and returns the post-update row. branchID + name are
// both supplied so we can re-load by name after the update commits
// (mirrors the Rust handler that re-selects via load_branch_by_name_tx).
func (r *Repo) ArchiveBranch(ctx context.Context, streamID, branchID uuid.UUID, name string) (*streambranch.StreamBranch, error) {
	row := r.Pool.QueryRow(ctx,
		`UPDATE streaming_stream_branches
		    SET status = 'archived',
		        archived_at = now()
		  WHERE id = $1
		  RETURNING id, stream_id, name, parent_branch_id, status,
		           head_sequence_no, dataset_branch_id, description,
		           created_by, created_at, archived_at`, branchID)
	v, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func scanBranch(r rowLikeT) (*streambranch.StreamBranch, error) {
	v := &streambranch.StreamBranch{}
	if err := r.Scan(
		&v.ID, &v.StreamID, &v.Name, &v.ParentBranchID, &v.Status,
		&v.HeadSequenceNo, &v.DatasetBranchID, &v.Description,
		&v.CreatedBy, &v.CreatedAt, &v.ArchivedAt,
	); err != nil {
		return nil, err
	}
	return v, nil
}

// ParentBranchBelongsTo reports whether the candidate parent UUID is
// actually a branch of streamID. Used by the create handler to gate
// payload.parent_branch_id.
func (r *Repo) ParentBranchBelongsTo(ctx context.Context, parentID, streamID uuid.UUID) (bool, error) {
	var ok bool
	err := r.Pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM streaming_stream_branches
			 WHERE id = $1 AND stream_id = $2)`,
		parentID, streamID,
	).Scan(&ok)
	return ok, err
}
