// Tests for the CommitTable handler error-envelope mapping (ICA-5).
//
// Covers the two typed errors the repo layer can surface and the
// HTTP shapes the slice contract pins:
//
//   - *repo.RequirementError       → 409 Conflict + envelope.kind
//   - *domain.SchemaIncompatibleError → 422 Unprocessable Entity + diff
//
// Mirrors `#[cfg(test)]` from
// services/iceberg-catalog-service/src/handlers/rest_catalog/tables.rs
// (the `enforce_schema_strict` + commit-failure branches) plus a few
// edge cases the Go side benefits from pinning explicitly.
package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/repo"
)

// commitStore extends fakeStore with a programmable CommitTable
// handler so each test can drive the typed-error branches in
// isolation.
type commitStore struct {
	*fakeStore
	commitErr error
}

func (c *commitStore) GetTable(_ context.Context, _ string, namespace []string, name string) (*models.IcebergTable, error) {
	c.fakeStore.mu.Lock()
	defer c.fakeStore.mu.Unlock()
	if t, ok := c.fakeStore.tables[tableKey(namespace, name)]; ok {
		return t, nil
	}
	return nil, nil
}

func (c *commitStore) CommitTable(_ context.Context, _ string, _ []string, _ string, _ *models.CommitTableRequest) (*models.IcebergTable, string, error) {
	if c.commitErr != nil {
		return nil, "", c.commitErr
	}
	return nil, "", errors.New("not configured")
}

func newCommitStore(t *testing.T) *commitStore {
	t.Helper()
	fs := newFakeStore()
	id := uuid.New()
	fs.tables[tableKey([]string{"events"}, "logins")] = &models.IcebergTable{
		ID:         id,
		RID:        "ri.foundry.main.iceberg-table." + id.String(),
		Namespace:  []string{"events"},
		Name:       "logins",
		TableUUID:  uuid.NewString(),
		Markings:   []string{"public"},
		SchemaJSON: json.RawMessage(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"long"}]}`),
	}
	return &commitStore{fakeStore: fs}
}

// TestCommitTable_RequirementErrorMapsTo409 verifies that a typed
// repo.RequirementError surfaces as HTTP 409 with the failing
// assertion `kind` exposed in the envelope. This is the contract
// PyIceberg + Spark rely on to retry without parsing free-form
// message strings.
func TestCommitTable_RequirementErrorMapsTo409(t *testing.T) {
	t.Parallel()
	store := newCommitStore(t)
	store.commitErr = &repo.RequirementError{
		Kind:   "assert-uuid",
		Detail: "expected aaa, found bbb",
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"requirements":[{"type":"assert-uuid","uuid":"aaa"}],"updates":[]}`
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables/logins", body),
		map[string]string{"namespace": "events", "table": "logins"})
	rec := httptest.NewRecorder()
	h.CommitTable(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var env models.ErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "assert-uuid", env.Error.Kind)
	assert.Equal(t, http.StatusConflict, env.Error.Code)
	assert.Contains(t, env.Error.Message, "assert-uuid")
}

