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

func TestMarkingCategorySG11WireShape(t *testing.T) {
	t.Parallel()
	cat := models.MarkingCategoryResponse{
		MarkingCategory: models.MarkingCategory{
			ID:          uuid.New(),
			Slug:        "export-control",
			DisplayName: "Export control",
			Description: "Export-controlled markings",
			Visibility:  models.MarkingCategoryVisibilityHidden,
			Metadata:    json.RawMessage(`{"steward":"security"}`),
			CreatedBy:   uuid.New(),
			CreatedAt:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		},
		Permissions: []models.MarkingCategoryPermission{
			{
				CategoryID:    uuid.New(),
				PrincipalKind: models.MarkingCategoryPrincipalUser,
				PrincipalID:   uuid.New(),
				Permission:    models.MarkingCategoryPermissionAdministrator,
				GrantedBy:     uuid.New(),
				CreatedAt:     time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	out, err := json.Marshal(cat)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"id", "slug", "display_name", "description", "visibility",
		"metadata", "created_by", "created_at", "updated_at", "permissions",
	} {
		assert.Contains(t, view, key)
	}
	permissions := view["permissions"].([]any)
	p0 := permissions[0].(map[string]any)
	for _, key := range []string{"category_id", "principal_kind", "principal_id", "permission", "granted_by", "created_at"} {
		assert.Contains(t, p0, key)
	}
}

func TestMarkingCategoryConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "visible", models.MarkingCategoryVisibilityVisible)
	assert.Equal(t, "hidden", models.MarkingCategoryVisibilityHidden)
	assert.Equal(t, "administrator", models.MarkingCategoryPermissionAdministrator)
	assert.Equal(t, "viewer", models.MarkingCategoryPermissionViewer)
	assert.Equal(t, "category.delete_blocked", models.MarkingCategoryAuditDeleteBlocked)
}

func TestMarkingSG12WireShape(t *testing.T) {
	t.Parallel()
	marking := models.MarkingResponse{
		Marking: models.Marking{
			ID:          uuid.New(),
			CategoryID:  uuid.New(),
			Slug:        "pii",
			DisplayName: "PII",
			Description: "Personally identifiable information",
			Metadata:    json.RawMessage(`{"training":"privacy-101"}`),
			CreatedBy:   uuid.New(),
			CreatedAt:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			UpdatedAt:   time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
		},
		Permissions: []models.MarkingPermission{
			{
				MarkingID:     uuid.New(),
				PrincipalKind: models.MarkingCategoryPrincipalGroup,
				PrincipalID:   uuid.New(),
				Permission:    models.MarkingPermissionMember,
				GrantedBy:     uuid.New(),
				CreatedAt:     time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	out, err := json.Marshal(marking)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"id", "category_id", "slug", "display_name", "description",
		"metadata", "created_by", "created_at", "updated_at", "permissions",
	} {
		assert.Contains(t, view, key)
	}
	p0 := view["permissions"].([]any)[0].(map[string]any)
	for _, key := range []string{"marking_id", "principal_kind", "principal_id", "permission", "granted_by", "created_at"} {
		assert.Contains(t, p0, key)
	}
}

func TestMarkingSG12Constants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "administrator", models.MarkingPermissionAdministrator)
	assert.Equal(t, "remover", models.MarkingPermissionRemover)
	assert.Equal(t, "applier", models.MarkingPermissionApplier)
	assert.Equal(t, "member", models.MarkingPermissionMember)
	assert.Equal(t, "marking.delete_blocked", models.MarkingAuditDeleteBlocked)
	assert.Equal(t, "marking.category_move_blocked", models.MarkingAuditCategoryMoveBlocked)
}

