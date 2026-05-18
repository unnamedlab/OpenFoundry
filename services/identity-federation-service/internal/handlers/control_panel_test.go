package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func controlPanelClaims(permissions ...string) *authmw.Claims {
	return &authmw.Claims{
		Sub:         uuid.New(),
		Email:       "admin@example.com",
		Permissions: permissions,
	}
}

func TestControlPanelRequiresReadPermission(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodGet, "/control-panel", nil)
	rec := httptest.NewRecorder()
	h.Get(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/control-panel", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), controlPanelClaims("users:read")))
	rec = httptest.NewRecorder()
	h.Get(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestControlPanelUpdatePersistsInProcess(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	claims := controlPanelClaims("control_panel:write")
	req := httptest.NewRequest(http.MethodPut, "/control-panel",
		strings.NewReader(`{"platform_name":"OpenFoundry Enterprise","maintenance_mode":true,"restricted_operations":["dataset.delete"]}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), claims))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req = httptest.NewRequest(http.MethodGet, "/control-panel", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), controlPanelClaims("control_panel:read")))
	rec = httptest.NewRecorder()
	h.Get(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var settings ControlPanelSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings))
	require.Equal(t, "OpenFoundry Enterprise", settings.PlatformName)
	require.True(t, settings.MaintenanceMode)
	require.Equal(t, []string{"dataset.delete"}, settings.RestrictedOperations)
}

func TestControlPanelWriteRequiresWritePermission(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	req := httptest.NewRequest(http.MethodPut, "/control-panel", strings.NewReader(`{"platform_name":"x"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), controlPanelClaims("control_panel:read")))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestControlPanelUpdatesScopedSessionConfig(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	claims := controlPanelClaims("control_panel:write")
	req := httptest.NewRequest(http.MethodPut, "/control-panel", strings.NewReader(`{
		"scoped_sessions":{
			"enabled":true,
			"allow_no_scoped_session":true,
			"always_show_selector":true,
			"allowed_bypass_groups":["security-admins","security-admins"],
			"presets":[
				{"id":"pii-review","name":"PII review","required_markings":["public","pii"],"allowed_markings":["public","pii"],"enabled":true}
			]
		}
	}`)).WithContext(authmw.ContextWithClaims(context.Background(), claims))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var settings ControlPanelSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings))
	require.True(t, settings.ScopedSessions.Enabled)
	require.True(t, settings.ScopedSessions.AlwaysShowSelector)
	require.Equal(t, []string{"security-admins"}, settings.ScopedSessions.AllowedBypassGroups)
	require.Len(t, settings.ScopedSessions.Presets, 1)
	require.Equal(t, []string{"public", "pii"}, settings.ScopedSessions.Presets[0].AllowedMarkings)
}

func TestApplicationAccessEvaluationBlocksMatchingGroup(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	cfg := defaultApplicationAccessConfig()
	cfg.Rules = []ApplicationAccessRule{{
		ID:             "hide-beta-ai",
		Name:           "Hide beta AIP tools from basic users",
		Effect:         "block",
		ApplicationIDs: []string{"ai"},
		GroupIDs:       []string{"basic-users"},
		Enabled:        true,
		Reason:         "Reduce application surface for curated users.",
	}}
	body, err := json.Marshal(UpdateControlPanelRequest{ApplicationAccess: &cfg})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPut, "/control-panel", strings.NewReader(string(body))).
		WithContext(authmw.ContextWithClaims(context.Background(), controlPanelClaims("control_panel:write")))
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	claims := controlPanelClaims()
	claims.Attributes = json.RawMessage(`{"group_ids":["basic-users"]}`)
	req = httptest.NewRequest(http.MethodPost, "/application-access/evaluate", strings.NewReader(`{"application_id":"ai"}`)).
		WithContext(authmw.ContextWithClaims(context.Background(), claims))
	rec = httptest.NewRecorder()
	h.EvaluateApplicationAccess(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp ApplicationAccessEvaluateResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Decisions, 1)
	require.False(t, resp.Decisions[0].Visible)
	require.Equal(t, "blocked_by_rule", resp.Decisions[0].Decision)
	require.Equal(t, []string{"hide-beta-ai"}, resp.Decisions[0].MatchedRuleIDs)
	require.True(t, resp.Decisions[0].UXScopeOnly)
}

