package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

const branchSelectColumns = `media_set_rid, branch_name, branch_rid,
	parent_branch_rid, head_transaction_rid, created_at, created_by`

// RequireBranch returns a branch by composite key. (nil, nil) when
// absent so the service layer can map to ErrBranchNotFound.
func (r *Repo) RequireBranch(ctx context.Context, mediaSetRID, branchName string) (*models.MediaSetBranch, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+branchSelectColumns+`
		   FROM media_set_branches
		  WHERE media_set_rid = $1 AND branch_name = $2`,
		mediaSetRID, branchName,
	)
	v, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// LockBranch returns the branch row for update. Used by CreateBranch
// to ensure a concurrent fork from the same parent serialises on the
// parent's row.
func (r *Repo) LockBranch(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName string) (*models.MediaSetBranch, error) {
	row := tx.QueryRow(ctx,
		`SELECT `+branchSelectColumns+`
		   FROM media_set_branches
		  WHERE media_set_rid = $1 AND branch_name = $2
		  FOR UPDATE`,
		mediaSetRID, branchName,
	)
	v, err := scanBranch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// ListBranches returns every branch on the media set, with `main`
// pinned first (matches the Rust order).
func (r *Repo) ListBranches(ctx context.Context, mediaSetRID string) ([]models.MediaSetBranch, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+branchSelectColumns+`
		   FROM media_set_branches
		  WHERE media_set_rid = $1
		  ORDER BY (branch_name = 'main') DESC, branch_name ASC`,
		mediaSetRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MediaSetBranch, 0)
	for rows.Next() {
		v, err := scanBranch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// CreateBranchParams captures the columns the INSERT writes.
type CreateBranchParams struct {
	MediaSetRID        string
	BranchName         string
	ParentBranchRID    *string
	HeadTransactionRID *string
	CreatedBy          string
}

// CreateBranch inserts the row inside the caller's transaction.
// Returns *ErrBranchExists when (media_set_rid, branch_name) is already
// taken so the handler maps to 400. The parent branch + optional
// from_transaction validation lives in the service layer.
func (r *Repo) CreateBranch(ctx context.Context, tx pgx.Tx, p CreateBranchParams) (*models.MediaSetBranch, error) {
	row := tx.QueryRow(ctx,
		`INSERT INTO media_set_branches
		    (media_set_rid, branch_name, parent_branch_rid, head_transaction_rid, created_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING `+branchSelectColumns,
		p.MediaSetRID, p.BranchName, p.ParentBranchRID, p.HeadTransactionRID, p.CreatedBy,
	)
	v, err := scanBranch(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, &ErrBranchExists{Name: p.BranchName, MediaSetRID: p.MediaSetRID}
		}
		return nil, err
	}
	return v, nil
}

// ReparentChildren re-points every child branch under `oldParentRID`
// to `newParentRID`. Used by DeleteBranch to honour the Foundry
// "child branches are re-parented under the deleted branch's parent"
// guarantee.
func (r *Repo) ReparentChildren(ctx context.Context, tx pgx.Tx, mediaSetRID string, oldParentRID string, newParentRID *string) error {
	_, err := tx.Exec(ctx,
		`UPDATE media_set_branches
		    SET parent_branch_rid = $1
		  WHERE media_set_rid = $2 AND parent_branch_rid = $3`,
		newParentRID, mediaSetRID, oldParentRID,
	)
	return err
}

// SoftDeleteItemsOnBranch flips deleted_at for every live item on
// `branchRID`. Returns the count of rows affected.
func (r *Repo) SoftDeleteItemsOnBranch(ctx context.Context, tx pgx.Tx, branchRID string) (int64, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE media_items SET deleted_at = NOW()
		  WHERE branch_rid = $1 AND deleted_at IS NULL`,
		branchRID,
	)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteBranchRow removes the branch row. Items are soft-deleted by
// SoftDeleteItemsOnBranch first; child branches are re-parented by
// ReparentChildren first.
func (r *Repo) DeleteBranchRow(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName string) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM media_set_branches WHERE media_set_rid = $1 AND branch_name = $2`,
		mediaSetRID, branchName,
	)
	return err
}

// RewindBranchHead clears the head_transaction_rid pointer and returns
// the post-update row. Mirrors the Rust `reset_branch_op` SQL.
func (r *Repo) RewindBranchHead(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName string) (*models.MediaSetBranch, error) {
	row := tx.QueryRow(ctx,
		`UPDATE media_set_branches
		    SET head_transaction_rid = NULL
		  WHERE media_set_rid = $1 AND branch_name = $2
		 RETURNING `+branchSelectColumns,
		mediaSetRID, branchName,
	)
	return scanBranch(row)
}