func TestMarkingPermissionCheckSG13WireShape(t *testing.T) {
	t.Parallel()
	resp := models.MarkingPermissionCheckResponse{
		MarkingID:                     uuid.New(),
		PrincipalID:                   uuid.New(),
		CanManage:                     true,
		CanApply:                      true,
		CanRemove:                     false,
		IsMember:                      false,
		CanAccessMarkedData:           false,
		ResourceUpdateMarkingsAllowed: true,
		ExpandAccessAllowed:           false,
		CanApplyToResource:            true,
		CanRemoveFromResource:         false,
		Reasons:                       []string{"apply permission does not imply marking membership"},
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"marking_id", "principal_id", "can_manage", "can_apply",
		"can_remove", "is_member", "can_access_marked_data",
		"resource_update_markings_allowed", "expand_access_allowed",
		"can_apply_to_resource", "can_remove_from_resource", "reasons",
	} {
		assert.Contains(t, view, key)
	}
	assert.Equal(t, false, view["is_member"])
	assert.Equal(t, false, view["can_access_marked_data"])
}

func TestResourceMarkingSG13WireShape(t *testing.T) {
	t.Parallel()
	item := models.ResourceMarking{
		ID:           uuid.New(),
		ResourceKind: "dataset",
		ResourceID:   "ri.foundry.dataset.example",
		MarkingID:    uuid.New(),
		SourceKind:   "direct",
		Metadata:     json.RawMessage(`{"reason":"classification"}`),
		AppliedBy:    uuid.New(),
		AppliedAt:    time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(item)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"id", "resource_kind", "resource_id", "marking_id",
		"source_kind", "metadata", "applied_by", "applied_at",
	} {
		assert.Contains(t, view, key)
	}
}

func TestEffectiveResourceMarkingSG14WireShape(t *testing.T) {
	t.Parallel()
	markingID := uuid.New()
	item := models.EffectiveResourceMarking{
		MarkingID:   markingID,
		MarkingName: "PII",
		RequiredFor: []string{models.ResourceMarkingRequiredForResourceAccess, models.ResourceMarkingRequiredForDataAccess},
		Sources: []models.EffectiveResourceMarkingSource{
			{
				SourceKind:              models.EffectiveResourceMarkingSourceLineage,
				RequiredFor:             models.ResourceMarkingRequiredForDataAccess,
				SourceResourceKind:      "dataset",
				SourceResourceID:        "ri.foundry.dataset.raw",
				DirectResourceMarkingID: uuid.New(),
				RelationKinds:           []string{models.ResourceMarkingRelationLineage},
				Path: []models.ResourceMarkingPathHop{
					{ResourceKind: "dataset", ResourceID: "ri.foundry.dataset.raw"},
					{ResourceKind: "dataset", ResourceID: "ri.foundry.dataset.derived", RelationKind: models.ResourceMarkingRelationLineage},
				},
				Metadata: json.RawMessage(`{"lineage":"openlineage"}`),
			},
		},
	}
	out, err := json.Marshal(item)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{"marking_id", "marking_name", "required_for", "sources"} {
		assert.Contains(t, view, key)
	}
	source := view["sources"].([]any)[0].(map[string]any)
	for _, key := range []string{"source_kind", "required_for", "source_resource_kind", "source_resource_id", "direct_resource_marking_id", "relation_kinds", "path", "metadata"} {
		assert.Contains(t, source, key)
	}
}

func TestResourceAccessCheckSG14WireShape(t *testing.T) {
	t.Parallel()
	resp := models.ResourceAccessCheckResponse{
		PrincipalID:           uuid.New(),
		ResourceKind:          "dataset",
		ResourceID:            "ri.foundry.dataset.derived",
		ResourceAccessAllowed: true,
		DataAccessAllowed:     false,
		AccessRequirements: []models.ResourceAccessRequirementResult{
			{
				Kind:      models.ResourceAccessRequirementScopedSession,
				Label:     "Scoped session",
				Status:    models.ResourceAccessRequirementStatusPassed,
				Satisfied: true,
				Present:   []string{"PII"},
			},
			{
				Kind:      models.ResourceAccessRequirementResourceMarking,
				Label:     "Resource markings",
				Status:    models.ResourceAccessRequirementStatusPassed,
				Satisfied: true,
			},
		},
		AdditionalDataRequirements: []models.ResourceAccessRequirementResult{
			{
				Kind:      models.ResourceAccessRequirementDataMarking,
				Label:     "Lineage-derived data markings",
				Status:    models.ResourceAccessRequirementStatusFailed,
				Satisfied: false,
				Missing:   []string{"PII"},
			},
		},
		MarkingResults: []models.ResourceAccessMarkingResult{
			{
				MarkingID:                uuid.New(),
				MarkingName:              "PII",
				RequiredFor:              []string{models.ResourceMarkingRequiredForDataAccess},
				Satisfied:                false,
				MembershipSatisfied:      true,
				ScopedSessionSatisfied:   false,
				ScopedSessionRequirement: true,
			},
		},
		CheckedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"principal_id", "resource_kind", "resource_id", "resource_access_allowed",
		"data_access_allowed", "access_requirements", "additional_data_requirements",
		"effective_markings", "marking_results", "checked_at",
	} {
		assert.Contains(t, view, key)
	}
	result := view["marking_results"].([]any)[0].(map[string]any)
	for _, key := range []string{"membership_satisfied", "scoped_session_satisfied", "scoped_session_requirement"} {
		assert.Contains(t, result, key)
	}
}

