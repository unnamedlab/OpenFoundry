package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/models"
)

// diagnoseStore is a minimal in-memory Store that satisfies the surface the
// diagnose endpoint exercises (`ListTopLevelNamespaces` + `FetchNamespaceByName`)
// plus enough no-ops so the handler-level test compiles against the full
// Store interface.
type diagnoseStore struct {
	handlers.Store

	mu sync.Mutex

	topLevelNamespaces []models.IcebergNamespace
	listErr            error
	listCalls          atomic.Int32

	probeNamespace *models.IcebergNamespace
	probeErr       error
	probeCalls     atomic.Int32
	lastProbePath  []string
	lastProject    string
}

func (s *diagnoseStore) ListTopLevelNamespaces(_ context.Context, projectRID string) ([]models.IcebergNamespace, error) {
	s.listCalls.Add(1)
	s.mu.Lock()
	s.lastProject = projectRID
	s.mu.Unlock()
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.topLevelNamespaces, nil
}

func (s *diagnoseStore) FetchNamespaceByName(_ context.Context, projectRID string, path []string) (*models.IcebergNamespace, error) {
	s.probeCalls.Add(1)
	s.mu.Lock()
	s.lastProject = projectRID
	s.lastProbePath = append([]string(nil), path...)
	s.mu.Unlock()
	if s.probeErr != nil {
		return nil, s.probeErr
	}
	return s.probeNamespace, nil
}

func authedContext() context.Context {
	return authmw.ContextWithClaims(context.Background(), &authmw.Claims{Sub: uuid.New()})
}

func TestGetConfigRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: &diagnoseStore{}, WarehouseURI: "s3://example"}
	req := httptest.NewRequest("GET", "/iceberg/v1/config", nil)
	rec := httptest.NewRecorder()
	h.GetConfig(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestGetConfigEmitsWarehouseDefault(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: &diagnoseStore{}, WarehouseURI: "s3://foundry-iceberg-warehouse"}
	req := httptest.NewRequest("GET", "/iceberg/v1/config", nil).WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.GetConfig(rec, req)
	require.Equal(t, 200, rec.Code)

	var body handlers.ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, map[string]string{"warehouse": "s3://foundry-iceberg-warehouse"}, body.Defaults)
	assert.Equal(t, map[string]string{}, body.Overrides)

	// JSON shape guard — the catalog REST contract pins these keys.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &raw))
	assert.Contains(t, raw, "defaults")
	assert.Contains(t, raw, "overrides")
}

func TestGetConfigOmitsWarehouseWhenUnset(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: &diagnoseStore{}}
	req := httptest.NewRequest("GET", "/iceberg/v1/config", nil).WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.GetConfig(rec, req)
	require.Equal(t, 200, rec.Code)

	var body handlers.ConfigResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.Defaults)
	assert.Equal(t, map[string]string{}, body.Overrides)
}

func TestRunDiagnoseRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: &diagnoseStore{}}
	req := httptest.NewRequest("POST", "/iceberg/v1/diagnose",
		strings.NewReader(`{"client":"pyiceberg"}`))
	rec := httptest.NewRecorder()
	h.RunDiagnose(rec, req)
	assert.Equal(t, 401, rec.Code)
}

func TestRunDiagnoseRejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{Repo: &diagnoseStore{}}
	req := httptest.NewRequest("POST", "/iceberg/v1/diagnose",
		strings.NewReader(`not-json`)).WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.RunDiagnose(rec, req)
	assert.Equal(t, 400, rec.Code)
}

