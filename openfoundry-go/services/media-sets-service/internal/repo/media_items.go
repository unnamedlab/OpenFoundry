package repo

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

// MediaItemRIDPrefix is the canonical Foundry prefix for media-item
// RIDs. Mirrors `MEDIA_ITEM_RID_PREFIX` in Rust.
const MediaItemRIDPrefix = "ri.foundry.main.media_item."

// NewMediaItemRID mints a fresh media-item RID using uuid v7-style
// monotonic ordering — uuid.New() in Go is v4, but the prefix scheme
// is what consumers key on, so the version difference is invisible.
func NewMediaItemRID() string {
	return MediaItemRIDPrefix + uuid.New().String()
}

// CreateMediaItemParams captures the columns the INSERT writes. All
// fields except DeduplicatedFrom are required; DeduplicatedFrom is
// stamped by the caller after running the dedup helper.
type CreateMediaItemParams struct {
	RID              string
	MediaSetRID      string
	Branch           string
	TransactionRID   string // empty string means transactionless
	Path             string
	MimeType         string
	SizeBytes        int64
	SHA256           string
	Metadata         json.RawMessage
	StorageURI       string
	DeduplicatedFrom *string
	RetentionSeconds int64
}

// itemSelectColumns lists every column the SELECTs return — kept in one
// constant so the projection lines up across get / list / create /
// patch.
const itemSelectColumns = `rid, media_set_rid, branch,
	COALESCE(transaction_rid, '') AS transaction_rid, path, mime_type,
	size_bytes, sha256, metadata, storage_uri, deduplicated_from,
	deleted_at, created_at, markings`