func TestPublishMarkingBuildSG15WireShape(t *testing.T) {
	t.Parallel()
	markingID := uuid.New()
	resp := models.PublishMarkingBuildResponse{
		Allowed:       false,
		Applied:       false,
		DryRun:        false,
		BuildID:       "build-1",
		TransactionID: "txn-1",
		OutputDiffs: []models.MarkingBuildOutputDiff{
			{
				OutputResource: models.MarkingBuildResourceRef{ResourceKind: "dataset", ResourceID: "ri.output"},
				Removed: []models.MarkingDiffEntry{
					{MarkingID: markingID, MarkingName: "PII", RequiredFor: []string{models.ResourceMarkingRequiredForDataAccess}},
				},
			},
		},
		BlockedRemovals: []models.MarkingBuildBlockedRemoval{
			{
				OutputResource: models.MarkingBuildResourceRef{ResourceKind: "dataset", ResourceID: "ri.output"},
				MarkingID:      markingID,
				MarkingName:    "PII",
				RequiredFor:    []string{models.ResourceMarkingRequiredForDataAccess},
				Permission: models.MarkingPermissionCheckResponse{
					MarkingID:   markingID,
					PrincipalID: uuid.New(),
					CanRemove:   false,
					Reasons:     []string{"principal lacks remove marking permission"},
				},
			},
		},
		CheckedAt: time.Date(2026, 5, 17, 0, 0, 0, 0, time.UTC),
	}
	out, err := json.Marshal(resp)
	require.NoError(t, err)
	var view map[string]any
	require.NoError(t, json.Unmarshal(out, &view))
	for _, key := range []string{
		"allowed", "applied", "dry_run", "build_id", "transaction_id",
		"output_diffs", "blocked_removals", "checked_at",
	} {
		assert.Contains(t, view, key)
	}
	diff := view["output_diffs"].([]any)[0].(map[string]any)
	for _, key := range []string{"output_resource", "before", "after", "added", "removed", "unchanged"} {
		assert.Contains(t, diff, key)
	}
}

func TestCreateMarkingCategoryRejectsEmptyRequiredFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/marking-categories", strings.NewReader(`{}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateMarkingCategory(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "display_name")
}

