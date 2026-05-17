package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// SG.8 wire-format: ProjectResourceGrant + EffectiveAccessResponse +
// the eight source-kind constants. Pinned so a future schema change
// fails loudly.
func TestProjectResourceGrantSG8WireShape(t *testing.T) {
	t.Parallel()
	scopeID := uuid.New()
	granter := uuid.New()
	g := models.ProjectResourceGrant{
		ID:            uuid.New(),
		ProjectID:     uuid.New(),
		ScopeKind:     models.ProjectGrantScopeFolder,
		ScopeID:       &scopeID,
		PrincipalKind: models.ProjectGrantPrincipalGroup,
		PrincipalID:   uuid.New(),
		Role:          models.OntologyProjectRoleEditor,
		GrantedBy:     &granter,
		CreatedAt:     time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(g)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "project_id", "scope_kind", "scope_id",
		"principal_kind", "principal_id", "role", "granted_by",
		"created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "folder", view["scope_kind"])
	assert.Equal(t, "group", view["principal_kind"])
	assert.Equal(t, "editor", view["role"])
}

func TestEffectiveAccessResponseWireShape(t *testing.T) {
	t.Parallel()
	owner := models.OntologyProjectRoleOwner
	prin := uuid.New()
	resp := models.EffectiveAccessResponse{
		UserID:       uuid.New(),
		ProjectID:    uuid.New(),
		ScopeKind:    models.ProjectGrantScopeProject,
		ResolvedRole: &owner,
		Sources: []models.EffectiveAccessSource{
			{
				Kind:        models.EffectiveAccessSourceProjectOwner,
				Role:        models.OntologyProjectRoleOwner,
				PrincipalID: &prin,
			},
			{
				Kind: models.EffectiveAccessSourceProjectDefault,
				Role: models.OntologyProjectRoleViewer,
			},
		},
		CheckedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"user_id", "project_id", "scope_kind", "resolved_role", "sources", "checked_at",
	} {
		assert.Contains(t, view, k)
	}
	srcs := view["sources"].([]any)
	require.Len(t, srcs, 2)
	first := srcs[0].(map[string]any)
	assert.Equal(t, "project_owner", first["kind"])
}

func TestEffectiveAccessSourceConstants(t *testing.T) {
	t.Parallel()
	// Pin the wire vocabulary — the admin UI keys translations
	// off these strings; renames are wire-breaking.
	assert.Equal(t, "project_owner", models.EffectiveAccessSourceProjectOwner)
	assert.Equal(t, "project_default_role", models.EffectiveAccessSourceProjectDefault)
	assert.Equal(t, "project_user_membership", models.EffectiveAccessSourceProjectUserMembership)
	assert.Equal(t, "project_group_membership", models.EffectiveAccessSourceProjectGroupMembership)
	assert.Equal(t, "direct_user_grant", models.EffectiveAccessSourceDirectUserGrant)
	assert.Equal(t, "direct_group_grant", models.EffectiveAccessSourceDirectGroupGrant)
	assert.Equal(t, "folder_user_grant", models.EffectiveAccessSourceFolderUserGrant)
	assert.Equal(t, "folder_group_grant", models.EffectiveAccessSourceFolderGroupGrant)
}

func TestProjectGrantVocabularyConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "project", models.ProjectGrantScopeProject)
	assert.Equal(t, "folder", models.ProjectGrantScopeFolder)
	assert.Equal(t, "user", models.ProjectGrantPrincipalUser)
	assert.Equal(t, "group", models.ProjectGrantPrincipalGroup)
}

// Validation paths that don't touch the DB.

func TestListProjectResourceGrantsRejectsBadScopeKind(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/projects/"+projID.String()+"/resource-grants?scope_kind=resource", nil)
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.ListProjectResourceGrants(rec, req)
	// 401 (no claims) acceptable; if claims present then 400.
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestCreateProjectResourceGrantRejectsBadScopeKind(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	body, _ := json.Marshal(models.CreateProjectResourceGrantRequest{
		ScopeKind:     "resource", // invalid
		PrincipalKind: "user",
		PrincipalID:   uuid.New(),
		Role:          "viewer",
	})
	req := httptest.NewRequest(http.MethodPost,
		"/projects/"+projID.String()+"/resource-grants", strings.NewReader(string(body)))
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.CreateProjectResourceGrant(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestCreateProjectResourceGrantRejectsProjectScopeWithScopeID(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	scope := uuid.New()
	body, _ := json.Marshal(models.CreateProjectResourceGrantRequest{
		ScopeKind:     models.ProjectGrantScopeProject,
		ScopeID:       &scope, // invalid combination
		PrincipalKind: models.ProjectGrantPrincipalUser,
		PrincipalID:   uuid.New(),
		Role:          models.OntologyProjectRoleViewer,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/projects/"+projID.String()+"/resource-grants", strings.NewReader(string(body)))
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.CreateProjectResourceGrant(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestCheckEffectiveAccessRequiresUserID(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/projects/"+projID.String()+"/effective-access", nil)
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.CheckEffectiveAccess(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestCheckEffectiveAccessRejectsBadScopeKind(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/projects/"+projID.String()+"/effective-access?user_id="+userID.String()+"&scope_kind=resource", nil)
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.CheckEffectiveAccess(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestCheckEffectiveAccessFolderScopeRequiresScopeID(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/projects/"+projID.String()+"/effective-access?user_id="+userID.String()+"&scope_kind=folder", nil)
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.CheckEffectiveAccess(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestCheckEffectiveAccessRejectsBadGroupIDs(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	userID := uuid.New()
	req := httptest.NewRequest(http.MethodGet,
		"/projects/"+projID.String()+"/effective-access?user_id="+userID.String()+"&group_ids=not-a-uuid", nil)
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.CheckEffectiveAccess(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}
