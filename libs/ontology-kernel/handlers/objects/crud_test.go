package objects

// crud_test.go pins the wire-format invariants of the 5 CRUD
// handlers ported in crud.go. The DB-backed paths
// (LoadEffectiveProperties + ValidateObjectProperties) sit behind
// `state.DB` which is `*pgxpool.Pool` (no swappable interface yet),
// so create_object / update_object's positive paths are exercised
// here for the early-exit branches that fire BEFORE the DB lookup
// (auth, marking validation, body parse, path validation, merge
// shape). End-to-end create/update tests with a live DB land
// alongside the next slice that adds the testcontainers harness.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/stores"
	storage "github.com/openfoundry/openfoundry-go/libs/storage-abstraction"
)

// ── helpers ──────────────────────────────────────────────────────────

func newCRUDState() *ontologykernel.AppState {
	return &ontologykernel.AppState{
		Stores: stores.NewInMemory(),
	}
}

// withClaims wires the *Claims into the request context — same shape
// the production middleware uses. Pass nil to simulate a request that
// arrived without a claims-stash, which all 5 endpoints must reject
// with 401.
func withClaims(claims *authmw.Claims, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims != nil {
			ctx := authmw.ContextWithClaims(r.Context(), claims)
			r = r.WithContext(ctx)
		}
		h.ServeHTTP(w, r)
	})
}

func sampleClaims() *authmw.Claims {
	return &authmw.Claims{
		Sub:     uuid.New(),
		Email:   "tester@example.com",
		Name:    "Tester",
		Roles:   []string{"admin"}, // admin bypasses marking enforcement
	}
}

func seedObject(t *testing.T, store storage.ObjectStore, tenant storage.TenantId, typeID, objID uuid.UUID, props string) {
	t.Helper()
	now := time.Now().UTC().UnixMilli()
	owner := storage.OwnerId(uuid.NewString())
	_, err := store.Put(context.Background(), storage.Object{
		Tenant:      tenant,
		ID:          storage.ObjectId(objID.String()),
		TypeID:      storage.TypeId(typeID.String()),
		Version:     0,
		Payload:     json.RawMessage(props),
		CreatedAtMs: &now,
		UpdatedAtMs: now,
		Owner:       &owner,
		Markings:    []storage.MarkingId{"public"},
	}, nil)
	require.NoError(t, err)
}

// ── Mount + auth ─────────────────────────────────────────────────────

func TestMount_RegistersAllFiveCRUDRoutes(t *testing.T) {
	t.Parallel()
	r := chi.NewRouter()
	Mount(r, newCRUDState())
	got := map[string]bool{}
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	}))
	want := []string{
		"POST /ontology/types/{type_id}/objects",
		"GET /ontology/types/{type_id}/objects",
		"GET /ontology/types/{type_id}/objects/{obj_id}",
		"PATCH /ontology/types/{type_id}/objects/{obj_id}",
		"DELETE /ontology/types/{type_id}/objects/{obj_id}",
	}
	for _, key := range want {
		assert.True(t, got[key], "missing route: %s", key)
	}
}

