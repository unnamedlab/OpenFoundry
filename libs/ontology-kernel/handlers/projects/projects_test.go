package projects

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	ontologykernel "github.com/openfoundry/openfoundry-go/libs/ontology-kernel"
	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

func sampleClaims() *authmw.Claims {
	return &authmw.Claims{Sub: uuid.Nil, Email: "test@example.com"}
}

func withClaims(claims *authmw.Claims, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := authmw.ContextWithClaims(r.Context(), claims)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// libs/ontology-kernel/src/handlers/projects.rs — every endpoint
// returns 401 when the request carries no Claims.
func TestEndpointsRequireClaims(t *testing.T) {
	state := &ontologykernel.AppState{}
	pid := uuid.New().String()
	uid := uuid.New().String()
	rid := uuid.New().String()
	bid := uuid.New().String()
	prid := uuid.New().String()

	cases := []struct {
		method, path, body string
		fn                 http.HandlerFunc
	}{
		{http.MethodGet, "/ontology/projects", ``, ListProjects(state)},
		{http.MethodPost, "/ontology/projects", `{"slug":"x"}`, CreateProject(state)},
		{http.MethodGet, "/ontology/projects/" + pid, ``, GetProject(state)},
		{http.MethodPatch, "/ontology/projects/" + pid, `{}`, UpdateProject(state)},
		{http.MethodDelete, "/ontology/projects/" + pid, ``, DeleteProject(state)},
		{http.MethodGet, "/ontology/projects/" + pid + "/memberships", ``, ListProjectMemberships(state)},
		{http.MethodPost, "/ontology/projects/" + pid + "/memberships", `{}`, UpsertProjectMembership(state)},
		{http.MethodDelete, "/ontology/projects/" + pid + "/memberships/" + uid, ``, DeleteProjectMembership(state)},
		{http.MethodGet, "/ontology/projects/" + pid + "/resources", ``, ListProjectResources(state)},
		{http.MethodPost, "/ontology/projects/" + pid + "/resources", `{}`, BindProjectResource(state)},
		{http.MethodDelete, "/ontology/projects/" + pid + "/resources/object_type/" + rid, ``, UnbindProjectResource(state)},
		{http.MethodGet, "/ontology/projects/" + pid + "/working-state", ``, GetProjectWorkingState(state)},
		{http.MethodPut, "/ontology/projects/" + pid + "/working-state", `{}`, ReplaceProjectWorkingState(state)},
		{http.MethodGet, "/ontology/projects/" + pid + "/branches", ``, ListProjectBranches(state)},
		{http.MethodPost, "/ontology/projects/" + pid + "/branches", `{}`, CreateProjectBranch(state)},
		{http.MethodPatch, "/ontology/projects/" + pid + "/branches/" + bid, `{}`, UpdateProjectBranch(state)},
		{http.MethodGet, "/ontology/projects/" + pid + "/proposals", ``, ListProjectProposals(state)},
		{http.MethodPost, "/ontology/projects/" + pid + "/proposals", `{}`, CreateProjectProposal(state)},
		{http.MethodPatch, "/ontology/projects/" + pid + "/proposals/" + prid, `{}`, UpdateProjectProposal(state)},
		{http.MethodGet, "/ontology/projects/" + pid + "/migrations", ``, ListProjectMigrations(state)},
		{http.MethodPost, "/ontology/projects/" + pid + "/migrations", `{}`, CreateProjectMigration(state)},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		tc.fn.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}

// libs/ontology-kernel/src/handlers/projects.rs — Mount registers
// every endpoint at the documented path / verb.
func TestMountRegistersEveryRoute(t *testing.T) {
	r := chi.NewRouter()
	Mount(r, &ontologykernel.AppState{})

	got := map[string]bool{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+route] = true
		return nil
	})
	want := []string{
		"GET /ontology/projects",
		"POST /ontology/projects",
		"GET /ontology/projects/{id}",
		"PATCH /ontology/projects/{id}",
		"DELETE /ontology/projects/{id}",
		"GET /ontology/projects/{id}/memberships",
		"POST /ontology/projects/{id}/memberships",
		"DELETE /ontology/projects/{id}/memberships/{user_id}",
		"GET /ontology/projects/{id}/resources",
		"POST /ontology/projects/{id}/resources",
		"DELETE /ontology/projects/{id}/resources/{resource_kind}/{resource_id}",
		"GET /ontology/projects/{id}/working-state",
		"PUT /ontology/projects/{id}/working-state",
		"GET /ontology/projects/{id}/branches",
		"POST /ontology/projects/{id}/branches",
		"PATCH /ontology/projects/{id}/branches/{branch_id}",
		"GET /ontology/projects/{id}/proposals",
		"POST /ontology/projects/{id}/proposals",
		"PATCH /ontology/projects/{id}/proposals/{proposal_id}",
		"GET /ontology/projects/{id}/migrations",
		"POST /ontology/projects/{id}/migrations",
	}
	for _, key := range want {
		assert.True(t, got[key], "missing route: %s", key)
	}
}

