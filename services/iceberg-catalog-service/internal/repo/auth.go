package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain/markings"
	tokendomain "github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain/token"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers/auth"
)

// IssueAPIToken inserts a new long-lived API token tied to userID and
// returns the freshly minted *plaintext* secret alongside the persisted
// row. The plaintext is shown to the caller exactly once; the catalog
// only stores the SHA-256 hash + a 4-character UI hint.
func (r *Repo) IssueAPIToken(ctx context.Context, userID uuid.UUID, name string, scopes []string, expiresAt *time.Time) (*tokendomain.APIToken, string, error) {
	raw, hash, hint, err := tokendomain.Mint()
	if err != nil {
		return nil, "", fmt.Errorf("mint token: %w", err)
	}
	id := uuid.New()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO iceberg_api_tokens (id, user_id, name, token_hash, token_hint, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, user_id, name, token_hint, scopes, expires_at, created_at, last_used_at, revoked_at`,
		id, userID, name, hash, hint, scopes, expiresAt)
	record := &tokendomain.APIToken{}
	if err := row.Scan(&record.ID, &record.UserID, &record.Name, &record.TokenHint,
		&record.Scopes, &record.ExpiresAt, &record.CreatedAt, &record.LastUsedAt, &record.RevokedAt); err != nil {
		return nil, "", err
	}
	return record, raw, nil
}

// ValidateAPIToken hits iceberg_api_tokens by its sha256 hash, checks
// that the row hasn't been revoked or expired, and bumps last_used_at.
// Returns the validator's view of the row (subject + scopes + tenant)
// so the bearer extractor can build a principal without further hops.
func (r *Repo) ValidateAPIToken(ctx context.Context, raw string) (*auth.StoredAPIToken, error) {
	hash := tokendomain.Hash(raw)
	var (
		id     uuid.UUID
		userID uuid.UUID
		scopes []string
	)
	row := r.Pool.QueryRow(ctx,
		`SELECT id, user_id, scopes
		 FROM iceberg_api_tokens
		 WHERE token_hash = $1
		   AND revoked_at IS NULL
		   AND (expires_at IS NULL OR expires_at > NOW())`,
		hash)
	if err := row.Scan(&id, &userID, &scopes); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("api token not found or revoked")
		}
		return nil, err
	}
	if _, err := r.Pool.Exec(ctx,
		`UPDATE iceberg_api_tokens SET last_used_at = NOW() WHERE id = $1`, id); err != nil {
		return nil, err
	}
	return &auth.StoredAPIToken{ID: id, UserID: userID, Scopes: scopes}, nil
}

// ResolveMarkingName projects a marking *name* (case-insensitive) to
// the marking_id used in the join tables.
func (r *Repo) ResolveMarkingName(ctx context.Context, name string) (uuid.UUID, error) {
	var id uuid.UUID
	row := r.Pool.QueryRow(ctx,
		`SELECT marking_id FROM iceberg_marking_names WHERE name = LOWER($1)`, name)
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, fmt.Errorf("unknown marking name `%s`", name)
		}
		return uuid.Nil, err
	}
	return id, nil
}

// ProjectMarkings hydrates the supplied id list into the user-facing
// projection shape (id + name + description). Order is canonical so
// responses are deterministic across reads.
func (r *Repo) ProjectMarkings(ctx context.Context, ids []uuid.UUID) ([]markings.MarkingProjection, error) {
	if len(ids) == 0 {
		return []markings.MarkingProjection{}, nil
	}
	rows, err := r.Pool.Query(ctx,
		`SELECT marking_id, name, description
		 FROM iceberg_marking_names
		 WHERE marking_id = ANY($1)
		 ORDER BY name`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]markings.MarkingProjection, 0, len(ids))
	for rows.Next() {
		var p markings.MarkingProjection
		if err := rows.Scan(&p.MarkingID, &p.Name, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LoadNamespaceMarkings projects iceberg_namespace_markings into the
// effective/explicit view. Namespaces don't inherit from anywhere yet
// (sub-namespace inheritance is reserved for D1.1.8 P5) so the two
// columns share the same content.
func (r *Repo) LoadNamespaceMarkings(ctx context.Context, namespaceID uuid.UUID) (*markings.NamespaceMarkings, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT marking_id FROM iceberg_namespace_markings WHERE namespace_id = $1`, namespaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	projections, err := r.ProjectMarkings(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &markings.NamespaceMarkings{
		Effective: projections,
		Explicit:  projections,
	}, nil
}

// LoadTableMarkings reads iceberg_table_markings and splits rows by
// `source` into the effective/explicit/inherited buckets the response
// surfaces.
func (r *Repo) LoadTableMarkings(ctx context.Context, tableID uuid.UUID) (*markings.TableMarkings, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT marking_id, source FROM iceberg_table_markings WHERE table_id = $1`, tableID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var (
		explicit  []uuid.UUID
		inherited []uuid.UUID
	)
	for rows.Next() {
		var (
			id     uuid.UUID
			source string
		)
		if err := rows.Scan(&id, &source); err != nil {
			return nil, err
		}
		switch source {
		case "inherited":
			inherited = append(inherited, id)
		default:
			explicit = append(explicit, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	effective := append([]uuid.UUID{}, explicit...)
	effective = append(effective, inherited...)
	effective = dedupUUIDs(effective)

	effProj, err := r.ProjectMarkings(ctx, effective)
	if err != nil {
		return nil, err
	}
	expProj, err := r.ProjectMarkings(ctx, explicit)
	if err != nil {
		return nil, err
	}
	inhProj, err := r.ProjectMarkings(ctx, inherited)
	if err != nil {
		return nil, err
	}
	return &markings.TableMarkings{
		Effective:              effProj,
		Explicit:               expProj,
		InheritedFromNamespace: inhProj,
	}, nil
}

// SetNamespaceMarkings replaces the namespace's explicit markings with
// `ids` and returns the resulting projection.
func (r *Repo) SetNamespaceMarkings(ctx context.Context, namespaceID uuid.UUID, ids []uuid.UUID, actor uuid.UUID) (*markings.NamespaceMarkings, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM iceberg_namespace_markings WHERE namespace_id = $1`, namespaceID); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx,
			`INSERT INTO iceberg_namespace_markings (namespace_id, marking_id, created_by)
			 VALUES ($1, $2, $3)
			 ON CONFLICT (namespace_id, marking_id) DO NOTHING`, namespaceID, id, actor); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.LoadNamespaceMarkings(ctx, namespaceID)
}