// TestCommitTable_SchemaIncompatibleMapsTo422 verifies that a
// schema-strict diff surfaces as HTTP 422 with the structured diff in
// the body so the pipeline-authoring UI's "generate ALTER TABLE" CTA
// can build the migration without re-running the diff client-side.
func TestCommitTable_SchemaIncompatibleMapsTo422(t *testing.T) {
	t.Parallel()
	store := newCommitStore(t)
	current := json.RawMessage(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"long"}]}`)
	attempted := json.RawMessage(`{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"string"}]}`)
	diff := domain.DiffSchemas(current, attempted)
	store.commitErr = &domain.SchemaIncompatibleError{
		CurrentSchema:   current,
		AttemptedSchema: attempted,
		Diff:            diff,
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"updates":[{"action":"add-schema","schema":{"schema-id":0,"type":"struct","fields":[{"id":1,"name":"id","required":true,"type":"string"}]}}]}`
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables/logins", body),
		map[string]string{"namespace": "events", "table": "logins"})
	rec := httptest.NewRecorder()
	h.CommitTable(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	var view map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	errBody, ok := view["error"].(map[string]any)
	require.True(t, ok, "error envelope")
	assert.Equal(t, float64(http.StatusUnprocessableEntity), errBody["code"])
	assert.Equal(t, "UnprocessableEntityException", errBody["type"])
	diffBody, ok := errBody["diff"].(map[string]any)
	require.True(t, ok, "diff payload")
	deltas, ok := diffBody["deltas"].([]any)
	require.True(t, ok)
	require.Len(t, deltas, 1)
	first := deltas[0].(map[string]any)
	assert.Equal(t, "changed-column-type", first["kind"])
	assert.Equal(t, "id", first["name"])
	assert.Equal(t, "long", first["from"])
	assert.Equal(t, "string", first["to"])
}

// TestCommitTable_GenericErrorFallsBackToStatusFromErr confirms that
// errors that are neither RequirementError nor SchemaIncompatibleError
// keep their pre-ICA-5 mapping (avoids regressing existing callers).
func TestCommitTable_GenericErrorFallsBackToStatusFromErr(t *testing.T) {
	t.Parallel()
	store := newCommitStore(t)
	store.commitErr = fmt.Errorf("boom: something broke")
	h := &handlers.Handlers{Repo: store}

	body := `{"requirements":[],"updates":[]}`
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables/logins", body),
		map[string]string{"namespace": "events", "table": "logins"})
	rec := httptest.NewRecorder()
	h.CommitTable(rec, req)

	// statusFromErr classifies non-conflict errors as 400 — the
	// pre-existing fallback we don't want to regress.
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "boom")
}

// TestCommitTable_AssertCreateAlwaysFailsOnExistingTable pins the
// "table must not exist" semantics of assert-create — in CommitTable
// the table is always present (we fetched it before validation), so
// this requirement is a guaranteed 409 with kind="assert-create".
func TestCommitTable_AssertCreateAlwaysFailsOnExistingTable(t *testing.T) {
	t.Parallel()
	store := newCommitStore(t)
	store.commitErr = &repo.RequirementError{
		Kind:   "assert-create",
		Detail: "table `logins` already exists",
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"requirements":[{"type":"assert-create"}],"updates":[]}`
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables/logins", body),
		map[string]string{"namespace": "events", "table": "logins"})
	rec := httptest.NewRecorder()
	h.CommitTable(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	var env models.ErrorEnvelope
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.Equal(t, "assert-create", env.Error.Kind)
}

// TestCommitTable_RequirementEnvelopeIsByteExact pins the JSON wire
// shape the slice's HTTP contract advertises. Anything that drifts
// (extra field, renamed field) breaks PyIceberg / Spark integration.
func TestCommitTable_RequirementEnvelopeIsByteExact(t *testing.T) {
	t.Parallel()
	store := newCommitStore(t)
	store.commitErr = &repo.RequirementError{
		Kind:   "assert-current-schema-id",
		Detail: "expected 5, found 3",
	}
	h := &handlers.Handlers{Repo: store}

	body := `{"requirements":[{"type":"assert-current-schema-id","current-schema-id":5}],"updates":[]}`
	req := withChiParams(authed("POST", "/iceberg/v1/namespaces/events/tables/logins", body),
		map[string]string{"namespace": "events", "table": "logins"})
	rec := httptest.NewRecorder()
	h.CommitTable(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	got := strings.TrimSpace(rec.Body.String())
	assert.JSONEq(t, `{"error":{
		"message":"assert-current-schema-id failed: expected 5, found 3",
		"type":"CommitFailedException",
		"code":409,
		"kind":"assert-current-schema-id"
	}}`, got)
}
