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

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// SG.5 wire-format: Group gains kind, display_name, realm,
// organization_id, attributes, rule_query, status, updated_at.
func TestGroupSG5WireShape(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	orgID := uuid.New()
	desc := "Engineering team"
	g := models.Group{
		ID: uuid.New(), Name: "eng", DisplayName: "Engineering",
		Description:    &desc,
		Kind:           models.GroupKindInternal,
		Realm:          "local",
		OrganizationID: &orgID,
		Attributes:     json.RawMessage(`{"region":"eu-west-1"}`),
		Status:         models.GroupStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	out, err := json.Marshal(g)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "name", "display_name", "description", "kind", "realm",
		"organization_id", "attributes", "status", "created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
}

func TestGroupAdminWireShape(t *testing.T) {
	t.Parallel()
	grantedBy := uuid.New()
	a := models.GroupAdmin{
		GroupID:   uuid.New(),
		UserID:    uuid.New(),
		Scope:     models.GroupAdminScopeManage,
		GrantedBy: &grantedBy,
		CreatedAt: time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(a)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"group_id", "user_id", "scope", "granted_by", "created_at"} {
		assert.Contains(t, view, k)
	}
}

func TestGroupMemberWireShape(t *testing.T) {
	t.Parallel()
	exp := time.Date(2026, 6, 14, 0, 0, 0, 0, time.UTC)
	addedBy := uuid.New()
	m := models.GroupMember{
		GroupID:   uuid.New(),
		UserID:    uuid.New(),
		AddedAt:   time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC),
		AddedBy:   &addedBy,
		ExpiresAt: &exp,
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"group_id", "user_id", "added_at", "added_by", "expires_at"} {
		assert.Contains(t, view, k)
	}
}

func TestGroupInspectionWireShape(t *testing.T) {
	t.Parallel()
	insp := models.GroupInspection{
		Group:               models.Group{ID: uuid.New(), Name: "eng", DisplayName: "Engineering", Kind: "internal", Realm: "local", Status: "active"},
		DirectMemberCount:   12,
		ExpiringMemberCount: 3,
		Admins:              []models.GroupAdmin{},
		Parents:             []models.GroupBrief{},
		Children:            []models.GroupBrief{},
		ProjectAccessHint:   "see tenancy-organizations-service",
	}
	out, err := json.Marshal(insp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"group", "direct_member_count", "expiring_member_count",
		"admins", "parents", "children", "project_access_hint",
	} {
		assert.Contains(t, view, k)
	}
}

func TestGroupKindConstants(t *testing.T) {
	t.Parallel()
	// Pin the wire vocabulary.
	assert.Equal(t, "internal", models.GroupKindInternal)
	assert.Equal(t, "external", models.GroupKindExternal)
	assert.Equal(t, "rule_based", models.GroupKindRuleBased)
	assert.Equal(t, "active", models.GroupStatusActive)
	assert.Equal(t, "archived", models.GroupStatusArchived)
	assert.Equal(t, "manage", models.GroupAdminScopeManage)
	assert.Equal(t, "manage_members", models.GroupAdminScopeManageMembers)
}

// Validation paths that don't touch the DB.

func TestSearchGroupsRejectsBadKind(t *testing.T) {
	t.Parallel()
	h := &handlers.RBAC{}
	req := httptest.NewRequest(http.MethodGet, "/groups/search?kind=ldap", nil)
	rec := httptest.NewRecorder()
	h.SearchGroups(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "kind")
}

func TestSearchGroupsRejectsBadStatus(t *testing.T) {
	t.Parallel()
	h := &handlers.RBAC{}
	req := httptest.NewRequest(http.MethodGet, "/groups/search?status=disabled", nil)
	rec := httptest.NewRecorder()
	h.SearchGroups(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "status")
}

func TestSearchGroupsRejectsBadOrgID(t *testing.T) {
	t.Parallel()
	h := &handlers.RBAC{}
	req := httptest.NewRequest(http.MethodGet, "/groups/search?organization_id=not-a-uuid", nil)
	rec := httptest.NewRecorder()
	h.SearchGroups(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "organization_id")
}

func TestAddGroupAdminRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	h := &handlers.RBAC{}
	groupID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/groups/"+groupID.String()+"/admins", strings.NewReader(`{}`))
	req = withChiParam(req, "id", groupID.String())
	rec := httptest.NewRecorder()
	h.AddGroupAdmin(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "user_id")
}

func TestAddGroupAdminRejectsBadScope(t *testing.T) {
	t.Parallel()
	h := &handlers.RBAC{}
	groupID := uuid.New()
	body, _ := json.Marshal(models.CreateGroupAdminRequest{
		UserID: uuid.New(),
		Scope:  ptr("ldap"),
	})
	req := httptest.NewRequest(http.MethodPost, "/groups/"+groupID.String()+"/admins", strings.NewReader(string(body)))
	req = withChiParam(req, "id", groupID.String())
	rec := httptest.NewRecorder()
	h.AddGroupAdmin(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "scope")
}

func ptr[T any](v T) *T { return &v }

// withChiParam returns a new request whose context carries a chi
// RouteContext with the given URL parameter set. Necessary because
// we drive handlers directly (no chi mux) in these unit tests.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
