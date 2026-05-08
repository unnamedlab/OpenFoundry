package jwksrotation

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresJwksKeyStore is the production JwksKeyStore backed by the
// `jwks_keys` table in the identity control-plane database. Mirrors
// services/identity-federation-service/src/hardening/jwks_rotation.rs::
// PostgresJwksKeyStore verbatim — same SQL shape, same atomic
// rotate/rollback semantics under a single transaction.
//
// The schema lives at the package level (JwksKeysDDL +
// JwksKeysActiveIndexDDL + JwksKeysVersionIndexDDL constants in
// jwksrotation.go) so EnsureSchema is a thin wrapper around three
// DDL execs.
type PostgresJwksKeyStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps a pgxpool.Pool as a JwksKeyStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresJwksKeyStore {
	return &PostgresJwksKeyStore{pool: pool}
}

// Compile-time interface assertion.
var _ JwksKeyStore = (*PostgresJwksKeyStore)(nil)

// EnsureSchema materialises the jwks_keys table + supporting
// indexes. Idempotent (CREATE IF NOT EXISTS).
func (s *PostgresJwksKeyStore) EnsureSchema(ctx context.Context) error {
	for _, stmt := range []string{
		JwksKeysDDL,
		JwksKeysActiveIndexDDL,
		JwksKeysVersionIndexDDL,
	} {
		if _, err := s.pool.Exec(ctx, stmt); err != nil {
			return jwksStoreErr(err)
		}
	}
	return nil
}

const jwksKeyColumns = `kid, kty, public_pem, vault_key_name, vault_key_version,
                       status, activated_at, grace_started_at, retire_after, retired_at`

// ActiveKey returns the most recently-activated row whose status is
// "active", or nil when none exists.
func (s *PostgresJwksKeyStore) ActiveKey(ctx context.Context) (*JwksKeyRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+jwksKeyColumns+`
           FROM jwks_keys WHERE status = 'active'
          ORDER BY activated_at DESC LIMIT 1`)
	rec, err := scanJwksKeyRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, jwksStoreErr(err)
	}
	return &rec, nil
}

