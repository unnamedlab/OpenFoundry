// Tests for the Iceberg table CRUD lifecycle (ICA-2). Mirrors the
// surface that services/iceberg-catalog-service/src/domain/table.rs and
// the rest_catalog/tables.rs handlers exercise.
package repo_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/repo"
)

// anyArgs returns n pgxmock.AnyArg matchers for variable-arg INSERT calls
// where we don't care about the exact bound values (uuids, JSON payloads).
func anyArgs(n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = pgxmock.AnyArg()
	}
	return out
}

// tableColumns returns the column ordering produced by tableSelect /
// scanTable. Keep in lockstep with repo.tableSelect.
func tableColumns() []string {
	return []string{
		"id", "rid", "namespace_id", "namespace_name", "name", "table_uuid",
		"format_version", "location", "current_snapshot_id", "current_metadata_location",
		"last_sequence_number", "partition_spec", "schema_json", "sort_order",
		"properties", "markings", "created_at", "updated_at",
	}
}

func tableRow(t *testing.T, name, namespaceName string, tableID uuid.UUID) *pgxmock.Rows {
	t.Helper()
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	return pgxmock.NewRows(tableColumns()).AddRow(
		tableID,
		"ri.foundry.main.iceberg-table."+tableID.String(),
		uuid.New(),
		namespaceName,
		name,
		uuid.NewString(),
		int32(2),
		"s3://openfoundry-warehouse/"+namespaceName+"/"+name,
		(*int64)(nil),
		(*string)(nil),
		int64(0),
		[]byte(`{"spec-id":0,"fields":[]}`),
		[]byte(`{"schema-id":0,"type":"struct","fields":[]}`),
		[]byte(`{"order-id":0,"fields":[]}`),
		[]byte(`{}`),
		[]string{"public"},
		now,
		now,
	)
}

func namespaceRow(t *testing.T, projectRID, name string, id uuid.UUID) *pgxmock.Rows {
	t.Helper()
	return pgxmock.NewRows([]string{
		"id", "project_rid", "name", "parent_namespace_id",
		"properties", "created_at", "created_by",
	}).AddRow(
		id, projectRID, name, (*uuid.UUID)(nil),
		[]byte(`{}`), time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), uuid.New(),
	)
}

