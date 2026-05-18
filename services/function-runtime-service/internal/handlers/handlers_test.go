package handlers_test

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
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/executor"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/function-runtime-service/internal/repo"
)

// ─── test fixture: in-router stub auth ────────────────────────────────

func authStub(tenant, actor uuid.UUID) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := &authmw.Claims{Sub: actor, OrgID: &tenant}
			next.ServeHTTP(w, r.WithContext(authmw.ContextWithClaims(r.Context(), c)))
		})
	}
}

func buildHandlers(tb testing.TB, ex executor.Executor) (*handlers.Handlers, *repo.MemoryStore) {
	tb.Helper()
	store := repo.NewMemoryStore()
	h := &handlers.Handlers{
		Store:          store,
		Exec:           ex,
		DefaultTimeout: 5 * time.Second,
		MaxTimeout:     30 * time.Second,
		Now:            func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
		NewID:          func() uuid.UUID { return ids.New() },
		// run async work inline so the test does not race.
		AsyncQueue: func(fn func()) { fn() },
	}
	return h, store
}

func buildRouter(tenant, actor uuid.UUID, h *handlers.Handlers) http.Handler {
	r := chi.NewRouter()
	r.Use(authStub(tenant, actor))
	r.Route("/api/v1/functions", func(api chi.Router) {
		api.Get("/runs", h.ListRuns)
		api.Get("/runs/{run_id}", h.GetRun)
		api.Post("/", h.CreateFunction)
		api.Get("/", h.ListFunctions)
		api.Get("/{id}", h.GetFunction)
		api.Post("/{id}/versions", h.PublishVersion)
		api.Post("/{id}/activate", h.Activate)
		api.Post("/{id}/deprecate", h.Deprecate)
		api.Post("/{id}/invoke", h.Invoke)
		api.Post("/{id}/invoke-async", h.InvokeAsync)
	})
	return r
}

func do(t *testing.T, r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ─── tests ────────────────────────────────────────────────────────────

func TestCreateAndGetFunction(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{output: []byte(`{"ok":true}`)})
	r := buildRouter(tenant, actor, h)

	w := do(t, r, http.MethodPost, "/api/v1/functions", `{
        "namespace":"billing","name":"compute","runtime":"ts",
        "source_uri":"inline:console.log('{}')","entry_point":"handler"
    }`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", w.Code, w.Body.String())
	}
	var fn models.FunctionDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &fn); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if fn.TenantID != tenant {
		t.Fatalf("tenant mismatch: %s vs %s", fn.TenantID, tenant)
	}
	if fn.LatestVersion != 1 {
		t.Fatalf("expected latest_version=1 after create with source_uri, got %d", fn.LatestVersion)
	}

	g := do(t, r, http.MethodGet, "/api/v1/functions/"+fn.ID.String(), "")
	if g.Code != http.StatusOK {
		t.Fatalf("get: status=%d body=%s", g.Code, g.Body.String())
	}
}

func TestPublishActivateInvoke_SyncSuccess(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{output: []byte(`{"echo":1}`), dur: 5 * time.Millisecond})
	r := buildRouter(tenant, actor, h)

	created := do(t, r, http.MethodPost, "/api/v1/functions",
		`{"namespace":"ns","name":"f","runtime":"ts"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var fn models.FunctionDefinition
	_ = json.Unmarshal(created.Body.Bytes(), &fn)

	pub := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/versions",
		`{"source_uri":"inline:x","entry_point":"h"}`)
	if pub.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", pub.Code, pub.Body.String())
	}

	act := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/activate?version=1", "")
	if act.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", act.Code, act.Body.String())
	}

	inv := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke",
		`{"input":{"x":1}}`)
	if inv.Code != http.StatusOK {
		t.Fatalf("invoke: %d %s", inv.Code, inv.Body.String())
	}
	var run models.FunctionRun
	if err := json.Unmarshal(inv.Body.Bytes(), &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	if run.Status != models.RunStatusSucceeded {
		t.Fatalf("run status: %s", run.Status)
	}
	if !bytes.Contains(run.Output, []byte(`{"echo":1}`)) {
		t.Fatalf("run output: %s", run.Output)
	}
}

func TestInvoke_FailureMapsTo500(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{err: domain.ErrExecutionFailed})
	r := buildRouter(tenant, actor, h)
	fn := mustCreateAndActivate(t, r)

	inv := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke",
		`{"input":{}}`)
	if inv.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", inv.Code, inv.Body.String())
	}
	var run models.FunctionRun
	_ = json.Unmarshal(inv.Body.Bytes(), &run)
	if run.Status != models.RunStatusFailed {
		t.Fatalf("expected RunStatusFailed, got %s", run.Status)
	}
}

func TestInvoke_TimeoutMapsTo504(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{err: domain.ErrExecutionTimeout})
	r := buildRouter(tenant, actor, h)
	fn := mustCreateAndActivate(t, r)

	inv := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke",
		`{"input":{}}`)
	if inv.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d body=%s", inv.Code, inv.Body.String())
	}
}

func TestInvoke_RequiresActiveOrExplicitVersion(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{output: []byte(`{}`)})
	r := buildRouter(tenant, actor, h)

	// Create function + 1 version, do NOT activate.
	c := do(t, r, http.MethodPost, "/api/v1/functions", `{"namespace":"n","name":"f","runtime":"ts"}`)
	var fn models.FunctionDefinition
	_ = json.Unmarshal(c.Body.Bytes(), &fn)
	_ = do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/versions",
		`{"source_uri":"inline:x","entry_point":"h"}`)

	noVer := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke", `{}`)
	if noVer.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected 412 with no active version, got %d", noVer.Code)
	}

	explicit := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke",
		`{"version":1}`)
	if explicit.Code != http.StatusOK {
		t.Fatalf("explicit version invoke: %d %s", explicit.Code, explicit.Body.String())
	}
}

func TestInvokeAsync_Returns202AndPersistsRun(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, store := buildHandlers(t, fakeExec{output: []byte(`{"ok":1}`)})
	r := buildRouter(tenant, actor, h)
	fn := mustCreateAndActivate(t, r)

	w := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke-async",
		`{"input":{}}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", w.Code, w.Body.String())
	}
	var queued models.FunctionRun
	_ = json.Unmarshal(w.Body.Bytes(), &queued)

	got, err := store.GetRun(context.Background(), queued.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Status != models.RunStatusSucceeded {
		t.Fatalf("async run should be finished by AsyncQueue inline, status=%s", got.Status)
	}
}