// SetTableExplicitMarkings replaces the table's *explicit* markings
// with `ids` (inherited rows survive untouched) and refreshes the
// cached `iceberg_tables.markings` array so existing handlers that
// read the cache see the new effective union.
func (r *Repo) SetTableExplicitMarkings(ctx context.Context, tableID uuid.UUID, ids []uuid.UUID, actor uuid.UUID) (*markings.TableMarkings, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM iceberg_table_markings WHERE table_id = $1 AND source = 'explicit'`, tableID); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, err := tx.Exec(ctx,
			`INSERT INTO iceberg_table_markings (table_id, marking_id, source, created_by)
			 VALUES ($1, $2, 'explicit', $3)
			 ON CONFLICT (table_id, marking_id, source) DO NOTHING`, tableID, id, actor); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE iceberg_tables t
		 SET markings = COALESCE((
		     SELECT array_agg(DISTINCT mn.name ORDER BY mn.name)
		     FROM iceberg_table_markings tm
		     JOIN iceberg_marking_names mn ON mn.marking_id = tm.marking_id
		     WHERE tm.table_id = t.id
		 ), '{}'::TEXT[])
		 WHERE t.id = $1`, tableID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.LoadTableMarkings(ctx, tableID)
}

func dedupUUIDs(in []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(in))
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
