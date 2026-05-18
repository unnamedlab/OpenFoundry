package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/domain/function"
	dispatch "github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/executionmode"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/handler"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/compute-module-service/internal/repo"
)

// fakeDispatcher is the test dispatcher: tests stage a result; Dispatch
// returns the staged result/err and records the last invocation it saw.
type fakeDispatcher struct {
	last   *function.FunctionInvocation
	result dispatch.Result
	err    error
	calls  int
}

func (f *fakeDispatcher) Dispatch(_ context.Context, inv *function.FunctionInvocation) (dispatch.Result, error) {
	f.calls++
	f.last = inv.Clone()
	return f.result, f.err
}

func (f *fakeDispatcher) Cancel(_ context.Context, _ uuid.UUID) error { return nil }

func buildTestRouter(t *testing.T) (http.Handler, *uuid.UUID) {
	r, _, caller := buildTestRouterWithDispatcher(t)
	return r, caller
}

func buildTestRouterWithDispatcher(t *testing.T) (http.Handler, *fakeDispatcher, *uuid.UUID) {
	t.Helper()
	store := repo.NewMemoryRepository()
	disp := &fakeDispatcher{result: dispatch.Result{Status: function.StatusSucceeded, Payload: []byte(`{"ok":true}`)}}
	state := &handler.State{
		Repo:              store,
		Dispatcher:        disp,
		PayloadLimitBytes: dispatch.DefaultBodyLimitBytes,
		DispatchTimeout:   2 * time.Second,
	}

	r := chi.NewRouter()
	caller := uuid.New()
	tenant := uuid.New()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			claims := &authmw.Claims{Sub: caller, OrgID: &tenant, Email: "tester@openfoundry.local"}
			ctx := authmw.ContextWithClaims(req.Context(), claims)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	r.Post("/api/v1/compute-modules", state.Create)
	r.Get("/api/v1/compute-modules", state.List)
	r.Get("/api/v1/compute-modules/{id}", state.Get)
	r.Patch("/api/v1/compute-modules/{id}", state.UpdateMetadata)
	r.Post("/api/v1/compute-modules/{id}/move", state.Move)
	r.Post("/api/v1/compute-modules/{id}/duplicate", state.Duplicate)
	r.Post("/api/v1/compute-modules/{id}/archive", state.Archive)
	r.Post("/api/v1/compute-modules/{id}/restore", state.Restore)
	r.Delete("/api/v1/compute-modules/{id}", state.Delete)
	r.Get("/api/v1/compute-modules/{id}/execution-mode", state.GetExecutionMode)
	r.Put("/api/v1/compute-modules/{id}/pipeline-io", state.SetPipelineIOConfig)
	r.Delete("/api/v1/compute-modules/{id}/pipeline-io", state.ClearPipelineIOConfig)
	r.Post("/api/v1/compute-modules/{module_id}/functions/{name}/invoke", state.InvokeFunction)
	r.Post("/api/v1/compute-modules/{module_id}/functions/{name}/invoke-async", state.InvokeFunctionAsync)
	r.Get("/api/v1/compute-modules/invocations", state.ListInvocations)
	r.Get("/api/v1/compute-modules/invocations/{invocation_id}", state.GetInvocation)
	r.Post("/api/v1/compute-modules/invocations/{invocation_id}/cancel", state.CancelInvocation)
	r.Put("/api/v1/compute-modules/{id}/container-image", state.SetContainerImage)
	r.Get("/api/v1/compute-modules/{id}/container-image", state.GetContainerImage)
	r.Delete("/api/v1/compute-modules/{id}/container-image", state.ClearContainerImage)
	r.Post("/api/v1/compute-modules/container-image/validate", state.ValidateContainerImage)
	r.Put("/api/v1/compute-modules/{id}/runtime", state.SetRuntimeConfig)
	r.Get("/api/v1/compute-modules/{id}/runtime", state.GetRuntimeConfig)
	r.Delete("/api/v1/compute-modules/{id}/runtime", state.ClearRuntimeConfig)
	r.Post("/api/v1/compute-modules/runtime/validate", state.ValidateRuntimeConfig)

	return r, disp, &caller
}

func doJSON(t *testing.T, r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf).WithContext(context.Background())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode %s: %v", string(mustReadAll(w.Body)), err)
	}
	return out
}

func mustReadAll(rc io.Reader) []byte {
	b, _ := io.ReadAll(rc)
	return b
}