func TestNotImplementedRuntimeMapsTo501(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{err: executor.ErrNotImplemented})
	r := buildRouter(tenant, actor, h)
	fn := mustCreateAndActivate(t, r)

	w := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/invoke", `{}`)
	if w.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestListFunctions_FiltersByTenant(t *testing.T) {
	t.Parallel()
	tenantA, tenantB := ids.New(), ids.New()
	actor := ids.New()
	h, store := buildHandlers(t, fakeExec{})

	// Create one function per tenant directly on the store.
	_ = store.CreateFunction(context.Background(), &models.FunctionDefinition{
		TenantID: tenantA, Namespace: "n", Name: "a", Runtime: models.RuntimeTypeScript,
	})
	_ = store.CreateFunction(context.Background(), &models.FunctionDefinition{
		TenantID: tenantB, Namespace: "n", Name: "b", Runtime: models.RuntimeTypeScript,
	})

	r := buildRouter(tenantA, actor, h)
	w := do(t, r, http.MethodGet, "/api/v1/functions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d body=%s", w.Code, w.Body.String())
	}
	var got []models.FunctionDefinition
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("tenant filter leaked: %+v", got)
	}
}

func TestCreateFunction_RejectsInvalidRuntime(t *testing.T) {
	t.Parallel()
	tenant := ids.New()
	actor := ids.New()
	h, _ := buildHandlers(t, fakeExec{})
	r := buildRouter(tenant, actor, h)

	w := do(t, r, http.MethodPost, "/api/v1/functions",
		`{"namespace":"n","name":"f","runtime":"rust"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// ─── helpers + fakes ──────────────────────────────────────────────────

func mustCreateAndActivate(t *testing.T, r http.Handler) models.FunctionDefinition {
	t.Helper()
	c := do(t, r, http.MethodPost, "/api/v1/functions", `{"namespace":"n","name":"f","runtime":"ts"}`)
	if c.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", c.Code, c.Body.String())
	}
	var fn models.FunctionDefinition
	_ = json.Unmarshal(c.Body.Bytes(), &fn)
	_ = do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/versions",
		`{"source_uri":"inline:x","entry_point":"h"}`)
	act := do(t, r, http.MethodPost, "/api/v1/functions/"+fn.ID.String()+"/activate?version=1", "")
	if act.Code != http.StatusOK {
		t.Fatalf("activate: %d %s", act.Code, act.Body.String())
	}
	var activated models.FunctionDefinition
	_ = json.Unmarshal(act.Body.Bytes(), &activated)
	return activated
}

type fakeExec struct {
	output []byte
	err    error
	dur    time.Duration
}

func (f fakeExec) Execute(_ context.Context, _ models.FunctionDefinition, _ models.FunctionVersion, _ []byte) (*executor.Result, error) {
	res := &executor.Result{Output: f.output, Duration: f.dur}
	if f.err != nil {
		switch {
		case errors.Is(f.err, domain.ErrExecutionTimeout):
			return res, f.err
		default:
			return res, f.err
		}
	}
	return res, nil
}
