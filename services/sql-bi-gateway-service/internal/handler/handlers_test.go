package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/sql-bi-gateway-service/internal/repo"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// fakeRepo is an in-memory SavedQueries impl for unit tests.
type fakeRepo struct {
	createIn  models.SavedQuery
	createOut models.SavedQuery
	createErr error

	listOut []models.SavedQuery
	listErr error

	deleted   uuid.UUID
	deleteErr error
}

func (f *fakeRepo) Create(_ context.Context, in models.SavedQuery) (models.SavedQuery, error) {
	f.createIn = in
	if f.createErr != nil {
		return models.SavedQuery{}, f.createErr
	}
	out := f.createOut
	if out.ID == uuid.Nil {
		out = in
		out.ID = uuid.New()
		out.CreatedAt = time.Now().UTC()
		out.UpdatedAt = out.CreatedAt
	}
	return out, nil
}

func (f *fakeRepo) List(_ context.Context, _ string, _, _ int64) ([]models.SavedQuery, error) {
	return f.listOut, f.listErr
}

func (f *fakeRepo) Delete(_ context.Context, id uuid.UUID) error {
	f.deleted = id
	return f.deleteErr
}

func newReq(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, target, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func withClaims(req *http.Request, sub uuid.UUID) *http.Request {
	return req.WithContext(authmw.ContextWithClaims(req.Context(), &authmw.Claims{
		Sub: sub, EXP: time.Now().Add(time.Hour).Unix(),
	}))
}

func TestCreateSavedQueryDerivesOwnerFromClaims(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{}
	h := New(fake, discardLogger())
	user := uuid.New()
	desc := "test"
	sql := "SELECT 1"
	body := models.CreateSavedQueryRequest{Name: "n", Description: &desc, SQL: &sql}
	req := withClaims(newReq(t, http.MethodPost, "/api/v1/queries/saved", body), user)
	w := httptest.NewRecorder()
	h.CreateSavedQuery(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status: got %d body=%s", w.Code, w.Body.String())
	}
	if fake.createIn.OwnerID != user {
		t.Fatalf("owner: got %s want %s", fake.createIn.OwnerID, user)
	}
	if fake.createIn.Name != "n" {
		t.Fatalf("name: got %q", fake.createIn.Name)
	}
	if fake.createIn.SQL != "SELECT 1" {
		t.Fatalf("sql: got %q", fake.createIn.SQL)
	}
}

func TestCreateSavedQuerySeedDatasetRIDFillsSQL(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{}
	h := New(fake, discardLogger())
	body := models.CreateSavedQueryRequest{Name: "seeded"}
	req := withClaims(newReq(t, http.MethodPost,
		"/api/v1/queries/saved?seed_dataset_rid=ds.foo.bar", body),
		uuid.New())
	w := httptest.NewRecorder()
	h.CreateSavedQuery(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status: %d", w.Code)
	}
	if !strings.Contains(fake.createIn.SQL, "ds.foo.bar") {
		t.Fatalf("expected seed-prefilled sql, got %q", fake.createIn.SQL)
	}
}

func TestCreateSavedQueryValidationError(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{createErr: errors.Join(repo.ErrValidation, errors.New("name is required"))}
	h := New(fake, discardLogger())
	body := models.CreateSavedQueryRequest{}
	req := withClaims(newReq(t, http.MethodPost, "/api/v1/queries/saved", body), uuid.New())
	w := httptest.NewRecorder()
	h.CreateSavedQuery(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", w.Code)
	}
}

func TestCreateSavedQueryStubBranchWhenNoRepo(t *testing.T) {
	t.Parallel()
	h := New(nil, discardLogger())
	sql := "SELECT 1"
	req := withClaims(newReq(t, http.MethodPost, "/api/v1/queries/saved",
		models.CreateSavedQueryRequest{Name: "n", SQL: &sql}), uuid.New())
	w := httptest.NewRecorder()
	h.CreateSavedQuery(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status: %d", w.Code)
	}
	var got models.SavedQuery
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatalf("stub did not allocate id")
	}
}

func TestListSavedQueriesPaginated(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{listOut: []models.SavedQuery{{ID: uuid.New(), Name: "a"}}}
	h := New(fake, discardLogger())
	req := withClaims(newReq(t, http.MethodGet,
		"/api/v1/queries/saved?page=2&per_page=5", nil), uuid.New())
	w := httptest.NewRecorder()
	h.ListSavedQueries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	var got map[string]any
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["page"].(float64) != 2 {
		t.Fatalf("page: %v", got["page"])
	}
	if got["per_page"].(float64) != 5 {
		t.Fatalf("per_page: %v", got["per_page"])
	}
}

func TestListSavedQueriesPerPageClamps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want float64
	}{{0, 1}, {-3, 1}, {1000, 100}, {50, 50}}
	for _, tc := range cases {
		fake := &fakeRepo{}
		h := New(fake, discardLogger())
		req := withClaims(newReq(t, http.MethodGet,
			"/api/v1/queries/saved?per_page="+strconv.FormatFloat(tc.in, 'f', -1, 64), nil), uuid.New())
		w := httptest.NewRecorder()
		h.ListSavedQueries(w, req)
		var got map[string]any
		_ = json.NewDecoder(w.Body).Decode(&got)
		if got["per_page"].(float64) != tc.want {
			t.Fatalf("per_page %v → got %v want %v", tc.in, got["per_page"], tc.want)
		}
	}
}

func TestDeleteSavedQueryNotFound(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{deleteErr: repo.ErrNotFound}
	h := mountDelete(fake)
	target := "/api/v1/queries/saved/" + uuid.New().String()
	req := withClaims(newReq(t, http.MethodDelete, target, nil), uuid.New())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", w.Code)
	}
}

func TestDeleteSavedQueryBadID(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{}
	h := mountDelete(fake)
	req := withClaims(newReq(t, http.MethodDelete, "/api/v1/queries/saved/not-a-uuid", nil), uuid.New())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", w.Code)
	}
}

func TestDeleteSavedQueryHappyPath(t *testing.T) {
	t.Parallel()
	fake := &fakeRepo{}
	h := mountDelete(fake)
	id := uuid.New()
	req := withClaims(newReq(t, http.MethodDelete, "/api/v1/queries/saved/"+id.String(), nil), uuid.New())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status: %d", w.Code)
	}
	if fake.deleted != id {
		t.Fatalf("id: got %s want %s", fake.deleted, id)
	}
}

func TestIsSafeRID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"ds.foo.bar", true},
		{"ds_foo-bar.0", true},
		{"DS.UPPER", true},
		{"ds foo", false},
		{"ds;DROP", false},
		{"ds'or'1='1", false},
	}
	for _, tc := range cases {
		if got := IsSafeRID(tc.in); got != tc.want {
			t.Errorf("IsSafeRID(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

// mountDelete bolts the DeleteSavedQuery handler onto a tiny chi router
// so URL params resolve in tests.
func mountDelete(repo *fakeRepo) http.Handler {
	r := chi.NewRouter()
	h := New(repo, discardLogger())
	r.Delete("/api/v1/queries/saved/{id}", h.DeleteSavedQuery)
	return r
}
