// Tests for the multi-table commit repo path (ICA-6).
//
// The full FOR UPDATE + apply-updates flow is integration-tested at
// the handler layer with a fake store that simulates the lock
// semantics; here we pin two unit-level invariants the repo owns:
//
//   - empty `TableChanges` short-circuits (no Begin / no SQL)
//   - a missing table surfaces as a typed RetryableError with
//     ConflictKind=Unknown so the build executor can label the
//     conflict source even before the lock is taken.
//
// Mirrors the Rust spec § "all-or-nothing commit" preflight.
package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/repo"
)

func TestMultiTableCommit_EmptyIsNoOp(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	r := &repo.Repo{Pool: mock}
	out, err := r.MultiTableCommit(context.Background(), "ri.foundry.main.project.x",
		&models.MultiTableCommitRequest{TableChanges: nil})
	require.NoError(t, err)
	assert.Empty(t, out)
	require.NoError(t, mock.ExpectationsWereMet(),
		"empty multi-table commit must not open a transaction or issue SQL")
}

func TestMultiTableCommit_NilBodyIsNoOp(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	r := &repo.Repo{Pool: mock}
	out, err := r.MultiTableCommit(context.Background(), "ri.foundry.main.project.x", nil)
	require.NoError(t, err)
	assert.Empty(t, out)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMultiTableCommit_RejectsMissingNamespace(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	r := &repo.Repo{Pool: mock}
	_, err = r.MultiTableCommit(context.Background(), "ri.foundry.main.project.x",
		&models.MultiTableCommitRequest{TableChanges: []models.MultiTableChange{
			{Identifier: models.TableIdentifier{Namespace: nil, Name: "logins"}},
		}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing namespace")
}

func TestMultiTableCommit_UnknownTableIsRetryableUnknown(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`FROM iceberg_tables`).
		WithArgs("ri.foundry.main.project.x", "events", "ghost").
		WillReturnRows(pgxmock.NewRows(tableColumns()))

	r := &repo.Repo{Pool: mock}
	_, err = r.MultiTableCommit(context.Background(), "ri.foundry.main.project.x",
		&models.MultiTableCommitRequest{TableChanges: []models.MultiTableChange{
			{Identifier: models.TableIdentifier{Namespace: []string{"events"}, Name: "ghost"}},
		}})
	var retry *repo.RetryableError
	require.True(t, errors.As(err, &retry), "missing table must surface as RetryableError")
	assert.Equal(t, models.ConflictKindUnknown, retry.ConflictingWith)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestMultiTableCommit_HappyPath pins the SQL side of a successful
// single-table commit through the multi-table path: GetTable → Begin →
// SELECT FOR UPDATE → properties read → final UPDATE → metadata-file
// INSERT → metadata-location UPDATE → Commit. We use a single table to
// keep the expectation list manageable; the multi-table fan-out is
// covered by the chaos test in the handler suite.
func TestMultiTableCommit_HappyPath(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	projectRID := "ri.foundry.main.project.x"
	tableID := uuid.New()

	mock.ExpectQuery(`FROM iceberg_tables`).
		WithArgs(projectRID, "events", "logins").
		WillReturnRows(tableRow(t, "logins", "events", tableID))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT current_snapshot_id, schema_json, last_sequence_number`).
		WithArgs(tableID).
		WillReturnRows(pgxmock.NewRows([]string{"current_snapshot_id", "schema_json", "last_sequence_number"}).
			AddRow((*int64)(nil), []byte(`{"schema-id":0,"type":"struct","fields":[]}`), int64(0)))
	mock.ExpectQuery(`UPDATE iceberg_tables SET schema_json`).
		WithArgs(anyArgs(8)...).
		WillReturnRows(tableRow(t, "logins", "events", tableID))
	mock.ExpectQuery(`SELECT MAX\(version\) FROM iceberg_table_metadata_files`).
		WithArgs(tableID).
		WillReturnRows(pgxmock.NewRows([]string{"max"}).AddRow((*int32)(nil)))
	mock.ExpectExec(`INSERT INTO iceberg_table_metadata_files`).
		WithArgs(anyArgs(4)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec(`UPDATE iceberg_tables SET current_metadata_location`).
		WithArgs(tableID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectCommit()

	r := &repo.Repo{Pool: mock}
	out, err := r.MultiTableCommit(context.Background(), projectRID,
		&models.MultiTableCommitRequest{
			BuildRID: "build-1",
			TableChanges: []models.MultiTableChange{{
				Identifier: models.TableIdentifier{Namespace: []string{"events"}, Name: "logins"},
				Updates:    nil,
			}},
		})
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "logins", out[0].Identifier.Name)
	assert.Equal(t, []string{"events"}, out[0].Identifier.Namespace)
	require.NoError(t, mock.ExpectationsWereMet())
}