func TestRunDiagnoseSuccessReportsBothSteps(t *testing.T) {
	t.Parallel()
	store := &diagnoseStore{
		topLevelNamespaces: []models.IcebergNamespace{
			{ID: uuid.New(), Name: "lakehouse"},
			{ID: uuid.New(), Name: "warehouse"},
		},
		probeNamespace: &models.IcebergNamespace{Name: "_diagnostic"},
	}
	h := &handlers.Handlers{Repo: store}

	req := httptest.NewRequest("POST", "/iceberg/v1/diagnose",
		strings.NewReader(`{"client":"pyiceberg","project_rid":"ri.foundry.main.project.acme"}`)).
		WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.RunDiagnose(rec, req)

	require.Equal(t, 200, rec.Code)
	var body handlers.DiagnoseResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "pyiceberg", body.Client)
	assert.True(t, body.Success)
	require.Len(t, body.Steps, 2)

	assert.Equal(t, "list_namespaces", body.Steps[0].Name)
	assert.True(t, body.Steps[0].Ok)
	require.NotNil(t, body.Steps[0].Detail)
	assert.Equal(t, "2 namespaces", *body.Steps[0].Detail)

	assert.Equal(t, "load_probe_namespace", body.Steps[1].Name)
	assert.True(t, body.Steps[1].Ok)
	require.NotNil(t, body.Steps[1].Detail)
	assert.Equal(t, "probe namespace reachable", *body.Steps[1].Detail)

	assert.GreaterOrEqual(t, body.TotalLatencyMS, int64(0))
	assert.Equal(t, int32(1), store.listCalls.Load())
	assert.Equal(t, int32(1), store.probeCalls.Load())
	assert.Equal(t, "ri.foundry.main.project.acme", store.lastProject)
	assert.Equal(t, []string{"_diagnostic"}, store.lastProbePath)
}

func TestRunDiagnoseDefaultsProjectRIDWhenAbsent(t *testing.T) {
	t.Parallel()
	store := &diagnoseStore{}
	h := &handlers.Handlers{Repo: store}

	req := httptest.NewRequest("POST", "/iceberg/v1/diagnose",
		strings.NewReader(`{"client":"spark"}`)).WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.RunDiagnose(rec, req)

	require.Equal(t, 200, rec.Code)
	assert.Equal(t, "ri.foundry.main.project.default", store.lastProject)
}

func TestRunDiagnoseListErrorFailsStepButReturnsOK(t *testing.T) {
	t.Parallel()
	store := &diagnoseStore{listErr: errors.New("connection refused")}
	h := &handlers.Handlers{Repo: store}

	req := httptest.NewRequest("POST", "/iceberg/v1/diagnose",
		strings.NewReader(`{"client":"pyiceberg"}`)).WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.RunDiagnose(rec, req)

	require.Equal(t, 200, rec.Code)
	var body handlers.DiagnoseResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.False(t, body.Success)

	require.Len(t, body.Steps, 2)
	assert.False(t, body.Steps[0].Ok)
	require.NotNil(t, body.Steps[0].Detail)
	assert.Equal(t, "connection refused", *body.Steps[0].Detail)

	// Step 2 is still reported as ok (soft-warn) even when step 1 failed.
	assert.True(t, body.Steps[1].Ok)
}

func TestRunDiagnoseMissingProbeNamespaceIsSoftWarn(t *testing.T) {
	t.Parallel()
	store := &diagnoseStore{} // no probe namespace, no list rows
	h := &handlers.Handlers{Repo: store}

	req := httptest.NewRequest("POST", "/iceberg/v1/diagnose",
		strings.NewReader(`{"client":"pyiceberg"}`)).WithContext(authedContext())
	rec := httptest.NewRecorder()
	h.RunDiagnose(rec, req)

	require.Equal(t, 200, rec.Code)
	var body handlers.DiagnoseResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Success)
	require.Len(t, body.Steps, 2)

	assert.Equal(t, "list_namespaces", body.Steps[0].Name)
	require.NotNil(t, body.Steps[0].Detail)
	assert.Equal(t, "0 namespaces", *body.Steps[0].Detail)

	assert.Equal(t, "load_probe_namespace", body.Steps[1].Name)
	require.NotNil(t, body.Steps[1].Detail)
	assert.Equal(t, "no probe namespace; create `_diagnostic` to enable load probe", *body.Steps[1].Detail)
}

func TestDiagnoseResponseJSONShape(t *testing.T) {
	t.Parallel()
	detail := "1 namespaces"
	body := handlers.DiagnoseResponse{
		Client:  "pyiceberg",
		Success: true,
		Steps: []handlers.DiagnoseStep{
			{Name: "list_namespaces", Ok: true, LatencyMS: 12, Detail: &detail},
		},
		TotalLatencyMS: 25,
	}
	out, err := json.Marshal(body)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(out, &raw))
	for _, k := range []string{"client", "success", "steps", "total_latency_ms"} {
		assert.Contains(t, raw, k)
	}
	step := raw["steps"].([]any)[0].(map[string]any)
	for _, k := range []string{"name", "ok", "latency_ms", "detail"} {
		assert.Contains(t, step, k)
	}
}
