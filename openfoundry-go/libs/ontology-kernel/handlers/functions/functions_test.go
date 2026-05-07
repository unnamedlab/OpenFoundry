package functions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/domain"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
)

type fakeFunctionPackageLoader struct{ pkg *models.FunctionPackage }

func (f fakeFunctionPackageLoader) LoadFunctionPackage(context.Context, *ontologykernel.AppState, uuid.UUID) (*models.FunctionPackage, error) {
	return f.pkg, nil
}

type recordedFunctionRun struct {
	status       string
	errorMessage *string
	pkg          models.FunctionPackageSummary
	runCtx       domain.FunctionPackageRunContext
}

type fakeFunctionRunRecorder struct{ runs []recordedFunctionRun }

func (f *fakeFunctionRunRecorder) RecordFunctionPackageRun(_ context.Context, _ *ontologykernel.AppState, pkg models.FunctionPackageSummary, runCtx domain.FunctionPackageRunContext, _ time.Time, _ time.Time, _ int64, status string, errorMessage *string) error {
	f.runs = append(f.runs, recordedFunctionRun{status: status, errorMessage: errorMessage, pkg: pkg, runCtx: runCtx})
	return nil
}

type fakePythonInlineRuntime struct {
	result      []byte
	err         error
	seenSource  string
	seenInput   []byte
	seenTimeout uint32
}

func (f *fakePythonInlineRuntime) ExecuteInline(_ context.Context, source string, inputJSON []byte, timeoutSeconds uint32) (*ontologykernel.InlineRuntimeResult, error) {
	f.seenSource = source
	f.seenInput = append([]byte(nil), inputJSON...)
	f.seenTimeout = timeoutSeconds
	if f.err != nil {
		return nil, f.err
	}
	return &ontologykernel.InlineRuntimeResult{ResultJSON: f.result, Stdout: "stdout", Stderr: "stderr"}, nil
}

func testFunctionState(runtime ontologykernel.PythonInlineRuntime) *ontologykernel.AppState {
	return &ontologykernel.AppState{
		Stores:             stores.NewInMemory(),
		JWTConfig:          authmw.NewJWTConfig("test-secret"),
		OntologyServiceURL: "http://ontology.test",
		AIServiceURL:       "http://ai.test",
		PythonRuntime:      runtime,
	}
}

