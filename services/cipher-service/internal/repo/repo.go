// Package repo is the pgx-backed persistence layer for the cipher
// service registry. Two tables, defined in
// migrations/20260517000001_initial_cipher_keys.sql:
//
//   - cipher_keys          — one row per registered key.
//   - cipher_key_versions  — one row per wrapping; multiple rows per
//                            key as rotations accumulate.
//
// Repo deliberately does NOT touch crypto, KMS, or the HTTP wire.
// It speaks domain.CipherKey / domain.CipherKeyVersion and surfaces
// the documented sentinels for handler-level mapping.
package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies every embedded migration in lex order. Same shape
// as identity-federation-service: idempotent SQL guarded by IF NOT
// EXISTS so reruns are cheap.
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

// Repo wraps the pgx pool. Methods are safe for concurrent use; the
// pool handles connection multiplexing.
type Repo struct {
	Pool *pgxpool.Pool
}

// New returns a Repo bound to `pool`. Kept as a constructor so future
// instrumentation (slog logger, metrics) can land without touching
// the call sites.
func New(pool *pgxpool.Pool) *Repo { return &Repo{Pool: pool} }

// CreateKeyParams is the payload InsertKey expects. The plaintext DEK
// has already been wrapped by the KMS layer when this struct is built.
type CreateKeyParams struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Alias              string
	Algorithm          domain.Algorithm
	WrappedKeyMaterial []byte
	KMSKeyRef          string
}

// ListPage captures cursor pagination. Empty cursor reads from the
// most recently created key; the cursor returned by ListKeys is the
// created_at timestamp of the last row plus its id, opaque to the
// caller.
type ListPage struct {
	Limit  uint32
	Cursor string
}

// ListResult bundles a single page of keys with a continuation
// cursor. NextCursor is empty when there is no further data.
type ListResult struct {
	Items      []*domain.CipherKey
	NextCursor string
}

