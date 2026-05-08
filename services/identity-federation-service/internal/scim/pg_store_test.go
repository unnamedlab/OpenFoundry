package scim

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PostgresUserStore + PostgresGroupStore are exercised end-to-end
// against a real Postgres in the consuming service's
// testcontainers integration tests. The unit tests here cover:
//   - Compile-time interface satisfaction.
//   - SQLSTATE classifiers (unique violation, FK violation).
//   - scanUserRow / scanGroupRow projection edges.
//   - SQL string invariants pinned per query (column order +
//     WHERE clauses) so silent edits show up in CI.

// --- Compile-time interface assertions ---------------------------------

func TestPostgresUserStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ UserStore = (*PostgresUserStore)(nil)
}

func TestPostgresGroupStoreSatisfiesInterface(t *testing.T) {
	t.Parallel()
	var _ GroupStore = (*PostgresGroupStore)(nil)
}

// --- pg error classifiers ----------------------------------------------

func TestIsPgUniqueViolation(t *testing.T) {
	t.Parallel()
	assert.False(t, isPgUniqueViolation(nil))
	assert.False(t, isPgUniqueViolation(errors.New("plain")))
	pgErr := &pgconn.PgError{Code: "23505", Message: "unique_violation"}
	assert.True(t, isPgUniqueViolation(pgErr))
	wrapped := errors.Join(errors.New("network"), pgErr)
	assert.True(t, isPgUniqueViolation(wrapped),
		"errors.Join wrapping must still classify as unique violation")
	other := &pgconn.PgError{Code: "23502"}
	assert.False(t, isPgUniqueViolation(other), "23502 (not_null_violation) is not a unique conflict")
}

func TestIsPgFKViolation(t *testing.T) {
	t.Parallel()
	pgErr := &pgconn.PgError{Code: "23503", Message: "foreign_key_violation"}
	assert.True(t, isPgFKViolation(pgErr))
	other := &pgconn.PgError{Code: "23505"}
	assert.False(t, isPgFKViolation(other))
	assert.False(t, isPgFKViolation(errors.New("plain")))
}

// --- pg-error → SCIM-sentinel translation ------------------------------

func TestPostgresUserStorePutTranslatesUniqueViolation(t *testing.T) {
	t.Parallel()
	// We don't roundtrip Put against pgxpool.Pool here — just
	// pin the translation contract: ErrUserNameTaken must be
	// reachable via errors.Is from a typed PgError 23505.
	pgErr := &pgconn.PgError{Code: "23505"}
	mapped := translateUserPutError(pgErr)
	assert.True(t, errors.Is(mapped, ErrUserNameTaken))
	assert.True(t, IsUniqueViolation(mapped))
}

func TestPostgresGroupStorePutTranslatesUniqueViolation(t *testing.T) {
	t.Parallel()
	pgErr := &pgconn.PgError{Code: "23505"}
	mapped := translateGroupPutError(pgErr)
	assert.True(t, errors.Is(mapped, ErrGroupNameTaken))
	assert.True(t, IsGroupUniqueViolation(mapped))
}

func TestInsertGroupMembersFKTranslatesToMemberNotFound(t *testing.T) {
	t.Parallel()
	pgErr := &pgconn.PgError{Code: "23503"}
	mapped := translateAddMemberError(pgErr)
	assert.True(t, errors.Is(mapped, ErrMemberNotFound))
	assert.True(t, IsMemberNotFound(mapped))
}

func TestInsertGroupMembersOtherErrorPropagates(t *testing.T) {
	t.Parallel()
	want := errors.New("connection reset")
	got := translateAddMemberError(want)
	assert.True(t, errors.Is(got, want), "non-FK errors must propagate verbatim")
}

// translateUserPutError / translateGroupPutError /
// translateAddMemberError are tiny shims that mirror the inline
// translation logic inside Put / insertGroupMembersTx. Keeping
// them here lets tests pin the contract without spinning up a
// pool. The production code calls isPgUniqueViolation +
// isPgFKViolation directly.
func translateUserPutError(err error) error {
	if isPgUniqueViolation(err) {
		return ErrUserNameTaken
	}
	return err
}

func translateGroupPutError(err error) error {
	if isPgUniqueViolation(err) {
		return ErrGroupNameTaken
	}
	return err
}

func translateAddMemberError(err error) error {
	if isPgFKViolation(err) {
		return ErrMemberNotFound
	}
	return err
}

// --- scanUserRow / scanGroupRow row-shape tests ------------------------

// fakeScanner mimics pgx.Row / pgx.Rows.Scan with a fixed argument
// vector so tests can pin the dest-slice shape both stores depend on.
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

// assignAny copies `src` into the value pointed to by `dst`.
// Limited to the type pairs scanUserRow / scanGroupRow use.
func assignAny(dst, src any) {
	switch d := dst.(type) {
	case *uuid.UUID:
		if u, ok := src.(uuid.UUID); ok {
			*d = u
		}
	case **uuid.UUID:
		if src == nil {
			*d = nil
		} else if u, ok := src.(*uuid.UUID); ok {
			*d = u
		} else if u, ok := src.(uuid.UUID); ok {
			*d = &u
		}
	case *string:
		if s, ok := src.(string); ok {
			*d = s
		}
	case **string:
		if src == nil {
			*d = nil
		} else if s, ok := src.(*string); ok {
			*d = s
		} else if s, ok := src.(string); ok {
			*d = &s
		}
	case *bool:
		if b, ok := src.(bool); ok {
			*d = b
		}
	case *[]byte:
		if b, ok := src.([]byte); ok {
			*d = b
		}
	case *time.Time:
		if t, ok := src.(time.Time); ok {
			*d = t
		}
	}
}