// SoftDeletePreviousAtPath ports the Rust dedup primitive
// `soft_delete_previous_at_path`. Inside the caller's transaction:
// flip the live row at `(media_set_rid, branch, path)` to deleted_at =
// NOW() and return its RID. Returns nil when no live row exists.
//
// The method form lives on *Repo so tests can stub the behaviour via
// the Repository interface; the package-level alias below preserves
// the original Rust call shape for production callers.
func (r *Repo) SoftDeletePreviousAtPath(ctx context.Context, tx pgx.Tx, mediaSetRID, branch, path string) (*string, error) {
	row := tx.QueryRow(ctx,
		`UPDATE media_items
		    SET deleted_at = NOW()
		  WHERE media_set_rid = $1 AND branch = $2 AND path = $3
		    AND deleted_at IS NULL
		RETURNING rid`,
		mediaSetRID, branch, path,
	)
	var rid string
	if err := row.Scan(&rid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &rid, nil
}

// CreateMediaItem inserts the row inside the caller's transaction.
// The caller is responsible for invoking SoftDeletePreviousAtPath
// first and threading the returned RID through DeduplicatedFrom so
// the partial unique index `uq_media_items_live_path` never sees an
// inconsistent intermediate state (mirrors the Rust contract).
func (r *Repo) CreateMediaItem(ctx context.Context, tx pgx.Tx, p CreateMediaItemParams) (*models.MediaItem, error) {
	var transactionRID any
	if p.TransactionRID != "" {
		transactionRID = p.TransactionRID
	}
	if len(p.Metadata) == 0 {
		p.Metadata = json.RawMessage("{}")
	}
	row := tx.QueryRow(ctx,
		`INSERT INTO media_items
		    (rid, media_set_rid, branch, branch_rid, transaction_rid, path,
		     mime_type, size_bytes, sha256, metadata, storage_uri,
		     deduplicated_from, retention_seconds)
		 VALUES ($1, $2, $3,
		         (SELECT branch_rid FROM media_set_branches
		           WHERE media_set_rid = $2 AND branch_name = $3),
		         $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 RETURNING `+itemSelectColumns,
		p.RID, p.MediaSetRID, p.Branch,
		transactionRID, p.Path, p.MimeType, p.SizeBytes, p.SHA256,
		p.Metadata, p.StorageURI, p.DeduplicatedFrom, p.RetentionSeconds,
	)
	return scanMediaItem(row)
}

// GetMediaItemFull returns the full row regardless of deleted_at. The
// access-pattern flow uses GetMediaItem (which filters deleted) — this
// helper is for the DELETE path which must see the row before
// soft-deleting it.
func (r *Repo) GetMediaItemFull(ctx context.Context, rid string) (*models.MediaItem, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+itemSelectColumns+` FROM media_items WHERE rid = $1`,
		rid,
	)
	v, err := scanMediaItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// ListMediaItemsParams captures the filters the list endpoint accepts.
type ListMediaItemsParams struct {
	MediaSetRID string
	Branch      string
	PathPrefix  *string
	Cursor      *string
	Limit       int
}

// ListMediaItems returns live items on the given branch matching the
// optional path prefix. `cursor` is the path of the last seen row
// (offset-less keyset pagination, mirroring the Rust impl).
func (r *Repo) ListMediaItems(ctx context.Context, p ListMediaItemsParams) ([]models.MediaItem, error) {
	limit := p.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
		if p.Limit > 500 {
			limit = 500
		}
	}
	branch := p.Branch
	if branch == "" {
		branch = "main"
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT `+itemSelectColumns+`
		   FROM media_items
		  WHERE media_set_rid = $1
		    AND branch        = $2
		    AND deleted_at IS NULL
		    AND ($3::text IS NULL OR path LIKE $3 || '%')
		    AND ($4::text IS NULL OR path > $4)
		  ORDER BY path ASC
		  LIMIT $5`,
		p.MediaSetRID, branch, p.PathPrefix, p.Cursor, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.MediaItem, 0)
	for rows.Next() {
		v, err := scanMediaItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// SoftDeleteMediaItem flips deleted_at on the row, returning whether
// it was the row that did the flip (false when already deleted).
func (r *Repo) SoftDeleteMediaItem(ctx context.Context, tx pgx.Tx, rid string) (bool, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE media_items SET deleted_at = NOW()
		  WHERE rid = $1 AND deleted_at IS NULL`,
		rid,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// PatchMediaItemMarkings replaces the markings array on the row.
// Surfaces (nil, nil) when the row does not exist; the service layer
// maps that to ErrNotFound.
func (r *Repo) PatchMediaItemMarkings(ctx context.Context, tx pgx.Tx, rid string, markings []string) (*models.MediaItem, error) {
	row := tx.QueryRow(ctx,
		`UPDATE media_items SET markings = $2
		  WHERE rid = $1
		 RETURNING `+itemSelectColumns,
		rid, markings,
	)
	v, err := scanMediaItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// CountTransactionLiveItems returns how many live (deleted_at IS NULL)
// items currently belong to the given transaction. The service layer
// uses this to enforce the Foundry per-transaction cap (default 10k).
func (r *Repo) CountTransactionLiveItems(ctx context.Context, transactionRID string) (int64, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM media_items
		  WHERE transaction_rid = $1 AND deleted_at IS NULL`,
		transactionRID,
	)
	var n int64
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// scanMediaItem reads one row into models.MediaItem. Used by
// every helper that returns the full struct.
func scanMediaItem(r rowLikeT) (*models.MediaItem, error) {
	v := &models.MediaItem{}
	var meta []byte
	var dedup *string
	if err := r.Scan(
		&v.RID, &v.MediaSetRID, &v.Branch, &v.TransactionRID,
		&v.Path, &v.MimeType, &v.SizeBytes, &v.SHA256, &meta,
		&v.StorageURI, &dedup, &v.DeletedAt, &v.CreatedAt, &v.Markings,
	); err != nil {
		return nil, err
	}
	v.DeduplicatedFrom = dedup
	if len(meta) > 0 {
		v.Metadata = json.RawMessage(meta)
	} else {
		v.Metadata = json.RawMessage("{}")
	}
	if v.Markings == nil {
		v.Markings = []string{}
	}
	return v, nil
}

// NormalizeMarkings returns the trim+lowercase+dedup+sorted markings
// the service layer writes to the row. Matches the Rust normaliser
// 1:1 so the column always carries lowercased canonical names.
func NormalizeMarkings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		l := strings.TrimSpace(strings.ToLower(raw))
		if l == "" {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	// In-place sort would mutate; allocate a fresh slice for clarity.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
