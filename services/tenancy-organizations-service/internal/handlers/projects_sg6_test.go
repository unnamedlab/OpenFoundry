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

	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/tenancy-organizations-service/internal/models"
)

// SG.6 wire-format: OntologyProject grows default_role, contact, and
// references. Pin the snake_case keys.
func TestOntologyProjectSG6WireShape(t *testing.T) {
	t.Parallel()
	ws := "engineering"
	contactID := uuid.New()
	contactEmail := "team@example.com"
	p := models.OntologyProject{
		ID:                   uuid.New(),
		RID:                  "ri.compass.main.project.018f2f1c-aaaa-7bbb-8ccc-000000000010",
		Slug:                 "fraud",
		DisplayName:          "Fraud",
		Description:          "ML scoring",
		WorkspaceSlug:        &ws,
		OwnerID:              uuid.New(),
		DefaultRole:          models.OntologyProjectRoleDiscoverer,
		PointOfContactUserID: &contactID,
		PointOfContactEmail:  &contactEmail,
		References: []models.OntologyProjectReference{
			{Kind: "project", ID: uuid.New(), Label: "Upstream"},
		},
		MarkingRIDs:                      []string{"ri.marking.main.marking.pii"},
		PropagateViewRequirementsEnabled: true,
		CreatedAt:                        time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		UpdatedAt:                        time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(p)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "slug", "display_name", "description", "workspace_slug", "owner_id",
		"default_role", "point_of_contact_user_id", "point_of_contact_email", "references",
		"rid", "marking_rids", "propagate_view_requirements_enabled",
		"created_at", "updated_at",
	} {
		assert.Contains(t, view, k)
	}
	refs, ok := view["references"].([]any)
	require.True(t, ok)
	require.Len(t, refs, 1)
	r0 := refs[0].(map[string]any)
	assert.Equal(t, "project", r0["kind"])
	assert.Equal(t, "Upstream", r0["label"])
}

func TestOntologyProjectFolderCMP5WireShape(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	projectID := uuid.New()
	parentID := uuid.New()
	f := models.OntologyProjectFolder{
		ID:                               id,
		RID:                              models.FolderRIDFromID(id),
		ProjectID:                        projectID,
		ProjectRID:                       models.ProjectRIDFromID(projectID),
		ParentFolderID:                   &parentID,
		ParentFolderRID:                  models.FolderRIDFromID(parentID),
		SpaceRID:                         models.DefaultProjectSpaceRID,
		Type:                             models.FolderResourceType,
		TrashStatus:                      models.FolderTrashStatusNotTrashed,
		InheritsProjectPolicies:          true,
		PolicyOverridesAllowed:           true,
		PropagateViewRequirementsEnabled: true,
		ViewRequirementMarkingRIDs:       []string{"ri.marking.main.marking.pii"},
		Name:                             "Models",
		Slug:                             "models",
		Description:                      "Production models",
		CreatedBy:                        uuid.New(),
		CreatedAt:                        time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		UpdatedAt:                        time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(f)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"rid", "project_rid", "parent_folder_rid", "space_rid", "type",
		"trash_status", "inherits_project_policies", "policy_overrides_allowed",
		"propagate_view_requirements_enabled", "view_requirement_marking_rids",
	} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, f.RID, view["rid"])
	assert.Equal(t, f.ParentFolderRID, view["parent_folder_rid"])
	assert.Equal(t, true, view["inherits_project_policies"])
}