func TestCreateMarkingCategoryRejectsBadVisibility(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/marking-categories",
		strings.NewReader(`{"slug":"pii","display_name":"PII","visibility":"private"}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateMarkingCategory(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "visibility")
}

func TestCreateMarkingCategoryRejectsArrayMetadata(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/marking-categories",
		strings.NewReader(`{"slug":"pii","display_name":"PII","metadata":[]}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateMarkingCategory(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "metadata")
}

func TestCreateMarkingCategoryRejectsNullMetadata(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/marking-categories",
		strings.NewReader(`{"slug":"pii","display_name":"PII","metadata":null}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateMarkingCategory(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "metadata")
}

func TestUpsertMarkingCategoryPermissionRejectsBadPermission(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	categoryID := uuid.New()
	body := `{"principal_kind":"user","principal_id":"` + uuid.NewString() + `","permission":"owner"}`
	req := httptest.NewRequest(http.MethodPut, "/marking-categories/"+categoryID.String()+"/permissions", strings.NewReader(body))
	req = markingCategoryWithChiParam(req, "id", categoryID.String())
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.UpsertMarkingCategoryPermission(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "permission")
}

func TestDeleteMarkingCategoryPermissionRejectsBadPrincipalKind(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	categoryID := uuid.New()
	req := httptest.NewRequest(http.MethodDelete,
		"/marking-categories/"+categoryID.String()+"/permissions/team/"+uuid.NewString()+"/viewer",
		nil,
	)
	req = markingCategoryWithChiParam(req, "id", categoryID.String())
	req = markingCategoryWithChiParam(req, "principal_kind", "team")
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.DeleteMarkingCategoryPermission(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "principal_kind")
}

func TestCreateMarkingRejectsEmptyRequiredFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	categoryID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/marking-categories/"+categoryID.String()+"/markings", strings.NewReader(`{}`))
	req = markingCategoryWithChiParam(req, "id", categoryID.String())
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateMarking(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "display_name")
}

func TestCreateMarkingRejectsNullMetadata(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	categoryID := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/marking-categories/"+categoryID.String()+"/markings",
		strings.NewReader(`{"slug":"pii","display_name":"PII","metadata":null}`))
	req = markingCategoryWithChiParam(req, "id", categoryID.String())
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CreateMarking(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "metadata")
}

func TestUpsertMarkingPermissionRejectsBadPermission(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	markingID := uuid.New()
	body := `{"principal_kind":"user","principal_id":"` + uuid.NewString() + `","permission":"owner"}`
	req := httptest.NewRequest(http.MethodPut, "/markings/"+markingID.String()+"/permissions", strings.NewReader(body))
	req = markingCategoryWithChiParam(req, "id", markingID.String())
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.UpsertMarkingPermission(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "permission")
}

func TestMoveMarkingCategoryRejectsMissingTarget(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	markingID := uuid.New()
	req := httptest.NewRequest(http.MethodPut, "/markings/"+markingID.String()+"/category", strings.NewReader(`{}`))
	req = markingCategoryWithChiParam(req, "id", markingID.String())
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.MoveMarkingCategory(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "target_category_id")
}

func TestApplyResourceMarkingRejectsMissingFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/resource-markings", strings.NewReader(`{}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.ApplyResourceMarking(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_kind")
}

func TestRemoveResourceMarkingRejectsMissingFields(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/resource-markings/remove", strings.NewReader(`{}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.RemoveResourceMarking(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_kind")
}

func TestListResourceMarkingsRejectsMissingQuery(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/resource-markings", nil)
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.ListResourceMarkings(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_kind")
}

func TestUpsertResourceMarkingEdgeRejectsSelfEdge(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	body := `{"source_resource_kind":"dataset","source_resource_id":"rid1","target_resource_kind":"dataset","target_resource_id":"rid1","relation_kind":"lineage"}`
	req := httptest.NewRequest(http.MethodPut, "/resource-marking-edges", strings.NewReader(body))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.UpsertResourceMarkingEdge(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "relation_kind")
}

func TestGetEffectiveResourceMarkingsRejectsMissingQuery(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodGet, "/resource-markings/effective", nil)
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.GetEffectiveResourceMarkings(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_kind")
}

func TestCheckResourceAccessRejectsMissingResource(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/resource-access:check", strings.NewReader(`{}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.CheckResourceAccess(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "resource_kind")
}

func TestPublishMarkingBuildRejectsMissingResources(t *testing.T) {
	t.Parallel()
	h := &handlers.Handlers{}
	req := httptest.NewRequest(http.MethodPost, "/resource-marking-builds:publish", strings.NewReader(`{}`))
	req = markingCategoryWithClaims(req)
	rec := httptest.NewRecorder()
	h.PublishMarkingBuildOutput(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "input_resources")
}

func markingCategoryWithChiParam(r *http.Request, key, value string) *http.Request {
	rctx, _ := r.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func markingCategoryWithClaims(r *http.Request) *http.Request {
	c := &authmw.Claims{
		Sub:         uuid.New(),
		Permissions: []string{"markings:read", "markings:write", "markings:audit"},
	}
	return r.WithContext(authmw.ContextWithClaims(r.Context(), c))
}