// AdvanceBranchHead points head_transaction_rid at txRID. Called by
// transactions.Service on COMMIT.
func (r *Repo) AdvanceBranchHead(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName, txRID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE media_set_branches
		    SET head_transaction_rid = $1
		  WHERE media_set_rid = $2 AND branch_name = $3`,
		txRID, mediaSetRID, branchName,
	)
	return err
}

// MergeSourceItem is one row read from the source branch when merging.
type MergeSourceItem struct {
	Path       string
	MimeType   string
	SHA256     string
	SizeBytes  int64
	StorageURI string
	Metadata   []byte
	SourceRID  string
	Markings   []string
}

// ListMergeSourceItems returns every live item on the source branch,
// in insertion order. Matches the Rust query verbatim.
func (r *Repo) ListMergeSourceItems(ctx context.Context, tx pgx.Tx, branchRID string) ([]MergeSourceItem, error) {
	rows, err := tx.Query(ctx,
		`SELECT path, mime_type, sha256, size_bytes, storage_uri, metadata, rid, markings
		   FROM media_items
		  WHERE branch_rid = $1 AND deleted_at IS NULL
		  ORDER BY created_at ASC`,
		branchRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MergeSourceItem, 0)
	for rows.Next() {
		v := MergeSourceItem{}
		if err := rows.Scan(&v.Path, &v.MimeType, &v.SHA256, &v.SizeBytes, &v.StorageURI, &v.Metadata, &v.SourceRID, &v.Markings); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LiveTargetPaths returns the set of paths currently live on the
// target branch. Used to compute the conflict surface during merge.
func (r *Repo) LiveTargetPaths(ctx context.Context, tx pgx.Tx, branchRID string) (map[string]struct{}, error) {
	rows, err := tx.Query(ctx,
		`SELECT path FROM media_items WHERE branch_rid = $1 AND deleted_at IS NULL`,
		branchRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = struct{}{}
	}
	return out, rows.Err()
}

// SoftDeleteAtPath flips deleted_at for the live row at
// (branch_rid, path), if any. Used by the merge resolver before
// inserting the source item so the partial unique index does not
// fire mid-tx.
func (r *Repo) SoftDeleteAtPath(ctx context.Context, tx pgx.Tx, branchRID, path string) error {
	_, err := tx.Exec(ctx,
		`UPDATE media_items SET deleted_at = NOW()
		  WHERE branch_rid = $1 AND path = $2 AND deleted_at IS NULL`,
		branchRID, path,
	)
	return err
}

// InsertMergedItem inserts a fresh row that mirrors a source row
// onto the target branch. Mirrors the Rust `insert_merged_item`.
//
// Returns isUniqueViolation=true when the partial path unique index
// trips so the service layer can count it as "skipped".
func (r *Repo) InsertMergedItem(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName, branchRID string, src MergeSourceItem) (skipped bool, err error) {
	newRID := MediaItemRIDPrefix + uuid.New().String()
	if len(src.Metadata) == 0 {
		src.Metadata = []byte("{}")
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO media_items
		    (rid, media_set_rid, branch, branch_rid, path, mime_type,
		     size_bytes, sha256, metadata, storage_uri, deduplicated_from,
		     retention_seconds, markings)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		         COALESCE((SELECT retention_seconds FROM media_sets WHERE rid = $2), 0),
		         $12)`,
		newRID, mediaSetRID, branchName, branchRID,
		src.Path, src.MimeType, src.SizeBytes, src.SHA256,
		src.Metadata, src.StorageURI, src.SourceRID, src.Markings,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func scanBranch(r rowLikeT) (*models.MediaSetBranch, error) {
	v := &models.MediaSetBranch{}
	var parent, head *string
	if err := r.Scan(
		&v.MediaSetRID, &v.BranchName, &v.BranchRID,
		&parent, &head, &v.CreatedAt, &v.CreatedBy,
	); err != nil {
		return nil, err
	}
	v.ParentBranchRID = parent
	v.HeadTransactionRID = head
	return v, nil
}

// ── Errors ────────────────────────────────────────────────────────

// ErrBranchExists is returned by CreateBranch when (media_set_rid,
// branch_name) is already taken.
type ErrBranchExists struct {
	Name        string
	MediaSetRID string
}

func (e *ErrBranchExists) Error() string {
	return fmt.Sprintf("branch `%s` already exists on media set `%s`", e.Name, e.MediaSetRID)
}
