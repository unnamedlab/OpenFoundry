package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

type fakeAppendStore struct {
	table       *models.IcebergTable
	getErr      error
	commitErr   error
	commitCalls int
	seenProject string
	seenNS      []string
	seenTable   string
	seenCommit  *models.CommitTableRequest
}

func (f *fakeAppendStore) ListNamespaces(context.Context, string) ([]models.IcebergNamespace, error) {
	return nil, nil
}
func (f *fakeAppendStore) ListTopLevelNamespaces(context.Context, string) ([]models.IcebergNamespace, error) {
	return nil, nil
}
func (f *fakeAppendStore) FetchNamespaceByName(context.Context, string, []string) (*models.IcebergNamespace, error) {
	return nil, nil
}
func (f *fakeAppendStore) GetNamespace(context.Context, uuid.UUID) (*models.IcebergNamespace, error) {
	return nil, nil
}
func (f *fakeAppendStore) CreateNamespace(context.Context, *models.CreateNamespaceRequest, uuid.UUID) (*models.IcebergNamespace, error) {
	return nil, nil
}
func (f *fakeAppendStore) UpdateNamespaceProperties(context.Context, uuid.UUID, []byte) (*models.IcebergNamespace, error) {
	return nil, nil
}
func (f *fakeAppendStore) DeleteNamespace(context.Context, uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeAppendStore) ListTables(context.Context, string, []string) ([]models.IcebergTable, error) {
	return nil, nil
}
func (f *fakeAppendStore) GetTable(_ context.Context, projectRID string, namespace []string, tableName string) (*models.IcebergTable, error) {
	f.seenProject = projectRID
	f.seenNS = namespace
	f.seenTable = tableName
	return f.table, f.getErr
}
func (f *fakeAppendStore) CreateTable(context.Context, string, []string, *models.CreateTableRequest, uuid.UUID) (*models.IcebergTable, string, error) {
	return nil, "", nil
}
func (f *fakeAppendStore) CommitTable(_ context.Context, _ string, _ []string, _ string, body *models.CommitTableRequest) (*models.IcebergTable, string, error) {
	f.commitCalls++
	f.seenCommit = body
	if f.commitErr != nil {
		return nil, "", f.commitErr
	}
	return f.table, "s3://warehouse/of_test/events/metadata/v2.metadata.json", nil
}
func (f *fakeAppendStore) MultiTableCommit(context.Context, string, *models.MultiTableCommitRequest) ([]models.CommittedTable, error) {
	return nil, nil
}
func (f *fakeAppendStore) ListSnapshots(context.Context, uuid.UUID) ([]models.Snapshot, error) {
	return nil, nil
}
func (f *fakeAppendStore) GetSnapshot(context.Context, uuid.UUID, int64) (*models.Snapshot, error) {
	return nil, nil
}
func (f *fakeAppendStore) ListRefs(context.Context, uuid.UUID) ([]models.TableRef, error) {
	return nil, nil
}
func (f *fakeAppendStore) GetRef(context.Context, uuid.UUID, string) (*models.TableRef, error) {
	return nil, nil
}
func (f *fakeAppendStore) UpsertRef(context.Context, uuid.UUID, string, *models.UpdateRefRequest) (*models.TableRef, error) {
	return nil, nil
}
func (f *fakeAppendStore) DeleteRef(context.Context, uuid.UUID, string) (bool, error) {
	return false, nil
}
func (f *fakeAppendStore) ListMetadataFiles(context.Context, uuid.UUID) ([]models.MetadataFile, error) {
	return nil, nil
}
func (f *fakeAppendStore) GetMetadataFile(context.Context, uuid.UUID, int32) (*models.MetadataFile, error) {
	return nil, nil
}
func (f *fakeAppendStore) DropTable(context.Context, string, []string, string, bool) (bool, error) {
	return false, nil
}
func (f *fakeAppendStore) RenameTable(context.Context, string, []string, string, []string, string) (*models.IcebergTable, error) {
	return nil, nil
}

func TestAppendBatchAuditContractFixtureCommits(t *testing.T) {
	t.Parallel()
	store := &fakeAppendStore{table: appendFixtureTable("of_audit", "events", auditAppendSchema())}
	h := &handlers.Handlers{Repo: store}

	rec := httptest.NewRecorder()
	h.AppendBatch(rec, httptest.NewRequest(http.MethodPost, "/openfoundry/iceberg/v1/append", strings.NewReader(auditAppendFixture())))

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, "ri.foundry.main.project.default", store.seenProject)
	assert.Equal(t, []string{"of_audit"}, store.seenNS)
	assert.Equal(t, "events", store.seenTable)
	require.Equal(t, 1, store.commitCalls)
	require.NotNil(t, store.seenCommit)
	require.Len(t, store.seenCommit.Updates, 1)
	assert.Contains(t, string(store.seenCommit.Updates[0]), `"action":"add-snapshot"`)
	assert.Contains(t, rec.Body.String(), `"rows":1`)
}

func TestAppendBatchAIContractFixtureCommits(t *testing.T) {
	t.Parallel()
	store := &fakeAppendStore{table: appendFixtureTable("of_ai", "responses", aiAppendSchema())}
	h := &handlers.Handlers{Repo: store}

	rec := httptest.NewRecorder()
	h.AppendBatch(rec, httptest.NewRequest(http.MethodPost, "/openfoundry/iceberg/v1/append", strings.NewReader(aiAppendFixture())))

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, []string{"of_ai"}, store.seenNS)
	assert.Equal(t, "responses", store.seenTable)
	require.Equal(t, 1, store.commitCalls)
}