func TestApplicationAccessDefaultHiddenRequiresAllowRule(t *testing.T) {
	t.Parallel()
	cfg := defaultApplicationAccessConfig()
	cfg.DefaultVisibility = "hidden"
	cfg.Rules = []ApplicationAccessRule{{
		ID:             "platform-admins-see-control-panel",
		Name:           "Platform admins see Control Panel",
		Effect:         "allow",
		ApplicationIDs: []string{"control-panel"},
		GroupIDs:       []string{"platform-admins"},
		Enabled:        true,
	}}
	claims := controlPanelClaims()
	resp, err := evaluateApplicationAccess(cfg, ApplicationAccessEvaluateRequest{
		ApplicationIDs: []string{"control-panel", "datasets"},
		GroupIDs:       []string{"platform-admins"},
	}, claims)

	require.NoError(t, err)
	require.Len(t, resp.Decisions, 2)
	require.True(t, resp.Decisions[0].Visible)
	require.Equal(t, "allowed_by_rule", resp.Decisions[0].Decision)
	require.False(t, resp.Decisions[1].Visible)
	require.Equal(t, "hidden_by_default", resp.Decisions[1].Decision)
}

func TestApplicationAccessApprovalPolicyChangeRequiresDistinctReviewer(t *testing.T) {
	t.Parallel()
	settings := ControlPanelSettings{ApplicationAccess: defaultApplicationAccessConfig()}
	author := controlPanelClaims("control_panel:write")
	cfg := defaultApplicationAccessConfig()
	cfg.ApprovalPolicy = ApplicationAccessApprovalPolicy{
		Mode:                             "review_required",
		ReviewerUserIDs:                  []string{"reviewer-1"},
		ReviewerGroupIDs:                 []string{},
		RequireDistinctReviewerForPolicy: true,
		Instructions:                     "Require peer approval for application access.",
	}

	require.NoError(t, applyApplicationAccessUpdate(&settings, cfg, author))
	require.Equal(t, "self_approve", settings.ApplicationAccess.ApprovalPolicy.Mode)
	require.Len(t, settings.ApplicationAccess.ChangeRequests, 1)
	require.Equal(t, "pending", settings.ApplicationAccess.ChangeRequests[0].Status)
	require.Equal(t, "approval_policy", settings.ApplicationAccess.ChangeRequests[0].Kind)

	err := decideApplicationAccessChangeRequest(
		&settings,
		settings.ApplicationAccess.ChangeRequests[0].ID,
		ApplicationAccessDecisionRequest{Decision: "approved"},
		author,
	)
	require.Error(t, err)

	reviewer := controlPanelClaims("control_panel:write")
	reviewer.Email = "reviewer@example.com"
	require.NoError(t, decideApplicationAccessChangeRequest(
		&settings,
		settings.ApplicationAccess.ChangeRequests[0].ID,
		ApplicationAccessDecisionRequest{Decision: "approved", Comment: "Looks good."},
		reviewer,
	))
	require.Equal(t, "review_required", settings.ApplicationAccess.ApprovalPolicy.Mode)
	require.Equal(t, "approved", settings.ApplicationAccess.ChangeRequests[0].Status)
	require.Len(t, settings.ApplicationAccess.History, 1)
	require.Equal(t, "approved", settings.ApplicationAccess.History[0].Action)
}