// GraceKeys returns every "grace" row whose retire_after is NULL or
// strictly after `now`, sorted by activated_at DESC.
func (s *PostgresJwksKeyStore) GraceKeys(ctx context.Context, now time.Time) ([]JwksKeyRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+jwksKeyColumns+`
           FROM jwks_keys
          WHERE status = 'grace' AND (retire_after IS NULL OR retire_after > $1)
          ORDER BY activated_at DESC`, now)
	if err != nil {
		return nil, jwksStoreErr(err)
	}
	defer rows.Close()
	out := make([]JwksKeyRecord, 0)
	for rows.Next() {
		rec, err := scanJwksKeyRow(rows)
		if err != nil {
			return nil, jwksStoreErr(err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, jwksStoreErr(err)
	}
	return out, nil
}

// UpsertActiveSeed inserts the boot-time active row, or rewrites
// the active record in place when one already exists for the given
// kid. Mirrors the Rust ON CONFLICT (kid) DO UPDATE shape.
func (s *PostgresJwksKeyStore) UpsertActiveSeed(ctx context.Context, record JwksKeyRecord) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO jwks_keys
            (kid, kty, public_pem, vault_key_name, vault_key_version, status,
             activated_at, grace_started_at, retire_after, retired_at)
         VALUES ($1, $2, $3, $4, $5, 'active', $6, NULL, NULL, NULL)
         ON CONFLICT (kid) DO UPDATE SET
            public_pem = EXCLUDED.public_pem,
            status = 'active',
            retired_at = NULL,
            updated_at = NOW()`,
		record.Kid, record.Kty, record.PublicPEM,
		record.VaultKeyName, int32(record.VaultKeyVersion),
		record.ActivatedAt)
	if err != nil {
		return jwksStoreErr(err)
	}
	return nil
}

// RotateTo atomically demotes `previous` to grace (with retire_after
// = graceUntil) and inserts/upgrades `next` as the new active row.
// Both writes share a single tx so a partial state is impossible.
func (s *PostgresJwksKeyStore) RotateTo(
	ctx context.Context,
	previous JwksKeyRecord,
	next JwksKeyRecord,
	graceUntil time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return jwksStoreErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE jwks_keys
            SET status = 'grace', grace_started_at = NOW(),
                retire_after = $2, updated_at = NOW()
          WHERE kid = $1`,
		previous.Kid, graceUntil); err != nil {
		return jwksStoreErr(err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO jwks_keys
            (kid, kty, public_pem, vault_key_name, vault_key_version, status,
             activated_at, grace_started_at, retire_after, retired_at)
         VALUES ($1, $2, $3, $4, $5, 'active', $6, NULL, NULL, NULL)
         ON CONFLICT (kid) DO UPDATE SET
            public_pem = EXCLUDED.public_pem,
            status = 'active',
            activated_at = EXCLUDED.activated_at,
            grace_started_at = NULL,
            retire_after = NULL,
            retired_at = NULL,
            updated_at = NOW()`,
		next.Kid, next.Kty, next.PublicPEM,
		next.VaultKeyName, int32(next.VaultKeyVersion),
		next.ActivatedAt); err != nil {
		return jwksStoreErr(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return jwksStoreErr(err)
	}
	return nil
}

// RollbackTo atomically demotes `demoted` to grace and restores
// `restored` to active. Both writes share a single tx.
func (s *PostgresJwksKeyStore) RollbackTo(
	ctx context.Context,
	restored JwksKeyRecord,
	demoted JwksKeyRecord,
	graceUntil time.Time,
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return jwksStoreErr(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE jwks_keys
            SET status = 'grace', grace_started_at = NOW(),
                retire_after = $2, updated_at = NOW()
          WHERE kid = $1`,
		demoted.Kid, graceUntil); err != nil {
		return jwksStoreErr(err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE jwks_keys
            SET status = 'active', grace_started_at = NULL,
                retire_after = NULL, retired_at = NULL,
                updated_at = NOW()
          WHERE kid = $1`,
		restored.Kid); err != nil {
		return jwksStoreErr(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return jwksStoreErr(err)
	}
	return nil
}

// pgScanner is the shape both pgx.Rows and pgx.Row satisfy for the
// 10-column projection. A small interface keeps scanJwksKeyRow
// reusable between ActiveKey (Row) and GraceKeys (Rows).
type pgScanner interface {
	Scan(dest ...any) error
}

// scanJwksKeyRow projects the 10-column SELECT into a
// JwksKeyRecord. Mirrors impl TryFrom<JwksKeyRow> for JwksKeyRecord
// — translates the int32 vault_key_version into uint32 and rejects
// negatives as a Store-classified RotationError.
func scanJwksKeyRow(s pgScanner) (JwksKeyRecord, error) {
	var (
		rec            JwksKeyRecord
		statusStr      string
		vaultKeyVer    int32
		graceStartedAt *time.Time
		retireAfter    *time.Time
		retiredAt      *time.Time
	)
	if err := s.Scan(
		&rec.Kid, &rec.Kty, &rec.PublicPEM, &rec.VaultKeyName,
		&vaultKeyVer, &statusStr, &rec.ActivatedAt,
		&graceStartedAt, &retireAfter, &retiredAt,
	); err != nil {
		return rec, err
	}
	if vaultKeyVer < 0 {
		return rec, &JwksRotationError{
			Kind:    ErrJwksStore,
			Message: "vault_key_version must be positive",
		}
	}
	rec.VaultKeyVersion = uint32(vaultKeyVer)
	status, err := ParseJwksKeyStatus(statusStr)
	if err != nil {
		return rec, err
	}
	rec.Status = status
	rec.GraceStartedAt = graceStartedAt
	rec.RetireAfter = retireAfter
	rec.RetiredAt = retiredAt
	return rec, nil
}