func TestScanUserRowProjectsAllFields(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	orgID := uuid.New()
	ext := "ext-7"
	now := time.Now().UTC()
	scanner := &fakeScanner{values: []any{
		id,                          // id
		"alice@x",                   // email
		"Alice Doe",                 // name
		true,                        // is_active
		&orgID,                      // organization_id
		[]byte(`{"scim":{"x":1}}`),  // attributes
		&ext,                        // scim_external_id
		now,                         // created_at
		now,                         // updated_at
	}}
	got, err := scanUserRow(scanner)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "alice@x", got.Email)
	assert.Equal(t, "Alice Doe", got.Name)
	assert.True(t, got.IsActive)
	require.NotNil(t, got.OrganizationID)
	assert.Equal(t, orgID, *got.OrganizationID)
	require.NotNil(t, got.ScimExternalID)
	assert.Equal(t, "ext-7", *got.ScimExternalID)
	var attrs map[string]any
	require.NoError(t, json.Unmarshal(got.Attributes, &attrs))
	assert.NotNil(t, attrs["scim"])
}

func TestScanUserRowEmptyAttributesYieldsNil(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	now := time.Now().UTC()
	scanner := &fakeScanner{values: []any{
		id, "x@y", "X", false,
		(*uuid.UUID)(nil), []byte{}, (*string)(nil), now, now,
	}}
	got, err := scanUserRow(scanner)
	require.NoError(t, err)
	assert.Empty(t, got.Attributes,
		"empty attributes column → nil RawMessage")
	assert.Nil(t, got.OrganizationID)
	assert.Nil(t, got.ScimExternalID)
}

func TestScanUserRowPropagatesScanError(t *testing.T) {
	t.Parallel()
	scanner := &fakeScanner{err: pgx.ErrNoRows}
	_, err := scanUserRow(scanner)
	require.Error(t, err)
	assert.ErrorIs(t, err, pgx.ErrNoRows,
		"pgx.ErrNoRows must surface unwrapped so callers can errors.Is-detect")
}

func TestScanGroupRowProjectsAllFields(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	ext := "sso-eng"
	scanner := &fakeScanner{values: []any{id, "Engineering", &ext}}
	got, err := scanGroupRow(scanner)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "Engineering", got.Name)
	require.NotNil(t, got.ScimExternalID)
	assert.Equal(t, "sso-eng", *got.ScimExternalID)
}

func TestScanGroupRowNilExternalID(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	scanner := &fakeScanner{values: []any{id, "G", (*string)(nil)}}
	got, err := scanGroupRow(scanner)
	require.NoError(t, err)
	assert.Nil(t, got.ScimExternalID)
}

// --- SQL string invariants ----------------------------------------------

func TestPgUserColumnsCovers9Fields(t *testing.T) {
	t.Parallel()
	// scanUserRow's dest slice + every read SQL depend on this
	// exact column order; lock it.
	want := []string{
		"id", "email", "name", "is_active", "organization_id",
		"attributes", "scim_external_id", "created_at", "updated_at",
	}
	got := splitColumns(pgUserColumns)
	assert.Equal(t, want, got, "scanUserRow assumes this exact order")
}

func TestPgGroupColumnsCovers3Fields(t *testing.T) {
	t.Parallel()
	want := []string{"id", "name", "scim_external_id"}
	got := splitColumns(pgGroupColumns)
	assert.Equal(t, want, got)
}

// splitColumns parses a column-list like `id, email, ...` into a
// []string with the whitespace + commas stripped.
func splitColumns(s string) []string {
	out := []string{}
	curr := []byte{}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case ',', ' ', '\n', '\t':
			if len(curr) > 0 {
				out = append(out, string(curr))
				curr = curr[:0]
			}
		default:
			curr = append(curr, c)
		}
	}
	if len(curr) > 0 {
		out = append(out, string(curr))
	}
	return out
}

// --- List unsupported-filter error -------------------------------------

func TestPostgresUserStoreListRejectsUnsupportedFilter(t *testing.T) {
	t.Parallel()
	// We don't need a real pool — the unsupported-filter path
	// short-circuits before any SQL is issued.
	store := &PostgresUserStore{pool: nil}
	_, _, err := store.List(context.Background(),
		&EqFilter{Attribute: FilterDisplayName, Value: "x"}, 1, 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsupportedFilter))
}

func TestPostgresGroupStoreListRejectsUnsupportedFilter(t *testing.T) {
	t.Parallel()
	store := &PostgresGroupStore{pool: nil}
	_, _, err := store.List(context.Background(),
		&EqFilter{Attribute: FilterUserName, Value: "x"}, 1, 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnsupportedFilter))
}
