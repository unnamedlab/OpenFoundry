package jwksrotation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PostgresJwksKeyStore is exercised end-to-end against a real
// Postgres in the consuming service's integration tests. The unit
// tests here cover the SQL-shape invariants + the scanJwksKeyRow
// projection — both can break independently of the database.

// --- Compile-time interface assertion ----------------------------------

func TestPostgresStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ JwksKeyStore = (*PostgresJwksKeyStore)(nil)
}

// --- jwksKeyColumns invariant ------------------------------------------

func TestJwksKeyColumnsCovers10Fields(t *testing.T) {
	t.Parallel()
	// scanJwksKeyRow assumes the projection has exactly these 10
	// columns in this order; the SELECT helpers concatenate them
	// via the jwksKeyColumns const. Lock both shape AND order.
	want := []string{
		"kid", "kty", "public_pem", "vault_key_name", "vault_key_version",
		"status", "activated_at", "grace_started_at", "retire_after", "retired_at",
	}
	got := strings.FieldsFunc(jwksKeyColumns, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	assert.Equal(t, want, got, "scanJwksKeyRow + every SELECT statement depend on this exact column order")
}

// --- scanJwksKeyRow projection -----------------------------------------

// fakeScanner mimics pgx.Row / pgx.Rows.Scan with a fixed argument
// vector so the test exercises the exact dest slice scanJwksKeyRow
// passes.
type fakeScanner struct {
	values []any
	err    error
}

func (f *fakeScanner) Scan(dest ...any) error {
	if f.err != nil {
		return f.err
	}
	if len(dest) != len(f.values) {
		return errors.New("dest count mismatch")
	}
	for i, d := range dest {
		assignAny(d, f.values[i])
	}
	return nil
}

// assignAny copies `src` into the value pointed to by `dst`. Only
// supports the exact type pairs scanJwksKeyRow uses — keeps the
// test focused.
func assignAny(dst, src any) {
	switch d := dst.(type) {
	case *string:
		*d = src.(string)
	case *int32:
		*d = src.(int32)
	case *time.Time:
		*d = src.(time.Time)
	case **time.Time:
		if src == nil {
			*d = nil
		} else if t, ok := src.(time.Time); ok {
			*d = &t
		}
	}
}

func TestScanJwksKeyRowProjectsAllFields(t *testing.T) {
	t.Parallel()
	activatedAt := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	graceStartedAt := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	retireAfter := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)

	scanner := &fakeScanner{values: []any{
		"openfoundry-jwt-v1",   // kid
		"RSA",                  // kty
		"-----BEGIN PUBLIC...", // public_pem
		"openfoundry-jwt",      // vault_key_name
		int32(1),               // vault_key_version
		"grace",                // status
		activatedAt,            // activated_at
		graceStartedAt,         // grace_started_at
		retireAfter,            // retire_after
		nil,                    // retired_at
	}}

	got, err := scanJwksKeyRow(scanner)
	require.NoError(t, err)
	assert.Equal(t, "openfoundry-jwt-v1", got.Kid)
	assert.Equal(t, "RSA", got.Kty)
	assert.Equal(t, "openfoundry-jwt", got.VaultKeyName)
	assert.Equal(t, uint32(1), got.VaultKeyVersion)
	assert.Equal(t, StatusGrace, got.Status)
	assert.True(t, got.ActivatedAt.Equal(activatedAt))
	require.NotNil(t, got.GraceStartedAt)
	assert.True(t, got.GraceStartedAt.Equal(graceStartedAt))
	require.NotNil(t, got.RetireAfter)
	assert.True(t, got.RetireAfter.Equal(retireAfter))
	assert.Nil(t, got.RetiredAt)
}

func TestScanJwksKeyRowRejectsNegativeVersion(t *testing.T) {
	t.Parallel()
	scanner := &fakeScanner{values: []any{
		"k", "RSA", "PEM", "k", int32(-7), "active",
		time.Now(), nil, nil, nil,
	}}
	_, err := scanJwksKeyRow(scanner)
	require.Error(t, err)
	assert.True(t, IsJwksStore(err))
	assert.Contains(t, err.Error(), "vault_key_version must be positive")
}

func TestScanJwksKeyRowRejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	scanner := &fakeScanner{values: []any{
		"k", "RSA", "PEM", "k", int32(1), "rotated",
		time.Now(), nil, nil, nil,
	}}
	_, err := scanJwksKeyRow(scanner)
	require.Error(t, err)
	assert.True(t, IsJwksStore(err))
	assert.Contains(t, err.Error(), "unknown JWKS key status")
}

func TestScanJwksKeyRowPropagatesScanError(t *testing.T) {
	t.Parallel()
	want := errors.New("connection reset")
	scanner := &fakeScanner{err: want}
	_, err := scanJwksKeyRow(scanner)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

func TestScanJwksKeyRowPropagatesNoRows(t *testing.T) {
	t.Parallel()
	// pgx.ErrNoRows must surface unwrapped so callers (ActiveKey)
	// can errors.Is-detect and return (nil, nil).
	scanner := &fakeScanner{err: pgx.ErrNoRows}
	_, err := scanJwksKeyRow(scanner)
	require.Error(t, err)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

// --- DDL constants -----------------------------------------------------

func TestJwksKeysDDLContainsCheckConstraint(t *testing.T) {
	t.Parallel()
	// status CHECK constraint catches schema drift between the
	// writer (UpsertActiveSeed/RotateTo/RollbackTo) and the
	// migration; locking it in here prevents a silent drop.
	assert.Contains(t, JwksKeysDDL, "status TEXT NOT NULL CHECK (status IN ('active', 'grace', 'retired'))")
}

func TestJwksKeysIndexesArePartialOnActive(t *testing.T) {
	t.Parallel()
	assert.Contains(t, JwksKeysActiveIndexDDL, "WHERE status = 'active'",
		"the active-key lookup partial index is what makes ActiveKey O(log n)")
}

func TestJwksKeysIndexesAllIdempotent(t *testing.T) {
	t.Parallel()
	// EnsureSchema runs at boot; CREATE INDEX must be IF NOT EXISTS
	// or repeated boots fail.
	assert.Contains(t, JwksKeysDDL, "CREATE TABLE IF NOT EXISTS")
	assert.Contains(t, JwksKeysActiveIndexDDL, "CREATE INDEX IF NOT EXISTS")
	assert.Contains(t, JwksKeysVersionIndexDDL, "CREATE INDEX IF NOT EXISTS")
}