func TestEndpointsRequireClaims(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New().String()
	oid := uuid.New().String()
	cases := []struct {
		method, path, body string
		fn                 http.HandlerFunc
	}{
		{http.MethodPost, "/ontology/types/" + tid + "/objects", `{}`, CreateObject(state)},
		{http.MethodGet, "/ontology/types/" + tid + "/objects", ``, ListObjects(state)},
		{http.MethodGet, "/ontology/types/" + tid + "/objects/" + oid, ``, GetObject(state)},
		{http.MethodPatch, "/ontology/types/" + tid + "/objects/" + oid, `{}`, UpdateObject(state)},
		{http.MethodDelete, "/ontology/types/" + tid + "/objects/" + oid, ``, DeleteObject(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

// ── GetObject ────────────────────────────────────────────────────────

func TestGetObject_NotFoundWhenStoreEmpty(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Get("/ontology/types/{type_id}/objects/{obj_id}", GetObject(state))

	req := httptest.NewRequest(http.MethodGet,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetObject_ReturnsStoredObjectForAdmin(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()
	seedObject(t, state.Stores.Objects, "default", tid, oid, `{"name":"alpha"}`)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Get("/ontology/types/{type_id}/objects/{obj_id}", GetObject(state))

	req := httptest.NewRequest(http.MethodGet,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"id":"`+oid.String()+`"`)
	assert.Contains(t, rec.Body.String(), `"object_type_id":"`+tid.String()+`"`)
	assert.Contains(t, rec.Body.String(), `"name":"alpha"`)
}

func TestGetObject_ForbiddenOnInsufficientClearance(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()

	// Seed an object marked "pii" (clearance rank 2 required).
	now := time.Now().UTC().UnixMilli()
	owner := storage.OwnerId(uuid.NewString())
	_, err := state.Stores.Objects.Put(context.Background(), storage.Object{
		Tenant:      "default",
		ID:          storage.ObjectId(oid.String()),
		TypeID:      storage.TypeId(tid.String()),
		Version:     0,
		Payload:     json.RawMessage(`{}`),
		CreatedAtMs: &now,
		UpdatedAtMs: now,
		Owner:       &owner,
		Markings:    []storage.MarkingId{"pii"},
	}, nil)
	require.NoError(t, err)

	// Caller is non-admin with no clearance attribute → default rank 0
	// (public). EnsureObjectAccess must reject with
	// ErrForbiddenClearance.
	claims := &authmw.Claims{
		Sub:   uuid.New(),
		Email: "u@o.test",
		// No Roles → not admin. No Attributes → clearance defaults to 0.
	}

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(claims, next) }).
		Get("/ontology/types/{type_id}/objects/{obj_id}", GetObject(state))

	req := httptest.NewRequest(http.MethodGet,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code, "body=%s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), "insufficient classification clearance")
}

func TestGetObject_BadPathUUIDReturns400(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Get("/ontology/types/{type_id}/objects/{obj_id}", GetObject(state))

	req := httptest.NewRequest(http.MethodGet, "/ontology/types/abc/objects/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── ListObjects ─────────────────────────────────────────────────────

func TestListObjects_EmptyStoreReturnsEnvelope(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Get("/ontology/types/{type_id}/objects", ListObjects(state))

	req := httptest.NewRequest(http.MethodGet, "/ontology/types/"+tid.String()+"/objects", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ListObjectsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Empty(t, resp.Data)
	assert.Equal(t, int64(0), resp.Total)
	assert.Equal(t, int64(1), resp.Page)
	assert.Equal(t, int64(20), resp.PerPage)
}

func TestListObjects_PaginationRespectsPerPage(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	for i := 0; i < 5; i++ {
		seedObject(t, state.Stores.Objects, "default", tid, uuid.New(), `{}`)
	}
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Get("/ontology/types/{type_id}/objects", ListObjects(state))

	req := httptest.NewRequest(http.MethodGet,
		"/ontology/types/"+tid.String()+"/objects?page=1&per_page=2", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ListObjectsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.Data, 2, "per_page=2 must return 2 items")
	assert.Equal(t, int64(5), resp.Total)
	assert.Equal(t, int64(1), resp.Page)
	assert.Equal(t, int64(2), resp.PerPage)
}

func TestListObjects_PerPageClampedTo100(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Get("/ontology/types/{type_id}/objects", ListObjects(state))

	req := httptest.NewRequest(http.MethodGet,
		"/ontology/types/"+tid.String()+"/objects?per_page=9999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ListObjectsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, int64(100), resp.PerPage, "per_page must be clamped to 100")
}

// ── DeleteObject ─────────────────────────────────────────────────────

func TestDeleteObject_NotFound(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()
	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Delete("/ontology/types/{type_id}/objects/{obj_id}", DeleteObject(state))

	req := httptest.NewRequest(http.MethodDelete,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeleteObject_HappyPathReturnsNoContentAndRevisionAppended(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()
	claims := sampleClaims()
	tenant := storage.TenantId("default") // claims.OrgID is nil → tenant = "default"
	seedObject(t, state.Stores.Objects, tenant, tid, oid, `{"a":1}`)

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(claims, next) }).
		Delete("/ontology/types/{type_id}/objects/{obj_id}", DeleteObject(state))

	req := httptest.NewRequest(http.MethodDelete,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	// Object is gone.
	gotten, err := state.Stores.Objects.Get(context.Background(), tenant,
		storage.ObjectId(oid.String()), storage.Strong())
	require.NoError(t, err)
	assert.Nil(t, gotten, "store must no longer contain the deleted object")

	// A "delete" revision landed in the action log.
	page, err := state.Stores.Actions.ListForObject(context.Background(), tenant,
		storage.ObjectId(oid.String()), storage.Page{Size: 10}, storage.Strong())
	require.NoError(t, err)
	require.NotEmpty(t, page.Items)
	assert.Equal(t, "revision", page.Items[0].Kind)
	assert.Contains(t, string(page.Items[0].Payload), `"operation":"delete"`)
}

// ── UpdateObject early-exit branches (no DB) ────────────────────────

func TestUpdateObject_BadJSONBodyReturns400(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Patch("/ontology/types/{type_id}/objects/{obj_id}", UpdateObject(state))

	req := httptest.NewRequest(http.MethodPatch,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(),
		strings.NewReader(`{not-json`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUpdateObject_NotFoundWhenObjectMissing(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()
	oid := uuid.New()

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Patch("/ontology/types/{type_id}/objects/{obj_id}", UpdateObject(state))

	body, _ := json.Marshal(UpdateObjectRequest{Properties: json.RawMessage(`{}`)})
	req := httptest.NewRequest(http.MethodPatch,
		"/ontology/types/"+tid.String()+"/objects/"+oid.String(),
		strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ── CreateObject early-exit branches (no DB) ────────────────────────

func TestCreateObject_RejectsInvalidMarking(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Post("/ontology/types/{type_id}/objects", CreateObject(state))

	bogus := "top-secret"
	body, _ := json.Marshal(CreateObjectRequest{
		Properties: json.RawMessage(`{}`),
		Marking:    &bogus,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/ontology/types/"+tid.String()+"/objects",
		strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid marking")
}

func TestCreateObject_BadJSONBodyReturns400(t *testing.T) {
	t.Parallel()
	state := newCRUDState()
	tid := uuid.New()

	r := chi.NewRouter()
	r.With(func(next http.Handler) http.Handler { return withClaims(sampleClaims(), next) }).
		Post("/ontology/types/{type_id}/objects", CreateObject(state))

	req := httptest.NewRequest(http.MethodPost,
		"/ontology/types/"+tid.String()+"/objects",
		strings.NewReader(`{not-json`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ── mergePatchProperties ────────────────────────────────────────────

func TestMergePatchProperties_ShallowOverrides(t *testing.T) {
	t.Parallel()
	current := json.RawMessage(`{"a":1,"b":2}`)
	patch := json.RawMessage(`{"b":99,"c":3}`)
	got, err := mergePatchProperties(current, patch)
	require.NoError(t, err)
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(got, &out))
	assert.JSONEq(t, `1`, string(out["a"]))
	assert.JSONEq(t, `99`, string(out["b"]))
	assert.JSONEq(t, `3`, string(out["c"]))
}

func TestMergePatchProperties_RejectsNonObjectPatch(t *testing.T) {
	t.Parallel()
	_, err := mergePatchProperties(json.RawMessage(`{}`), json.RawMessage(`[1,2,3]`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JSON object when replace=false")
}