func TestAppendBatchReturnsNotFoundForMissingTable(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: &fakeAppendStore{}}
	rec := httptest.NewRecorder()
	h.AppendBatch(rec, httptest.NewRequest(http.MethodPost, "/openfoundry/iceberg/v1/append", strings.NewReader(auditAppendFixture())))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAppendBatchReturnsUnprocessableEntityForSchemaMismatch(t *testing.T) {
	t.Parallel()
	badSchema := []models.FieldSpec{{ID: 1, Name: "event_id", Type: "string", Required: true}}
	h := &handlers.Handlers{Repo: &fakeAppendStore{table: appendFixtureTable("of_audit", "events", badSchema)}}
	rec := httptest.NewRecorder()
	h.AppendBatch(rec, httptest.NewRequest(http.MethodPost, "/openfoundry/iceberg/v1/append", strings.NewReader(auditAppendFixture())))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestAppendBatchReturnsConflictForCommitSchemaAssertion(t *testing.T) {
	t.Parallel()
	store := &fakeAppendStore{table: appendFixtureTable("of_audit", "events", auditAppendSchema()), commitErr: errors.New("assert-current-schema-id failed")}
	h := &handlers.Handlers{Repo: store}
	rec := httptest.NewRecorder()
	h.AppendBatch(rec, httptest.NewRequest(http.MethodPost, "/openfoundry/iceberg/v1/append", strings.NewReader(auditAppendFixture())))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func appendFixtureTable(namespace, table string, schema []models.FieldSpec) *models.IcebergTable {
	schemaJSON, _ := json.Marshal(schema)
	partitionSpec, _ := json.Marshal("day(at)")
	sortOrder, _ := json.Marshal("at ASC")
	return &models.IcebergTable{
		ID:            uuid.New(),
		RID:           "ri.foundry.main.iceberg-table.test",
		NamespaceID:   uuid.New(),
		Namespace:     []string{namespace},
		Name:          table,
		TableUUID:     uuid.NewString(),
		FormatVersion: 2,
		Location:      "s3://warehouse/" + namespace + "/" + table,
		SchemaJSON:    schemaJSON,
		PartitionSpec: partitionSpec,
		SortOrder:     sortOrder,
		Properties:    json.RawMessage(`{}`),
		Markings:      []string{"public"},
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

func auditAppendSchema() []models.FieldSpec {
	return []models.FieldSpec{
		{ID: 1, Name: "event_id", Type: "uuid", Required: true},
		{ID: 2, Name: "at", Type: "timestamptz", Required: true},
		{ID: 3, Name: "correlation_id", Type: "string", Required: false},
		{ID: 4, Name: "kind", Type: "string", Required: true},
		{ID: 5, Name: "payload", Type: "string", Required: true},
	}
}

func aiAppendSchema() []models.FieldSpec {
	return []models.FieldSpec{
		{ID: 1, Name: "event_id", Type: "uuid", Required: true},
		{ID: 2, Name: "at", Type: "timestamptz", Required: true},
		{ID: 3, Name: "kind", Type: "string", Required: true},
		{ID: 4, Name: "run_id", Type: "uuid", Required: false},
		{ID: 5, Name: "trace_id", Type: "string", Required: false},
		{ID: 6, Name: "producer", Type: "string", Required: true},
		{ID: 7, Name: "schema_version", Type: "uint32", Required: true},
		{ID: 8, Name: "payload", Type: "string", Required: true},
	}
}

func auditAppendFixture() string {
	return `{"spec":{"catalog":"lakekeeper","warehouse":"warehouse-1","namespace":"of_audit","table":"events","partition_transform":"day(at)","sort_order":"at ASC","schema":[{"id":1,"name":"event_id","type":"uuid","required":true},{"id":2,"name":"at","type":"timestamptz","required":true},{"id":3,"name":"correlation_id","type":"string","required":false},{"id":4,"name":"kind","type":"string","required":true},{"id":5,"name":"payload","type":"string","required":true}]},"rows":[{"event_id":"00000000-0000-7000-8000-000000000001","at":1700000000000000,"correlation_id":"corr-1","kind":"auth.login.ok","payload":"{\"ok\":true}"}]}`
}

func aiAppendFixture() string {
	return `{"spec":{"catalog":"lakekeeper","warehouse":"warehouse-1","namespace":"of_ai","table":"responses","partition_transform":"day(at)","sort_order":"at ASC","schema":[{"id":1,"name":"event_id","type":"uuid","required":true},{"id":2,"name":"at","type":"timestamptz","required":true},{"id":3,"name":"kind","type":"string","required":true},{"id":4,"name":"run_id","type":"uuid","required":false},{"id":5,"name":"trace_id","type":"string","required":false},{"id":6,"name":"producer","type":"string","required":true},{"id":7,"name":"schema_version","type":"uint32","required":true},{"id":8,"name":"payload","type":"string","required":true}]},"rows":[{"event_id":"00000000-0000-7000-8000-000000000001","at":1700000000000000,"kind":"response","run_id":"00000000-0000-7000-8000-000000000123","trace_id":"trace-1","producer":"agent-runtime-service","schema_version":1,"payload":"{\"tokens\":42}"}]}`
}
