package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

// MintAccessPatternID generates the canonical RID for a new pattern.
// Mirrors the Rust `format!("ri.foundry.main.access_pattern.{}", Uuid::now_v7())`.
func MintAccessPatternID() string {
	return "ri.foundry.main.access_pattern." + uuid.New().String()
}

// BeginTx starts a pgx transaction. Exposed so the service layer can
// keep the ledger insert + audit emission inside the same atomic
// commit (ADR-0022).
func (r *Repo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.Pool.Begin(ctx)
}

// CreateAccessPatternParams captures the columns the INSERT writes.
// Pulled out so the audit-emitting service layer can build the row
// without depending on pgx types.
type CreateAccessPatternParams struct {
	MediaSetRID string
	Kind        string
	Params      []byte
	Persistence string
	TTLSeconds  int64
	CreatedBy   string
}

// CreateAccessPattern inserts the row and returns it. Surfaces a
// typed ErrDuplicateKind for unique-violation on (media_set_rid, kind)
// so the handler returns 400 instead of 500.
func (r *Repo) CreateAccessPattern(ctx context.Context, p CreateAccessPatternParams) (*models.AccessPattern, error) {
	id := MintAccessPatternID()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO media_set_access_patterns
		    (id, media_set_rid, kind, params, persistence, ttl_seconds, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by`,
		id, p.MediaSetRID, p.Kind, p.Params, p.Persistence, p.TTLSeconds, p.CreatedBy,
	)
	out, err := scanAccessPattern(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, &ErrDuplicateKind{Kind: p.Kind, MediaSetRID: p.MediaSetRID}
		}
		return nil, err
	}
	return out, nil
}

// ListAccessPatterns returns every pattern registered on the media set,
// newest first.
func (r *Repo) ListAccessPatterns(ctx context.Context, mediaSetRID string) ([]models.AccessPattern, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by
		   FROM media_set_access_patterns
		  WHERE media_set_rid = $1
		  ORDER BY created_at DESC`,
		mediaSetRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.AccessPattern, 0)
	for rows.Next() {
		v, err := scanAccessPattern(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// GetAccessPattern returns one row by id. (nil, nil) if absent.
func (r *Repo) GetAccessPattern(ctx context.Context, id string) (*models.AccessPattern, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by
		   FROM media_set_access_patterns
		  WHERE id = $1`,
		id,
	)
	out, err := scanAccessPattern(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// GetAccessPatternByKind backs the per-item shortcut endpoint
// `GET /items/{rid}/access-patterns/{kind}/url`.
func (r *Repo) GetAccessPatternByKind(ctx context.Context, mediaSetRID, kind string) (*models.AccessPattern, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, media_set_rid, kind, params, persistence, ttl_seconds, created_at, created_by
		   FROM media_set_access_patterns
		  WHERE media_set_rid = $1 AND kind = $2`,
		mediaSetRID, kind,
	)
	out, err := scanAccessPattern(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// GetMediaItem returns a live media-item row (deleted_at IS NULL).
// Used by the access-pattern flow + every read-side handler. The
// DELETE handler uses GetMediaItemFull which sees soft-deleted rows.
func (r *Repo) GetMediaItem(ctx context.Context, rid string) (*models.MediaItem, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+itemSelectColumns+`
		   FROM media_items
		  WHERE rid = $1 AND deleted_at IS NULL`,
		rid,
	)
	v, err := scanMediaItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// CachedOutput is a row from media_set_access_pattern_outputs.
type CachedOutput struct {
	StorageURI string
	OutputMime string
	ExpiresAt  *time.Time
}

// LookupCachedOutput returns the cached derived artifact for
// (pattern, item, params_hash), if present and not expired.
func (r *Repo) LookupCachedOutput(ctx context.Context, patternID, itemRID, paramsHash string) (*CachedOutput, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT storage_uri, output_mime, expires_at
		   FROM media_set_access_pattern_outputs
		  WHERE pattern_id = $1 AND item_rid = $2 AND params_hash = $3
		    AND (expires_at IS NULL OR expires_at > NOW())`,
		patternID, itemRID, paramsHash,
	)
	out := &CachedOutput{}
	if err := row.Scan(&out.StorageURI, &out.OutputMime, &out.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

// WriteCacheRow upserts a derived-artifact row keyed by
// (pattern, item, params_hash).
func (r *Repo) WriteCacheRow(ctx context.Context, patternID, itemRID, paramsHash, storageURI, outputMime string, bytes int64, expiresAt *time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`INSERT INTO media_set_access_pattern_outputs
		    (pattern_id, item_rid, params_hash, storage_uri, output_mime, bytes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (pattern_id, item_rid, params_hash) DO UPDATE SET
		    storage_uri = EXCLUDED.storage_uri,
		    output_mime = EXCLUDED.output_mime,
		    bytes       = EXCLUDED.bytes,
		    expires_at  = EXCLUDED.expires_at,
		    created_at  = NOW()`,
		patternID, itemRID, paramsHash, storageURI, outputMime, bytes, expiresAt,
	)
	return err
}

// LedgerEntry mirrors the columns inserted into
// media_set_access_pattern_invocations.
type LedgerEntry struct {
	MediaSetRID    string
	PatternID      string
	Kind           string
	ItemRID        string
	InputBytes     int64
	ComputeSeconds int64
	Persistence    string
	CacheHit       bool
	InvokedBy      string
}

// InsertInvocation appends one row to the ledger inside the caller's
// transaction. The audit envelope is enqueued in the same tx by the
// service layer (ADR-0022 atomicity).
func (r *Repo) InsertInvocation(ctx context.Context, tx pgx.Tx, e LedgerEntry) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO media_set_access_pattern_invocations
		    (media_set_rid, pattern_id, kind, item_rid, input_bytes,
		     compute_seconds, persistence, cache_hit, invoked_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.MediaSetRID, e.PatternID, e.Kind, e.ItemRID, e.InputBytes,
		e.ComputeSeconds, e.Persistence, e.CacheHit, e.InvokedBy,
	)
	return err
}

// ParamsHash is the SHA-256 of the canonical-form params JSON. Used
// to distinguish cache entries for the same pattern with different
// runtime params (e.g. `resize 64×64` vs `resize 128×128`). Mirrors
// the Rust `params_hash` helper in handlers/access_patterns.rs.
func ParamsHash(params json.RawMessage) string {
	if len(params) == 0 {
		return zeroHash
	}
	// json.Compact produces a deterministic byte representation by
	// stripping insignificant whitespace; sorted-key normalisation is
	// not strictly needed because canonical input is the registration
	// row's stored JSONB (Postgres normalises object key order on
	// JSONB ingest).
	var buf bytes.Buffer
	if err := json.Compact(&buf, params); err != nil {
		return zeroHash
	}
	digest := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(digest[:])
}

const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

// scanAccessPattern reads one access-pattern row from a row-like
// scanner. Reused by Create / Get / List.
func scanAccessPattern(r rowLikeT) (*models.AccessPattern, error) {
	v := &models.AccessPattern{}
	var params []byte
	if err := r.Scan(&v.ID, &v.MediaSetRID, &v.Kind, &params,
		&v.Persistence, &v.TTLSeconds, &v.CreatedAt, &v.CreatedBy); err != nil {
		return nil, err
	}
	v.Params = json.RawMessage(params)
	if len(v.Params) == 0 {
		v.Params = json.RawMessage("{}")
	}
	return v, nil
}

// ErrDuplicateKind is returned when a (media_set_rid, kind) pair is
// already registered. Allows the handler to surface 400 with a stable
// message instead of leaking the pgx error string.
type ErrDuplicateKind struct {
	Kind        string
	MediaSetRID string
}

func (e *ErrDuplicateKind) Error() string {
	return fmt.Sprintf("kind `%s` already registered on media set `%s`", e.Kind, e.MediaSetRID)
}

// isUniqueViolation reports whether err is a Postgres unique_violation
// (SQLSTATE 23505). pgconn surfaces the SQLSTATE in PgError.Code.
func isUniqueViolation(err error) bool {
	type pgErr interface{ SQLState() string }
	var p pgErr
	if errors.As(err, &p) {
		return p.SQLState() == "23505"
	}
	return false
}