func TestCreateModuleHappyPath(t *testing.T) {
	r, _ := buildTestRouter(t)
	body := handler.CreateComputeModuleRequest{
		Name:          "Sales Forecast",
		Description:   "weekly model",
		ProjectID:     uuid.New(),
		ExecutionMode: models.ExecutionModeFunction,
		Labels:        map[string]string{"env": "dev"},
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/compute-modules", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
	got := decode[models.ComputeModule](t, w)
	if got.Name != "Sales Forecast" || got.ExecutionMode != models.ExecutionModeFunction {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestCreateRejectsInvalidExecutionMode(t *testing.T) {
	r, _ := buildTestRouter(t)
	body := handler.CreateComputeModuleRequest{
		Name:          "Bad mode",
		ProjectID:     uuid.New(),
		ExecutionMode: "container",
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/compute-modules", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
	}
}

func TestListPagination(t *testing.T) {
	r, _ := buildTestRouter(t)
	project := uuid.New()
	for i := 0; i < 4; i++ {
		body := handler.CreateComputeModuleRequest{
			Name:          "mod-" + time.Now().Format("150405.000") + "-" + uuid.New().String()[:6],
			ProjectID:     project,
			ExecutionMode: models.ExecutionModeFunction,
		}
		if w := doJSON(t, r, http.MethodPost, "/api/v1/compute-modules", body); w.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", w.Code, w.Body.String())
		}
	}
	w := doJSON(t, r, http.MethodGet, "/api/v1/compute-modules?limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	type page struct {
		Items      []models.ComputeModule `json:"items"`
		NextCursor *string                `json:"next_cursor,omitempty"`
	}
	got := decode[page](t, w)
	if len(got.Items) != 2 || got.NextCursor == nil {
		t.Fatalf("expected 2 items + cursor, got %+v", got)
	}
}

func TestUpdateMoveDuplicateLifecycle(t *testing.T) {
	r, _ := buildTestRouter(t)
	project := uuid.New()
	createBody := handler.CreateComputeModuleRequest{
		Name:          "Inventory Ingest",
		ProjectID:     project,
		ExecutionMode: models.ExecutionModePipeline,
	}
	w := doJSON(t, r, http.MethodPost, "/api/v1/compute-modules", createBody)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	created := decode[models.ComputeModule](t, w)

	// Patch description.
	desc := "new description"
	w = doJSON(t, r, http.MethodPatch, "/api/v1/compute-modules/"+created.ID.String(), handler.UpdateComputeModuleRequest{Description: &desc})
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	patched := decode[models.ComputeModule](t, w)
	if patched.Description != "new description" {
		t.Fatalf("description not updated: %q", patched.Description)
	}

	// Move to a new project.
	newProject := uuid.New()
	w = doJSON(t, r, http.MethodPost, "/api/v1/compute-modules/"+created.ID.String()+"/move", handler.MoveComputeModuleRequest{ProjectID: newProject})
	if w.Code != http.StatusOK {
		t.Fatalf("move: %d %s", w.Code, w.Body.String())
	}
	moved := decode[models.ComputeModule](t, w)
	if moved.ProjectID != newProject {
		t.Fatalf("project_id not updated: %s", moved.ProjectID)
	}

	// Duplicate into a different name.
	w = doJSON(t, r, http.MethodPost, "/api/v1/compute-modules/"+created.ID.String()+"/duplicate", handler.DuplicateComputeModuleRequest{NewName: "Inventory Ingest Copy"})
	if w.Code != http.StatusCreated {
		t.Fatalf("duplicate: %d %s", w.Code, w.Body.String())
	}
	dup := decode[models.ComputeModule](t, w)
	if dup.ID == created.ID {
		t.Fatal("duplicate should produce a new ID")
	}
	if dup.ExecutionMode != models.ExecutionModePipeline {
		t.Fatal("duplicate should inherit execution mode")
	}

	// Archive + restore round-trip.
	w = doJSON(t, r, http.MethodPost, "/api/v1/compute-modules/"+created.ID.String()+"/archive", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("archive: %d %s", w.Code, w.Body.String())
	}
	archived := decode[models.ComputeModule](t, w)
	if !archived.IsArchived() {
		t.Fatal("module should be archived")
	}

	// Restore.
	w = doJSON(t, r, http.MethodPost, "/api/v1/compute-modules/"+created.ID.String()+"/restore", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	restored := decode[models.ComputeModule](t, w)
	if restored.State != models.LifecycleActive {
		t.Fatal("module should be active after restore")
	}

	// Delete.
	w = doJSON(t, r, http.MethodDelete, "/api/v1/compute-modules/"+created.ID.String(), nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(t, r, http.MethodGet, "/api/v1/compute-modules/"+created.ID.String(), nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestAnonymousRequestRejected(t *testing.T) {
	store := repo.NewMemoryRepository()
	state := &handler.State{Repo: store}
	r := chi.NewRouter()
	r.Post("/api/v1/compute-modules", state.Create)

	w := doJSON(t, r, http.MethodPost, "/api/v1/compute-modules", handler.CreateComputeModuleRequest{
		Name:          "anon",
		ProjectID:     uuid.New(),
		ExecutionMode: models.ExecutionModeFunction,
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}