func TestMemberDiscoveryUpdateRecordsHistoryAndWarning(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	orgID := uuid.New().String()
	claims := controlPanelClaims("control_panel:write")
	req := httptest.NewRequest(http.MethodPut, "/control-panel", strings.NewReader(`{
		"member_discovery":{
			"default_discover_users":true,
			"default_discover_groups":true,
			"organizations":[{
				"organization_id":"`+orgID+`",
				"organization_slug":"consumer-a",
				"discover_users":false,
				"discover_groups":false,
				"consumer_mode_boundary":true,
				"notes":"Consumer privacy boundary"
			}]
		}
	}`)).WithContext(authmw.ContextWithClaims(context.Background(), claims))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var settings ControlPanelSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings))
	require.Equal(t, MemberDiscoveryWarning, settings.MemberDiscovery.Warning)
	require.Len(t, settings.MemberDiscovery.Organizations, 1)
	require.False(t, settings.MemberDiscovery.Organizations[0].DiscoverUsers)
	require.False(t, settings.MemberDiscovery.Organizations[0].DiscoverGroups)
	require.True(t, settings.MemberDiscovery.Organizations[0].ConsumerModeBoundary)
	require.NotNil(t, settings.MemberDiscovery.Organizations[0].UpdatedBy)
	require.Len(t, settings.MemberDiscovery.History, 1)
	require.Equal(t, orgID, settings.MemberDiscovery.History[0].OrganizationID)
}

func TestMemberDiscoveryBlocksNonAdminButPreservesAdminVisibility(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	cp := NewControlPanel()
	cp.settings.MemberDiscovery = MemberDiscoveryConfig{
		DefaultDiscoverUsers:  true,
		DefaultDiscoverGroups: true,
		Warning:               MemberDiscoveryWarning,
		Organizations: []MemberDiscoveryOrganizationConfig{{
			OrganizationID: orgID.String(),
			DiscoverUsers:  false,
			DiscoverGroups: false,
		}},
	}
	rbac := &RBAC{ControlPanel: cp}

	ordinary := controlPanelClaims()
	ordinary.OrgID = &orgID
	req := httptest.NewRequest(http.MethodGet, "/users/search", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), ordinary))
	rec := httptest.NewRecorder()
	require.False(t, rbac.allowUserDiscovery(rec, req, &orgID))
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "member_discovery_disabled")

	admin := controlPanelClaims("users:read")
	admin.OrgID = &orgID
	req = httptest.NewRequest(http.MethodGet, "/users/search", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), admin))
	rec = httptest.NewRecorder()
	require.True(t, rbac.allowUserDiscovery(rec, req, &orgID))
	require.Equal(t, http.StatusOK, rec.Code)

	groupAdmin := controlPanelClaims("groups:read")
	groupAdmin.OrgID = &orgID
	req = httptest.NewRequest(http.MethodGet, "/groups/search", nil).
		WithContext(authmw.ContextWithClaims(context.Background(), groupAdmin))
	rec = httptest.NewRecorder()
	require.True(t, rbac.allowGroupDiscovery(rec, req, &orgID))
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestFileAccessPresetUpdateRecordsHistoryAndNormalizesOrder(t *testing.T) {
	t.Parallel()
	h := NewControlPanel()
	claims := controlPanelClaims("control_panel:write")
	req := httptest.NewRequest(http.MethodPut, "/control-panel", strings.NewReader(`{
		"file_access_presets":{
			"enabled":true,
			"guest_organization_behavior":"primary_organization",
			"presets":[
				{"id":"restricted","title":"Restricted","marking_ids":["pii","pii","export"],"default_order":2,"enabled":true,"supported_resource_kinds":["Project"]},
				{"id":"public","title":"Public","marking_ids":[],"default_order":1,"enabled":true}
			]
		}
	}`)).WithContext(authmw.ContextWithClaims(context.Background(), claims))
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var settings ControlPanelSettings
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&settings))
	require.Equal(t, FileAccessPresetWarning, settings.FileAccessPresets.Warning)
	require.Len(t, settings.FileAccessPresets.Presets, 2)
	require.Equal(t, "public", settings.FileAccessPresets.Presets[0].ID)
	require.Equal(t, []string{"pii", "export"}, settings.FileAccessPresets.Presets[1].MarkingIDs)
	require.Equal(t, []string{"project"}, settings.FileAccessPresets.Presets[1].SupportedResourceKinds)
	require.NotNil(t, settings.FileAccessPresets.Presets[1].UpdatedBy)
	require.Len(t, settings.FileAccessPresets.History, 1)
	require.Equal(t, "primary_organization", settings.FileAccessPresets.GuestOrganizationBehavior)
}