func testFunctionPackage(t *testing.T) *models.FunctionPackage {
	t.Helper()
	now := time.Now().UTC()
	return &models.FunctionPackage{
		ID:           uuid.New(),
		Name:         "classify_case",
		Version:      "1.0.0",
		DisplayName:  "Classify Case",
		Description:  "test package",
		Runtime:      "python",
		Source:       "result = {'ok': True}",
		Entrypoint:   "handler",
		Capabilities: models.FunctionCapabilities{AllowOntologyRead: true, TimeoutSeconds: 7, MaxSourceBytes: 1024},
		OwnerID:      uuid.New(),
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func simulateRequest(t *testing.T, packageID, objectTypeID uuid.UUID) *http.Request {
	t.Helper()
	body, err := json.Marshal(models.SimulateFunctionPackageRequest{
		ObjectTypeID: objectTypeID,
		Parameters:   json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatalf("request body: %v", err)
	}
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", packageID.String())
	claims := &authmw.Claims{Sub: uuid.New(), Email: "runner@example.com", Roles: []string{"admin"}}
	ctx := authmw.ContextWithClaims(context.Background(), claims)
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	return httptest.NewRequest(http.MethodPost, "/functions/"+packageID.String()+"/simulate", bytes.NewReader(body)).WithContext(ctx)
}

func TestSimulateFunctionPackagePythonOK(t *testing.T) {
	t.Parallel()
	pkg := testFunctionPackage(t)
	objectTypeID := uuid.New()
	runtime := &fakePythonInlineRuntime{result: []byte(`{"value":1,"stdout":["hi"],"stderr":["warn"]}`)}
	state := testFunctionState(runtime)
	recorder := &fakeFunctionRunRecorder{}

	rec := httptest.NewRecorder()
	SimulateFunctionPackageWithDeps(state, functionPackageSimulationDeps{
		Loader:   fakeFunctionPackageLoader{pkg: pkg},
		Recorder: recorder,
	}).ServeHTTP(rec, simulateRequest(t, pkg.ID, objectTypeID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var body models.SimulateFunctionPackageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response json: %v", err)
	}
	if body.Package.ID != pkg.ID || string(body.Result) != `{"value":1,"stdout":["hi"],"stderr":["warn"]}` {
		t.Fatalf("response drift: package=%+v result=%s", body.Package, body.Result)
	}
	if runtime.seenSource != pkg.Source || runtime.seenTimeout != 7 {
		t.Fatalf("runtime request drift: source=%q timeout=%d", runtime.seenSource, runtime.seenTimeout)
	}
	var input map[string]any
	if err := json.Unmarshal(runtime.seenInput, &input); err != nil {
		t.Fatalf("runtime input json: %v", err)
	}
	if input["functionPackage"] == nil || input["serviceToken"] == "" {
		t.Fatalf("runtime envelope missing package/token: %+v", input)
	}
	if len(recorder.runs) != 1 || recorder.runs[0].status != "success" || recorder.runs[0].errorMessage != nil {
		t.Fatalf("success run not recorded correctly: %+v", recorder.runs)
	}
	if recorder.runs[0].runCtx.InvocationKind != "simulation" || recorder.runs[0].runCtx.ObjectTypeID == nil || *recorder.runs[0].runCtx.ObjectTypeID != objectTypeID {
		t.Fatalf("run context drift: %+v", recorder.runs[0].runCtx)
	}
}

func TestSimulateFunctionPackagePythonExceptionRecordsFailure(t *testing.T) {
	t.Parallel()
	pkg := testFunctionPackage(t)
	state := testFunctionState(&fakePythonInlineRuntime{err: errors.New("Traceback: boom")})
	recorder := &fakeFunctionRunRecorder{}

	rec := httptest.NewRecorder()
	SimulateFunctionPackageWithDeps(state, functionPackageSimulationDeps{Loader: fakeFunctionPackageLoader{pkg: pkg}, Recorder: recorder}).ServeHTTP(rec, simulateRequest(t, pkg.ID, uuid.New()))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "Traceback: boom") {
		t.Fatalf("unexpected failure response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(recorder.runs) != 1 || recorder.runs[0].status != "failure" || recorder.runs[0].errorMessage == nil || !strings.Contains(*recorder.runs[0].errorMessage, "Traceback: boom") {
		t.Fatalf("failure run not recorded correctly: %+v", recorder.runs)
	}
}

func TestSimulateFunctionPackageMalformedResultRecordsFailure(t *testing.T) {
	t.Parallel()
	pkg := testFunctionPackage(t)
	state := testFunctionState(&fakePythonInlineRuntime{result: []byte(`{"ok":`)})
	recorder := &fakeFunctionRunRecorder{}

	rec := httptest.NewRecorder()
	SimulateFunctionPackageWithDeps(state, functionPackageSimulationDeps{Loader: fakeFunctionPackageLoader{pkg: pkg}, Recorder: recorder}).ServeHTTP(rec, simulateRequest(t, pkg.ID, uuid.New()))

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "failed to decode Python function response") {
		t.Fatalf("unexpected malformed response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(recorder.runs) != 1 || recorder.runs[0].status != "failure" || recorder.runs[0].errorMessage == nil || !strings.Contains(*recorder.runs[0].errorMessage, "malformed result JSON") {
		t.Fatalf("malformed failure run not recorded correctly: %+v", recorder.runs)
	}
}

func TestSimulateFunctionPackageRuntimeMissingRecordsFailure(t *testing.T) {
	t.Parallel()
	pkg := testFunctionPackage(t)
	state := testFunctionState(nil)
	recorder := &fakeFunctionRunRecorder{}

	rec := httptest.NewRecorder()
	SimulateFunctionPackageWithDeps(state, functionPackageSimulationDeps{Loader: fakeFunctionPackageLoader{pkg: pkg}, Recorder: recorder}).ServeHTTP(rec, simulateRequest(t, pkg.ID, uuid.New()))

	if rec.Code != http.StatusNotImplemented || !strings.Contains(rec.Body.String(), "python_runtime_not_wired") {
		t.Fatalf("unexpected missing-runtime response: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(recorder.runs) != 1 || recorder.runs[0].status != "failure" || recorder.runs[0].errorMessage == nil || !strings.Contains(*recorder.runs[0].errorMessage, "python runtime not wired") {
		t.Fatalf("missing-runtime failure run not recorded correctly: %+v", recorder.runs)
	}
}