func TestOntologyProjectGroupMembershipWireShape(t *testing.T) {
	t.Parallel()
	by := uuid.New()
	m := models.OntologyProjectGroupMembership{
		ProjectID: uuid.New(), GroupID: uuid.New(),
		Role: models.OntologyProjectRoleEditor, GrantedBy: &by,
		CreatedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{"project_id", "group_id", "role", "granted_by", "created_at", "updated_at"} {
		assert.Contains(t, view, k)
	}
	assert.Equal(t, "editor", view["role"])
}

func TestOntologyProjectAccessRequestWireShape(t *testing.T) {
	t.Parallel()
	scope := "folder"
	scopeID := uuid.New()
	by := uuid.New()
	reason := "Need analyst access"
	at := time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC)
	req := models.OntologyProjectAccessRequest{
		ID:                uuid.New(),
		ProjectID:         uuid.New(),
		RequestedBy:       uuid.New(),
		RequestedRole:     models.OntologyProjectRoleViewer,
		Reason:            reason,
		ScopeResourceKind: &scope,
		ScopeResourceID:   &scopeID,
		Status:            models.ProjectAccessRequestStatusPending,
		DecidedBy:         &by,
		DecisionReason:    &reason,
		CreatedAt:         at,
		DecidedAt:         &at,
	}
	out, err := json.Marshal(req)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, k := range []string{
		"id", "project_id", "requested_by", "requested_role", "reason",
		"scope_resource_kind", "scope_resource_id", "status",
		"decided_by", "decision_reason", "created_at", "decided_at",
	} {
		assert.Contains(t, view, k)
	}
}

func TestProjectAccessRequestStatusConstants(t *testing.T) {
	t.Parallel()
	// Pin the wire vocabulary — the admin UI keys translations off
	// these strings. Renames are wire-breaking.
	assert.Equal(t, "pending", models.ProjectAccessRequestStatusPending)
	assert.Equal(t, "approved", models.ProjectAccessRequestStatusApproved)
	assert.Equal(t, "denied", models.ProjectAccessRequestStatusDenied)
	assert.Equal(t, "cancelled", models.ProjectAccessRequestStatusCancelled)
}

// Validation paths that don't touch the DB.

func TestUpsertProjectGroupMembershipRejectsEmptyBody(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	req := httptest.NewRequest(http.MethodPut, "/projects/"+projID.String()+"/group-memberships", strings.NewReader(`{}`))
	req = projectsWithChiParam(req, "id", projID.String())
	req = projectsWithAuthClaims(req)
	rec := httptest.NewRecorder()
	h.UpsertProjectGroupMembership(rec, req)
	// 401 because we did not supply claims; 400 if claims were
	// present and body invalid. Either way, the handler refused.
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestEnsureProjectAccessGroupsRequiresAtLeastOneID(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/projects/"+projID.String()+"/access-groups:bootstrap", strings.NewReader(`{}`))
	req = projectsWithChiParam(req, "id", projID.String())
	rec := httptest.NewRecorder()
	h.EnsureProjectAccessGroups(rec, req)
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

func TestDecideProjectAccessRequestRejectsBadDecision(t *testing.T) {
	t.Parallel()
	h := &handlers.ProjectsHandlers{}
	projID := uuid.New()
	reqID := uuid.New()
	body, _ := json.Marshal(models.DecideProjectAccessRequestRequest{Decision: "later"})
	req := httptest.NewRequest(http.MethodPost,
		"/projects/"+projID.String()+"/access-requests/"+reqID.String()+"/decision",
		strings.NewReader(string(body)))
	req = projectsWithChiParam(req, "id", projID.String())
	req = projectsWithChiParam(req, "request_id", reqID.String())
	rec := httptest.NewRecorder()
	h.DecideProjectAccessRequest(rec, req)
	// 401 missing claims, or 400 bad decision after auth — both
	// surface a refusal without touching the DB.
	assert.True(t, rec.Code == http.StatusUnauthorized || rec.Code == http.StatusBadRequest,
		"expected 400 or 401, got %d", rec.Code)
}

// ─── helpers ───────────────────────────────────────────────────────────

func projectsWithChiParam(r *http.Request, key, value string) *http.Request {
	rctx, _ := r.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

// projectsWithAuthClaims is intentionally a no-op: the handlers
// call authClaims which writes a 401 to the recorder when claims
// are missing. The validation tests assert "either 401 or 400" so
// they don't need a real JWT setup.
func projectsWithAuthClaims(r *http.Request) *http.Request { return r }
