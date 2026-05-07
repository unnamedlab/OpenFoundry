package cedarauthz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// PolicyTable name validation (rejects identifiers that could form a
// SQL injection vector — every char must be alnum or underscore).
func TestPgPolicyStoreRejectsBadTableNames(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	cases := []string{
		"",
		"cedar policies",      // space
		`cedar"policies`,      // quote
		"cedar;DROP TABLE x;", // semicolon
		"cedar-policies",      // hyphen (not underscore)
	}
	for _, name := range cases {
		pg := cedarauthz.NewPgPolicyStore(mock, store).WithTable(name)
		_, err := pg.Reload(context.Background())
		require.Error(t, err, "table name %q must be rejected", name)
		assert.True(t, errors.Is(err, cedarauthz.ErrBackend), "error: %v", err)
	}
}

func TestPgPolicyStoreReloadEmptyTable(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	mock.ExpectQuery(`SELECT id, version, source, description`).
		WillReturnRows(pgxmock.NewRows([]string{"id", "version", "source", "description"}))

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	count, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.True(t, store.IsEmpty())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestPgPolicyStoreReloadHydratesAndSwapsAtomically(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	desc := "cleared readers permitted"
	rows := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow(
			"permit-cleared-readers",
			int32(1),
			`permit(
				principal,
				action == Action::"read",
				resource is Dataset
			) when {
				principal.clearances.containsAll(resource.markings)
			};`,
			&desc,
		)
	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(rows)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	count, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	assert.Equal(t, 1, store.Len())
	require.NoError(t, mock.ExpectationsWereMet())
}

// A second Reload with a different row set must atomically replace the
// previous bundle (matches the Rust idempotent contract).
func TestPgPolicyStoreReloadIsIdempotent(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	src := `permit(principal, action == Action::"read", resource is Dataset);`
	first := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("p1", int32(1), src, (*string)(nil))
	second := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("p2", int32(1), src, (*string)(nil)).
		AddRow("p3", int32(1), src, (*string)(nil))

	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(first)
	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(second)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	c1, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, c1)
	assert.Equal(t, 1, store.Len())

	c2, err := pg.Reload(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, c2)
	assert.Equal(t, 2, store.Len())
	require.NoError(t, mock.ExpectationsWereMet())
}

// Bad SQL bubbles up as ErrBackend, store stays unchanged.
func TestPgPolicyStoreReloadPropagatesBackendError(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	mock.ExpectQuery(`SELECT id, version, source, description`).
		WillReturnError(errors.New("connection refused"))

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	_, err = pg.Reload(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, cedarauthz.ErrBackend))
	assert.True(t, store.IsEmpty(), "failure must not leave a partial state")
}

// Reload with a malformed cedar policy bubbles up as PolicyParseError
// and never swaps the active set.
func TestPgPolicyStoreReloadFailsOnInvalidCedar(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewWithPolicies([]cedarauthz.PolicyRecord{
		{ID: "p1", Source: `permit(principal, action == Action::"read", resource is Dataset);`},
	})
	require.NoError(t, err)
	require.Equal(t, 1, store.Len())

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	bad := pgxmock.NewRows([]string{"id", "version", "source", "description"}).
		AddRow("broken", int32(1), "this is not cedar", (*string)(nil))
	mock.ExpectQuery(`SELECT id, version, source, description`).WillReturnRows(bad)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	_, err = pg.Reload(context.Background())
	require.Error(t, err)
	var ppe *cedarauthz.PolicyParseError
	require.True(t, errors.As(err, &ppe), "want PolicyParseError, got %T", err)
	assert.Equal(t, "broken", ppe.ID)
	// Active set is preserved — the bad bundle was never swapped in.
	assert.Equal(t, 1, store.Len(), "failed reload preserves previous policies")
}

// Smoke test for context cancellation propagation.
func TestPgPolicyStoreReloadHonoursContext(t *testing.T) {
	t.Parallel()
	store, err := cedarauthz.NewEmpty()
	require.NoError(t, err)
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	mock.ExpectQuery(`SELECT id, version, source, description`).
		WillReturnError(context.DeadlineExceeded)

	pg := cedarauthz.NewPgPolicyStore(mock, store)
	_, err = pg.Reload(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, cedarauthz.ErrBackend))
}