// libs/ontology-kernel/src/handlers/projects.rs `normalize_slug`.
// Pinned cases: trim + lower-case, reject empty / whitespace, reject
// invalid characters, reject leading / trailing hyphen.
func TestNormalizeSlug(t *testing.T) {
	got, err := NormalizeSlug("  My-Slug-001 ", "slug")
	assert.NoError(t, err)
	assert.Equal(t, "my-slug-001", got)

	_, err = NormalizeSlug("   ", "slug")
	assert.ErrorContains(t, err, "slug is required")

	_, err = NormalizeSlug("nope!", "slug")
	assert.ErrorContains(t, err, "lowercase letters, digits, and hyphens")

	_, err = NormalizeSlug("-nope", "slug")
	assert.ErrorContains(t, err, "cannot start or end with a hyphen")

	_, err = NormalizeSlug("nope-", "slug")
	assert.ErrorContains(t, err, "cannot start or end with a hyphen")
}

// libs/ontology-kernel/src/handlers/projects.rs `normalize_optional_slug`.
// nil / whitespace input → ("", false, nil) so the caller knows to
// leave the column untouched.
func TestNormalizeOptionalSlug(t *testing.T) {
	v, has, err := NormalizeOptionalSlug(nil, "workspace_slug")
	assert.NoError(t, err)
	assert.False(t, has)
	assert.Equal(t, "", v)

	whitespace := "   "
	v, has, err = NormalizeOptionalSlug(&whitespace, "workspace_slug")
	assert.NoError(t, err)
	assert.False(t, has)

	value := "  My-Workspace "
	v, has, err = NormalizeOptionalSlug(&value, "workspace_slug")
	assert.NoError(t, err)
	assert.True(t, has)
	assert.Equal(t, "my-workspace", v)

	bad := "with spaces"
	_, _, err = NormalizeOptionalSlug(&bad, "workspace_slug")
	assert.ErrorContains(t, err, "lowercase letters, digits, and hyphens")
}

// libs/ontology-kernel/src/handlers/projects.rs `normalize_branch_name`
// — same alphabet as slug PLUS `/`.
func TestNormalizeBranchName(t *testing.T) {
	got, err := NormalizeBranchName(" Feature/My-Branch ")
	assert.NoError(t, err)
	assert.Equal(t, "feature/my-branch", got)

	_, err = NormalizeBranchName("  ")
	assert.ErrorContains(t, err, "branch name is required")

	_, err = NormalizeBranchName("with spaces")
	assert.ErrorContains(t, err, "lowercase letters, digits, hyphens, and slashes")
}

// libs/ontology-kernel/src/handlers/projects.rs `ensure_project_owner_or_admin`.
func TestEnsureProjectOwnerOrAdmin(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	project := &models.OntologyProject{OwnerID: owner}

	// Owner passes.
	assert.NoError(t, ensureProjectOwnerOrAdmin(project, &authmw.Claims{Sub: owner}))

	// Admin passes.
	assert.NoError(t, ensureProjectOwnerOrAdmin(project, &authmw.Claims{Sub: other, Roles: []string{"admin"}}))

	// Non-owner non-admin rejects with the verbatim Rust string.
	err := ensureProjectOwnerOrAdmin(project, &authmw.Claims{Sub: other})
	assert.ErrorContains(t, err, "forbidden: only the ontology project owner can manage memberships or delete the project")
}

// libs/ontology-kernel/src/handlers/projects.rs — Create rejects an
// invalid slug body before touching the DB.
func TestCreateProjectRejectsInvalidSlug(t *testing.T) {
	state := &ontologykernel.AppState{}
	body, _ := json.Marshal(models.CreateOntologyProjectRequest{Slug: "  "})
	req := httptest.NewRequest(http.MethodPost, "/ontology/projects", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), CreateProject(state)).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "slug is required")
}

// libs/ontology-kernel/src/handlers/projects.rs — pathUUID rejects
// malformed path values with 400.
func TestPathUUIDRejectsMalformed(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	r.Get("/ontology/projects/{id}", GetProject(state))
	req := httptest.NewRequest(http.MethodGet, "/ontology/projects/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid path id")
}

// libs/ontology-kernel/src/handlers/projects.rs — BindProjectResource
// rejects an unknown resource_kind from the body with 400 and the
// verbatim project_access error string.
func TestBindProjectResourceRejectsUnknownKind(t *testing.T) {
	state := &ontologykernel.AppState{}
	r := chi.NewRouter()
	r.Post("/ontology/projects/{id}/resources", BindProjectResource(state))
	body, _ := json.Marshal(models.BindOntologyProjectResourceRequest{
		ResourceKind: "garbage",
		ResourceID:   uuid.New(),
	})
	req := httptest.NewRequest(http.MethodPost,
		"/ontology/projects/"+uuid.New().String()+"/resources",
		strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	withClaims(sampleClaims(), r).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_kind 'garbage' is not supported")
}

// Round-trip ListOntologyProjectsResponse wire shape.
func TestListProjectsResponseShape(t *testing.T) {
	resp := models.ListOntologyProjectsResponse{
		Data:    []models.OntologyProject{},
		Total:   0,
		Page:    1,
		PerPage: 50,
	}
	b, err := json.Marshal(resp)
	assert.NoError(t, err)
	assert.Equal(t, `{"data":[],"total":0,"page":1,"per_page":50}`, string(b))
}
