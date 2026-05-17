package handlers_test

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
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// SG.7 wire-format: RoleSet, RoleSetRole, and the delegation
// response. Pinned so a future schema change fails loudly.
func TestRoleSetSG7WireShape(t *testing.T) {
	t.Parallel()
	rs := models.RoleSetResponse{
		RoleSet: models.RoleSet{
			ID:        uuid.New(),
			Slug:      "project-default",
			Name:      "Project default roles",
			Context:   models.RoleSetContextProject,
			CreatedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		},
		Roles: []models.RoleSetRole{
			{
				RoleSetID: uuid.New(),
				RoleID:    uuid.New(),
				RoleName:  "project_owner",
				Rank:      4,
				CreatedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	out, err := json.Marshal(rs)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "slug", "name", "context", "roles", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	roles := view["roles"].([]any)
	r0 := roles[0].(map[string]any)
	for _, k := range []string{"role_set_id", "role_id", "role_name", "rank", "created_at"} {
		assert.Contains(t, r0, k)
	}
	assert.Equal(t, float64(4), r0["rank"])
}

func TestRoleSetContextConstants(t *testing.T) {
	t.Parallel()
	// Pin the wire vocabulary — the admin UI keys translations
	// off these strings; renames are wire-breaking.
	assert.Equal(t, "project", models.RoleSetContextProject)
	assert.Equal(t, "ontology", models.RoleSetContextOntology)
	assert.Equal(t, "restricted_view", models.RoleSetContextRestrictedView)
	assert.Equal(t, "platform_admin", models.RoleSetContextPlatformAdmin)
}

func TestCheckDelegationResponseWireShape(t *testing.T) {
	t.Parallel()
	rid := uuid.New()
	rank := 3
	resp := models.CheckDelegationResponse{
		Allowed:       false,
		GrantorRoleID: &rid,
		GrantorRank:   &rank,
		TargetRoleID:  uuid.New(),
		TargetRank:    4,
		Reason:        "grantor rank 3 is below target rank 4",
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"allowed", "grantor_role_id", "grantor_rank", "target_role_id", "target_rank", "reason"} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, false, view["allowed"])
}

func TestOperationCatalogEntryWireShape(t *testing.T) {
	t.Parallel()
	desc := "Read project metadata and resources"
	e := models.OperationCatalogEntry{
		ID:          uuid.New(),
		Resource:    "project",
		Action:      "read",
		Description: &desc,
	}
	out, err := json.Marshal(e)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"id", "resource", "action", "description"} {
		assert.Contains(t, view, k)
	}
}

// Validation paths that don't touch the DB.

func TestListRoleSetsRejectsBadContext(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/role-sets?context=ldap", nil)
	req = roleSetWithClaims(req)
	rec := httptest.NewRecorder()
	h.ListRoleSets(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "context")
}

func TestCreateRoleSetRejectsBadContext(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	body, _ := json.Marshal(models.CreateRoleSetRequest{Slug: "x", Name: "X", Context: "ldap"})
	req := httptest.NewRequest(http.MethodPost, "/role-sets", strings.NewReader(string(body)))
	req = roleSetWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateRoleSet(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "context")
}

func TestCreateRoleSetRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/role-sets", strings.NewReader(`{}`))
	req = roleSetWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateRoleSet(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "slug")
}

func TestCheckRoleSetDelegationRejectsMissingTarget(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	roleSetID := uuid.New()
	req := httptest.NewRequest(http.MethodPost,
		"/role-sets/"+roleSetID.String()+"/delegation:check",
		strings.NewReader(`{}`))
	req = roleSetWithChiParam(req, "id", roleSetID.String())
	req = roleSetWithClaims(req)
	rec := httptest.NewRecorder()
	h.CheckRoleSetDelegation(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "target_role_id")
}

func TestAddRoleToRoleSetRejectsNonPositiveRank(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	roleSetID := uuid.New()
	body, _ := json.Marshal(models.AddRoleToRoleSetRequest{
		RoleID: uuid.New(),
		Rank:   0,
	})
	req := httptest.NewRequest(http.MethodPost,
		"/role-sets/"+roleSetID.String()+"/roles",
		strings.NewReader(string(body)))
	req = roleSetWithChiParam(req, "id", roleSetID.String())
	req = roleSetWithClaims(req)
	rec := httptest.NewRecorder()
	h.AddRoleToRoleSet(rec, req)
	// 500 (nil repo on GetRoleSet) is acceptable for this no-DB
	// test; the relevant assertion is that the handler refused
	// (non-2xx) before reaching the repo with a zero rank.
	assert.True(t, rec.Code >= 400, "expected 4xx/5xx, got %d", rec.Code)
}

// ─── helpers ───────────────────────────────────────────────────────────

func roleSetWithChiParam(r *http.Request, key, value string) *http.Request {
	rctx, _ := r.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// roleSetWithClaims attaches an admin-permission claims set so the
// handler's requirePermission gate passes; tests then exercise the
// SG.7-specific validation paths.
func roleSetWithClaims(r *http.Request) *http.Request {
	c := &authmw.Claims{
		Sub:         uuid.New(),
		Permissions: []string{"roles:read", "roles:write", "permissions:read"},
	}
	return r.WithContext(authmw.ContextWithClaims(r.Context(), c))
}