func TestListTables_ScansAllRows(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	projectRID := "ri.foundry.main.project.x"
	mock.ExpectQuery(`FROM iceberg_tables`).
		WithArgs(projectRID, "events").
		WillReturnRows(
			tableRow(t, "logins", "events", uuid.New()).
				AddRow(uuid.New(), "ri.foundry.main.iceberg-table.x", uuid.New(),
					"events", "signups", uuid.NewString(), int32(2),
					"s3://openfoundry-warehouse/events/signups", (*int64)(nil), (*string)(nil),
					int64(0), []byte(`{}`), []byte(`{"schema-id":0}`),
					[]byte(`{}`), []byte(`{}`), []string{"public"},
					time.Now(), time.Now()),
		)

	r := &repo.Repo{Pool: mock}
	out, err := r.ListTables(context.Background(), projectRID, []string{"events"})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, []string{"events"}, out[0].Namespace)
	assert.Equal(t, "logins", out[0].Name)
	assert.Equal(t, "signups", out[1].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestGetTable_ReturnsNilOnNotFound(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`FROM iceberg_tables`).
		WithArgs("ri.foundry.main.project.x", "events", "missing").
		WillReturnRows(pgxmock.NewRows(tableColumns()))

	r := &repo.Repo{Pool: mock}
	got, err := r.GetTable(context.Background(), "ri.foundry.main.project.x", []string{"events"}, "missing")
	require.NoError(t, err)
	assert.Nil(t, got)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateTable_RejectsBadFormatVersion(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	nsID := uuid.New()
	mock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs("ri.foundry.main.project.x", "events").
		WillReturnRows(namespaceRow(t, "ri.foundry.main.project.x", "events", nsID))

	r := &repo.Repo{Pool: mock}
	bad := int32(99)
	_, _, err = r.CreateTable(context.Background(), "ri.foundry.main.project.x", []string{"events"},
		&models.CreateTableRequest{
			Name:          "t",
			Schema:        []byte(`{"schema-id":0}`),
			FormatVersion: &bad,
		}, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid format-version 99")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateTable_RequiresSchema(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	nsID := uuid.New()
	mock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs("ri.foundry.main.project.x", "events").
		WillReturnRows(namespaceRow(t, "ri.foundry.main.project.x", "events", nsID))

	r := &repo.Repo{Pool: mock}
	_, _, err = r.CreateTable(context.Background(), "ri.foundry.main.project.x", []string{"events"},
		&models.CreateTableRequest{Name: "t", Schema: []byte(`null`)}, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema is required")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCreateTable_HappyPathInsertsAndSeedsMetadata(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	projectRID := "ri.foundry.main.project.x"
	nsID := uuid.New()
	tableID := uuid.New()
	mock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs(projectRID, "events").
		WillReturnRows(namespaceRow(t, projectRID, "events", nsID))
	mock.ExpectQuery(`INSERT INTO iceberg_tables`).
		WithArgs(anyArgs(12)...).
		WillReturnRows(tableRow(t, "logins", "events", tableID))
	mock.ExpectExec(`INSERT INTO iceberg_table_metadata_files`).
		WithArgs(anyArgs(3)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	r := &repo.Repo{Pool: mock}
	tab, location, err := r.CreateTable(context.Background(), projectRID, []string{"events"},
		&models.CreateTableRequest{
			Name:   "logins",
			Schema: []byte(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"long"}]}`),
		}, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, tab)
	assert.Equal(t, "logins", tab.Name)
	assert.Equal(t, []string{"events"}, tab.Namespace)
	assert.True(t, len(location) > 0)
	assert.Contains(t, location, "/v1.metadata.json")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreateTable_AlreadyExists verifies the unique-violation surface
// from Postgres is wrapped into a clean "already exists" error so the
// REST handler maps it to HTTP 409 (mirrors Rust's
// TableError::AlreadyExists).
func TestCreateTable_AlreadyExists(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	projectRID := "ri.foundry.main.project.x"
	nsID := uuid.New()
	mock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs(projectRID, "events").
		WillReturnRows(namespaceRow(t, projectRID, "events", nsID))
	mock.ExpectQuery(`INSERT INTO iceberg_tables`).
		WithArgs(anyArgs(12)...).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "iceberg_tables_namespace_id_name_key"})

	r := &repo.Repo{Pool: mock}
	_, _, err = r.CreateTable(context.Background(), projectRID, []string{"events"},
		&models.CreateTableRequest{
			Name:   "logins",
			Schema: []byte(`{"schema-id":0,"type":"struct","fields":[]}`),
		}, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already exists in namespace")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestCreateTable_ConcurrentOnlyOneSucceeds simulates two callers
// racing to create the same (namespace, name). The second insert hits
// the unique constraint and must surface AlreadyExists. The Rust
// implementation enforces this through a SELECT-then-INSERT plus the
// underlying UNIQUE(namespace_id, name) — under contention only the
// constraint is authoritative, which is what we assert here.
func TestCreateTable_ConcurrentOnlyOneSucceeds(t *testing.T) {
	t.Parallel()
	projectRID := "ri.foundry.main.project.x"
	nsID := uuid.New()
	tableID := uuid.New()

	winnerMock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer winnerMock.Close()
	winnerMock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs(projectRID, "events").
		WillReturnRows(namespaceRow(t, projectRID, "events", nsID))
	winnerMock.ExpectQuery(`INSERT INTO iceberg_tables`).
		WithArgs(anyArgs(12)...).
		WillReturnRows(tableRow(t, "logins", "events", tableID))
	winnerMock.ExpectExec(`INSERT INTO iceberg_table_metadata_files`).
		WithArgs(anyArgs(3)...).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	loserMock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer loserMock.Close()
	loserMock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs(projectRID, "events").
		WillReturnRows(namespaceRow(t, projectRID, "events", nsID))
	loserMock.ExpectQuery(`INSERT INTO iceberg_tables`).
		WithArgs(anyArgs(12)...).
		WillReturnError(&pgconn.PgError{Code: "23505", ConstraintName: "iceberg_tables_namespace_id_name_key"})

	body := &models.CreateTableRequest{
		Name:   "logins",
		Schema: []byte(`{"schema-id":0,"type":"struct","fields":[]}`),
	}

	var wg sync.WaitGroup
	var successes, conflicts int32
	wg.Add(2)
	for _, pool := range []pgxmock.PgxPoolIface{winnerMock, loserMock} {
		go func(p pgxmock.PgxPoolIface) {
			defer wg.Done()
			r := &repo.Repo{Pool: p}
			_, _, err := r.CreateTable(context.Background(), projectRID, []string{"events"}, body, uuid.New())
			if err == nil {
				atomic.AddInt32(&successes, 1)
				return
			}
			if assert.Contains(t, err.Error(), "already exists in namespace") {
				atomic.AddInt32(&conflicts, 1)
			}
		}(pool)
	}
	wg.Wait()

	assert.EqualValues(t, 1, successes, "exactly one caller must succeed")
	assert.EqualValues(t, 1, conflicts, "the other caller must see AlreadyExists")
	require.NoError(t, winnerMock.ExpectationsWereMet())
	require.NoError(t, loserMock.ExpectationsWereMet())
}

func TestDropTable_ReturnsFalseWhenAbsent(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec(`DELETE FROM iceberg_tables`).
		WithArgs("ri.foundry.main.project.x", "events", "ghost").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	r := &repo.Repo{Pool: mock}
	deleted, err := r.DropTable(context.Background(), "ri.foundry.main.project.x", []string{"events"}, "ghost", false)
	require.NoError(t, err)
	assert.False(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDropTable_ReportsDeletion(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectExec(`DELETE FROM iceberg_tables`).
		WithArgs("ri.foundry.main.project.x", "events", "logins").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	r := &repo.Repo{Pool: mock}
	deleted, err := r.DropTable(context.Background(), "ri.foundry.main.project.x", []string{"events"}, "logins", true)
	require.NoError(t, err)
	assert.True(t, deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRenameTable_AcrossNamespaces(t *testing.T) {
	t.Parallel()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	projectRID := "ri.foundry.main.project.x"
	destNS := uuid.New()
	mock.ExpectQuery(`FROM iceberg_namespaces`).
		WithArgs(projectRID, "warehouse").
		WillReturnRows(namespaceRow(t, projectRID, "warehouse", destNS))
	mock.ExpectQuery(`UPDATE iceberg_tables`).
		WithArgs(projectRID, "events", "logins", destNS, "logins_v2", "warehouse").
		WillReturnRows(tableRow(t, "logins_v2", "warehouse", uuid.New()))

	r := &repo.Repo{Pool: mock}
	tab, err := r.RenameTable(context.Background(), projectRID,
		[]string{"events"}, "logins",
		[]string{"warehouse"}, "logins_v2")
	require.NoError(t, err)
	require.NotNil(t, tab)
	assert.Equal(t, "logins_v2", tab.Name)
	assert.Equal(t, []string{"warehouse"}, tab.Namespace)
	require.NoError(t, mock.ExpectationsWereMet())
}