func TestVisibleFileAccessPresetsRequiresApplyPermissionForEveryMarking(t *testing.T) {
	t.Parallel()
	orgID := uuid.New()
	cfg := FileAccessPresetConfig{
		Enabled:                   true,
		GuestOrganizationBehavior: FileAccessPresetGuestPrimaryOrganization,
		Presets: []FileAccessPreset{
			{ID: "public", Title: "Public", Enabled: true, DefaultOrder: 1, OrganizationIDs: []string{orgID.String()}},
			{ID: "pii", Title: "PII", Enabled: true, DefaultOrder: 2, MarkingIDs: []string{"pii", "export"}, OrganizationIDs: []string{orgID.String()}, SupportedResourceKinds: []string{"project"}},
			{ID: "dataset-only", Title: "Dataset", Enabled: true, DefaultOrder: 3, MarkingIDs: []string{"pii"}, OrganizationIDs: []string{orgID.String()}, SupportedResourceKinds: []string{"dataset"}},
		},
	}
	claims := controlPanelClaims()
	claims.OrgID = &orgID
	claims.Attributes = json.RawMessage(`{"apply_marking_ids":["pii"]}`)

	resp, err := visibleFileAccessPresets(cfg, FileAccessPresetVisibilityRequest{ResourceKind: "project"}, claims)

	require.NoError(t, err)
	require.Equal(t, orgID.String(), resp.EffectiveOrganizationID)
	require.Equal(t, "public", resp.DefaultPresetID)
	require.Len(t, resp.Presets, 1)
	require.Equal(t, "public", resp.Presets[0].ID)
	require.Equal(t, 2, resp.FilteredPresetCount)

	claims.Attributes = json.RawMessage(`{"apply_marking_ids":["pii","export"]}`)
	resp, err = visibleFileAccessPresets(cfg, FileAccessPresetVisibilityRequest{ResourceKind: "project"}, claims)
	require.NoError(t, err)
	require.Len(t, resp.Presets, 2)
	require.Equal(t, "pii", resp.Presets[1].ID)
}

func TestVisibleFileAccessPresetsUsesPrimaryOrganizationForGuests(t *testing.T) {
	t.Parallel()
	hostOrgID := uuid.New()
	primaryOrgID := uuid.New()
	cfg := FileAccessPresetConfig{
		Enabled:                   true,
		GuestOrganizationBehavior: FileAccessPresetGuestPrimaryOrganization,
		Presets: []FileAccessPreset{
			{ID: "host", Title: "Host", Enabled: true, DefaultOrder: 1, OrganizationIDs: []string{hostOrgID.String()}},
			{ID: "primary", Title: "Primary", Enabled: true, DefaultOrder: 2, OrganizationIDs: []string{primaryOrgID.String()}},
		},
	}
	sessionKind := "guest_session"
	claims := controlPanelClaims("markings:apply")
	claims.OrgID = &hostOrgID
	claims.SessionKind = &sessionKind

	resp, err := visibleFileAccessPresets(cfg, FileAccessPresetVisibilityRequest{
		OrganizationID:        hostOrgID.String(),
		PrimaryOrganizationID: primaryOrgID.String(),
	}, claims)

	require.NoError(t, err)
	require.Equal(t, primaryOrgID.String(), resp.EffectiveOrganizationID)
	require.Len(t, resp.Presets, 1)
	require.Equal(t, "primary", resp.Presets[0].ID)
}