// InsertKey persists a fresh key + its v1 version inside one tx. The
// (tenant_id, alias) uniqueness constraint surfaces as a typed error
// the handler maps to 409.
func (r *Repo) InsertKey(ctx context.Context, p CreateKeyParams) (*domain.CipherKey, error) {
	if !p.Algorithm.Valid() {
		return nil, fmt.Errorf("repo: invalid algorithm %q", p.Algorithm)
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	now := time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`INSERT INTO cipher_keys (id, tenant_id, alias, algorithm, version, status, created_at)
		 VALUES ($1, $2, $3, $4, 1, $5, $6)`,
		p.ID, p.TenantID, p.Alias, string(p.Algorithm), string(domain.StatusActive), now,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAliasConflict
		}
		return nil, fmt.Errorf("insert key: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO cipher_key_versions
		     (key_id, version, wrapped_key_material, kms_key_ref, created_at, activated_at)
		 VALUES ($1, 1, $2, $3, $4, $4)`,
		p.ID, p.WrappedKeyMaterial, p.KMSKeyRef, now,
	); err != nil {
		return nil, fmt.Errorf("insert v1: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &domain.CipherKey{
		ID:        p.ID,
		TenantID:  p.TenantID,
		Alias:     p.Alias,
		Algorithm: p.Algorithm,
		Version:   1,
		Status:    domain.StatusActive,
		CreatedAt: now,
	}, nil
}

// GetKey returns the key row keyed by (tenant_id, id). Cross-tenant
// callers see ErrKeyNotFound — the same response a deletion would
// produce — to avoid leaking existence across tenant boundaries.
func (r *Repo) GetKey(ctx context.Context, tenantID, id uuid.UUID) (*domain.CipherKey, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT id, tenant_id, alias, algorithm, version, status, created_at, rotated_at
		 FROM cipher_keys WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	k := &domain.CipherKey{}
	var alg, status string
	if err := row.Scan(&k.ID, &k.TenantID, &k.Alias, &alg, &k.Version, &status, &k.CreatedAt, &k.RotatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrKeyNotFound
		}
		return nil, fmt.Errorf("get key: %w", err)
	}
	k.Algorithm = domain.Algorithm(alg)
	k.Status = domain.Status(status)
	return k, nil
}

// ListKeys returns up to page.Limit keys for a tenant. Cursor encodes
// the last row's (created_at, id) so a stable sort survives same-instant
// inserts.
func (r *Repo) ListKeys(ctx context.Context, tenantID uuid.UUID, page ListPage) (ListResult, error) {
	limit := page.Limit
	if limit == 0 || limit > 200 {
		limit = 50
	}

	cursorTS, cursorID, hasCursor := decodeCursor(page.Cursor)

	q := `SELECT id, tenant_id, alias, algorithm, version, status, created_at, rotated_at
	      FROM cipher_keys WHERE tenant_id = $1`
	args := []any{tenantID}
	if hasCursor {
		q += ` AND (created_at, id) < ($2, $3)`
		args = append(args, cursorTS, cursorID)
	}
	q += ` ORDER BY created_at DESC, id DESC LIMIT ` + fmt.Sprintf("%d", limit+1)

	rows, err := r.Pool.Query(ctx, q, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list keys: %w", err)
	}
	defer rows.Close()

	out := make([]*domain.CipherKey, 0, limit)
	for rows.Next() {
		k := &domain.CipherKey{}
		var alg, status string
		if err := rows.Scan(&k.ID, &k.TenantID, &k.Alias, &alg, &k.Version, &status, &k.CreatedAt, &k.RotatedAt); err != nil {
			return ListResult{}, fmt.Errorf("scan: %w", err)
		}
		k.Algorithm = domain.Algorithm(alg)
		k.Status = domain.Status(status)
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}

	res := ListResult{Items: out}
	if uint32(len(out)) > limit {
		// We over-fetched by one to detect more rows; trim and
		// publish a continuation cursor pointing at the last
		// element actually returned.
		res.Items = out[:limit]
		last := res.Items[limit-1]
		res.NextCursor = encodeCursor(last.CreatedAt, last.ID)
	}
	return res, nil
}

// GetActiveVersion returns the version row for the key's currently
// active wrapping (matches cipher_keys.version).
func (r *Repo) GetActiveVersion(ctx context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKeyVersion, error) {
	k, err := r.GetKey(ctx, tenantID, keyID)
	if err != nil {
		return nil, err
	}
	return r.GetVersion(ctx, tenantID, keyID, k.Version)
}

// GetVersion fetches one historical version. tenantID is enforced via
// a JOIN against cipher_keys so a cross-tenant id never returns a row.
func (r *Repo) GetVersion(ctx context.Context, tenantID, keyID uuid.UUID, version uint32) (*domain.CipherKeyVersion, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT v.key_id, v.version, v.wrapped_key_material, v.kms_key_ref,
		        v.created_at, v.activated_at, v.retired_at
		 FROM cipher_key_versions v
		 JOIN cipher_keys k ON k.id = v.key_id
		 WHERE v.key_id = $1 AND v.version = $2 AND k.tenant_id = $3`,
		keyID, version, tenantID,
	)
	v := &domain.CipherKeyVersion{}
	if err := row.Scan(&v.KeyID, &v.Version, &v.WrappedKeyMaterial, &v.KMSKeyRef,
		&v.CreatedAt, &v.ActivatedAt, &v.RetiredAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrKeyVersionNotFound
		}
		return nil, fmt.Errorf("get version: %w", err)
	}
	return v, nil
}

// RotateKey appends a new version row, marks the previous active row
// retired (only for version metadata — the key itself stays active),
// and bumps cipher_keys.version + status. Returns the new active
// version.
func (r *Repo) RotateKey(ctx context.Context, tenantID, keyID uuid.UUID, wrappedDEK []byte, kmsRef string) (*domain.CipherKey, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current uint32
	var status string
	row := tx.QueryRow(ctx,
		`SELECT version, status FROM cipher_keys
		 WHERE id = $1 AND tenant_id = $2 FOR UPDATE`,
		keyID, tenantID,
	)
	if err := row.Scan(&current, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrKeyNotFound
		}
		return nil, fmt.Errorf("lock key: %w", err)
	}
	if domain.Status(status) == domain.StatusRetired {
		return nil, domain.ErrKeyRetired
	}

	now := time.Now().UTC()
	next := current + 1

	if _, err := tx.Exec(ctx,
		`UPDATE cipher_key_versions SET retired_at = $3
		 WHERE key_id = $1 AND version = $2 AND retired_at IS NULL`,
		keyID, current, now,
	); err != nil {
		return nil, fmt.Errorf("retire prev version: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO cipher_key_versions
		     (key_id, version, wrapped_key_material, kms_key_ref, created_at, activated_at)
		 VALUES ($1, $2, $3, $4, $5, $5)`,
		keyID, next, wrappedDEK, kmsRef, now,
	); err != nil {
		return nil, fmt.Errorf("insert new version: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE cipher_keys SET version = $2, status = $3, rotated_at = $4
		 WHERE id = $1`,
		keyID, next, string(domain.StatusRotating), now,
	); err != nil {
		return nil, fmt.Errorf("bump key version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return r.GetKey(ctx, tenantID, keyID)
}

// RetireKey flips the status to retired. Decrypt continues to work
// (CanDecrypt stays true); encrypts are rejected at the handler layer.
// Idempotent: retiring an already-retired key returns the current row.
func (r *Repo) RetireKey(ctx context.Context, tenantID, keyID uuid.UUID) (*domain.CipherKey, error) {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE cipher_keys SET status = $3
		 WHERE id = $1 AND tenant_id = $2 AND status != $3`,
		keyID, tenantID, string(domain.StatusRetired),
	)
	if err != nil {
		return nil, fmt.Errorf("retire key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Either already retired or never existed — disambiguate
		// with a follow-up read.
		if existing, getErr := r.GetKey(ctx, tenantID, keyID); getErr == nil {
			return existing, nil
		}
		return nil, domain.ErrKeyNotFound
	}
	return r.GetKey(ctx, tenantID, keyID)
}

// MarkRotationComplete clears the StatusRotating state once any async
// rewrap work has caught up. Milestone A returns to "active" inline
// after RotateKey — exposed here so a future background worker
// (CIP.16) can drive the transition independently.
func (r *Repo) MarkRotationComplete(ctx context.Context, tenantID, keyID uuid.UUID) error {
	tag, err := r.Pool.Exec(ctx,
		`UPDATE cipher_keys SET status = $3
		 WHERE id = $1 AND tenant_id = $2 AND status = $4`,
		keyID, tenantID, string(domain.StatusActive), string(domain.StatusRotating),
	)
	if err != nil {
		return fmt.Errorf("mark rotation complete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Key is either not rotating or not found. Either is a
		// no-op from the caller's perspective.
		return nil
	}
	return nil
}

// ErrAliasConflict signals a (tenant, alias) pair is already taken.
var ErrAliasConflict = errors.New("cipher repo: alias already in use for this tenant")

// isUniqueViolation matches pg SQLSTATE 23505. Kept as a local
// helper so the repo doesn't pull pgconn into every consumer.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps pgerrcode.UniqueViolation as a *pgconn.PgError; we
	// match on the textual SQLSTATE to avoid importing pgconn here.
	return strings.Contains(err.Error(), "SQLSTATE 23505")
}

// encodeCursor / decodeCursor produce opaque continuation tokens. The
// wire format is "<unix-nano>:<uuid>"; callers must not parse it.
func encodeCursor(t time.Time, id uuid.UUID) string {
	return fmt.Sprintf("%d:%s", t.UTC().UnixNano(), id.String())
}

func decodeCursor(raw string) (time.Time, uuid.UUID, bool) {
	if raw == "" {
		return time.Time{}, uuid.Nil, false
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, false
	}
	var nanos int64
	if _, err := fmt.Sscanf(parts[0], "%d", &nanos); err != nil {
		return time.Time{}, uuid.Nil, false
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, false
	}
	return time.Unix(0, nanos).UTC(), id, true
}
