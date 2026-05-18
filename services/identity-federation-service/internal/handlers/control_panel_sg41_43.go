package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

const CrossOrganizationWarning = "Cross-organization collaboration must expose only configured shared organizations, spaces, resources, and visible groups; guest sessions use primary-organization policy for presets and cannot discover hidden host groups."
const ConsumerModeGovernanceWarning = "Consumer mode restricts navigation and application discovery for UX/privacy boundaries; backend resource, project, application, and OAuth permissions must still be enforced on every request."
const IdentityCacheWarning = "Policy decisions can be affected by cached IdP attributes, group memberships, inactivity state, or object-security inputs until their TTL expires or an explicit invalidation is recorded."

type CrossOrganizationConfig struct {
	Enabled             bool                          `json:"enabled"`
	Warning             string                        `json:"warning"`
	Organizations       []CrossOrganizationBoundary   `json:"organizations"`
	CollaborationGuides []CrossOrganizationSetupGuide `json:"collaboration_guides"`
}

type CrossOrganizationBoundary struct {
	OrganizationID              string   `json:"organization_id"`
	OrganizationSlug            string   `json:"organization_slug,omitempty"`
	PrimaryMemberGroupIDs       []string `json:"primary_member_group_ids"`
	GuestMemberGroupIDs         []string `json:"guest_member_group_ids"`
	DiscoverableOrganizationIDs []string `json:"discoverable_organization_ids"`
	SharedSpaceIDs              []string `json:"shared_space_ids"`
	SharedResourcePrefixes      []string `json:"shared_resource_prefixes"`
	VisibleGroupIDs             []string `json:"visible_group_ids"`
	HiddenInternalGroupIDs      []string `json:"hidden_internal_group_ids"`
	AllowedGuestPresetOrgIDs    []string `json:"allowed_guest_preset_org_ids"`
	IdentityProviderGuidance    string   `json:"identity_provider_guidance,omitempty"`
	SecurityBoundaryDescription string   `json:"security_boundary_description,omitempty"`
}

type CrossOrganizationSetupGuide struct {
	Scenario string   `json:"scenario"`
	Steps    []string `json:"steps"`
}

type CrossOrganizationEvaluateRequest struct {
	HostOrganizationID    string   `json:"host_organization_id"`
	PrimaryOrganizationID string   `json:"primary_organization_id,omitempty"`
	TargetOrganizationID  string   `json:"target_organization_id,omitempty"`
	ResourcePath          string   `json:"resource_path,omitempty"`
	RequestedPresetOrgID  string   `json:"requested_preset_org_id,omitempty"`
	GroupIDs              []string `json:"group_ids,omitempty"`
}

type CrossOrganizationEvaluateResponse struct {
	Warning                 string   `json:"warning"`
	HostOrganizationID      string   `json:"host_organization_id"`
	PrimaryOrganizationID   string   `json:"primary_organization_id,omitempty"`
	GuestSession            bool     `json:"guest_session"`
	CanDiscoverTargetOrg    bool     `json:"can_discover_target_org"`
	CanAccessSharedResource bool     `json:"can_access_shared_resource"`
	CanApplyRequestedPreset bool     `json:"can_apply_requested_preset"`
	VisibleGroupIDs         []string `json:"visible_group_ids"`
	HiddenGroupIDs          []string `json:"hidden_group_ids"`
	RequiredBoundary        string   `json:"required_boundary"`
}

type ConsumerModeGovernanceConfig struct {
	Enabled       bool                         `json:"enabled"`
	Warning       string                       `json:"warning"`
	Organizations []ConsumerOrganizationConfig `json:"organizations"`
	AuthPatterns  []ConsumerAuthPattern        `json:"auth_patterns"`
}

type ConsumerOrganizationConfig struct {
	OrganizationID                 string   `json:"organization_id"`
	OrganizationSlug               string   `json:"organization_slug,omitempty"`
	ConsumerGroupIDs               []string `json:"consumer_group_ids"`
	PlatformAccessRestricted       bool     `json:"platform_access_restricted"`
	UserDiscoveryHidden            bool     `json:"user_discovery_hidden"`
	GroupDiscoveryHidden           bool     `json:"group_discovery_hidden"`
	RequiredApplicationPolicyID    string   `json:"required_application_policy_id,omitempty"`
	AllowedApplicationIDs          []string `json:"allowed_application_ids"`
	ConsumerFacingResourcePrefixes []string `json:"consumer_facing_resource_prefixes"`
	DefaultApplicationURL          string   `json:"default_application_url,omitempty"`
	NavigationRestrictions         []string `json:"navigation_restrictions"`
	MonitoringSignals              []string `json:"monitoring_signals"`
}

type ConsumerAuthPattern struct {
	Pattern  string   `json:"pattern"`
	Controls []string `json:"controls"`
}

type ConsumerModeEvaluateRequest struct {
	OrganizationID string   `json:"organization_id"`
	ApplicationID  string   `json:"application_id,omitempty"`
	ResourcePath   string   `json:"resource_path,omitempty"`
	GroupIDs       []string `json:"group_ids,omitempty"`
}

type ConsumerModeEvaluateResponse struct {
	Warning                     string   `json:"warning"`
	OrganizationID              string   `json:"organization_id"`
	ConsumerMode                bool     `json:"consumer_mode"`
	PlatformAccessRestricted    bool     `json:"platform_access_restricted"`
	DiscoveryHidden             bool     `json:"discovery_hidden"`
	ApplicationAllowed          bool     `json:"application_allowed"`
	ResourceGrantConfigured     bool     `json:"resource_grant_configured"`
	RequiredApplicationPolicyID string   `json:"required_application_policy_id,omitempty"`
	NavigationRestrictions      []string `json:"navigation_restrictions"`
	MonitoringSignals           []string `json:"monitoring_signals"`
}

type IdentityCacheConfig struct {
	Enabled                bool                        `json:"enabled"`
	Warning                string                      `json:"warning"`
	DefaultTTLSeconds      int                         `json:"default_ttl_seconds"`
	AttributeTTLSeconds    int                         `json:"attribute_ttl_seconds"`
	GroupTTLSeconds        int                         `json:"group_ttl_seconds"`
	InactivityTTLSeconds   int                         `json:"inactivity_ttl_seconds"`
	ObjectPolicyTTLSeconds int                         `json:"object_policy_ttl_seconds"`
	HighRiskGroups         []string                    `json:"high_risk_groups"`
	HighRiskAttributes     []string                    `json:"high_risk_attributes"`
	Invalidations          []IdentityCacheInvalidation `json:"invalidations"`
}

type IdentityCacheInvalidation struct {
	ID        string    `json:"id"`
	SubjectID string    `json:"subject_id,omitempty"`
	GroupID   string    `json:"group_id,omitempty"`
	Attribute string    `json:"attribute,omitempty"`
	Reason    string    `json:"reason"`
	Actor     string    `json:"actor"`
	CreatedAt time.Time `json:"created_at"`
}

type IdentityCacheDecisionContextRequest struct {
	SubjectID               string    `json:"subject_id,omitempty"`
	GroupIDs                []string  `json:"group_ids,omitempty"`
	Attributes              []string  `json:"attributes,omitempty"`
	DecisionComputedAt      time.Time `json:"decision_computed_at,omitempty"`
	ObjectPolicyEvaluatedAt time.Time `json:"object_policy_evaluated_at,omitempty"`
}

type IdentityCacheDecisionContextResponse struct {
	Warning                   string     `json:"warning"`
	MayBeCached               bool       `json:"may_be_cached"`
	HighRiskInputs            []string   `json:"high_risk_inputs"`
	ExpiredInputs             []string   `json:"expired_inputs"`
	RecommendedAction         string     `json:"recommended_action"`
	NextRefreshAt             *time.Time `json:"next_refresh_at,omitempty"`
	RecentInvalidationMatched bool       `json:"recent_invalidation_matched"`
}

type IdentityCacheInvalidateRequest struct {
	SubjectID string `json:"subject_id,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
	Attribute string `json:"attribute,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func defaultCrossOrganizationConfig() CrossOrganizationConfig {
	return CrossOrganizationConfig{Enabled: true, Warning: CrossOrganizationWarning, Organizations: []CrossOrganizationBoundary{}, CollaborationGuides: []CrossOrganizationSetupGuide{
		{Scenario: "b2b_shared_space", Steps: []string{"Create a dedicated external organization and IdP mapping.", "Create a shared space containing only collaboration projects.", "Disable host internal group discovery for guests.", "Grant resources by project/space boundary and audit shared-resource access."}},
		{Scenario: "b2c_private_space", Steps: []string{"Create one consumer organization per privacy boundary.", "Use rule-based groups from IdP attributes.", "Keep customers in private spaces unless collaboration is explicitly required.", "Use restricted views or object policies for row-level sharing."}},
	}}
}

func defaultConsumerModeGovernanceConfig() ConsumerModeGovernanceConfig {
	return ConsumerModeGovernanceConfig{Enabled: true, Warning: ConsumerModeGovernanceWarning, Organizations: []ConsumerOrganizationConfig{}, AuthPatterns: []ConsumerAuthPattern{
		{Pattern: "in_platform_consumer_app", Controls: []string{"Restrict platform access", "Set a default consumer app URL", "Grant only app resources and backing projects"}},
		{Pattern: "foundry_hosted_oauth_app", Controls: []string{"Use OAuth scopes", "Pin redirect URIs", "Require application access policy"}},
		{Pattern: "client_credentials_app", Controls: []string{"Use service users", "Grant only required resources", "Monitor token and app-level access"}},
	}}
}

func defaultIdentityCacheConfig() IdentityCacheConfig {
	return IdentityCacheConfig{Enabled: true, Warning: IdentityCacheWarning, DefaultTTLSeconds: 300, AttributeTTLSeconds: 300, GroupTTLSeconds: 300, InactivityTTLSeconds: 900, ObjectPolicyTTLSeconds: 60, HighRiskGroups: []string{}, HighRiskAttributes: []string{}, Invalidations: []IdentityCacheInvalidation{}}
}

func normalizeCrossOrganizationConfig(cfg CrossOrganizationConfig) (CrossOrganizationConfig, error) {
	cfg.Warning = CrossOrganizationWarning
	seen := map[string]struct{}{}
	for i := range cfg.Organizations {
		org := &cfg.Organizations[i]
		org.OrganizationID = strings.TrimSpace(org.OrganizationID)
		org.OrganizationSlug = strings.TrimSpace(org.OrganizationSlug)
		if org.OrganizationID == "" && org.OrganizationSlug == "" {
			return CrossOrganizationConfig{}, fmt.Errorf("cross organization boundary requires organization_id or organization_slug")
		}
		key := strings.ToLower(org.OrganizationID)
		if key == "" {
			key = "slug:" + strings.ToLower(org.OrganizationSlug)
		}
		if _, ok := seen[key]; ok {
			return CrossOrganizationConfig{}, fmt.Errorf("cross organization boundaries must be unique")
		}
		seen[key] = struct{}{}
		org.PrimaryMemberGroupIDs = normalizeStringSet(org.PrimaryMemberGroupIDs)
		org.GuestMemberGroupIDs = normalizeStringSet(org.GuestMemberGroupIDs)
		org.DiscoverableOrganizationIDs = normalizeStringSet(org.DiscoverableOrganizationIDs)
		org.SharedSpaceIDs = normalizeStringSet(org.SharedSpaceIDs)
		org.SharedResourcePrefixes = normalizeStringSet(org.SharedResourcePrefixes)
		org.VisibleGroupIDs = normalizeStringSet(org.VisibleGroupIDs)
		org.HiddenInternalGroupIDs = normalizeStringSet(org.HiddenInternalGroupIDs)
		org.AllowedGuestPresetOrgIDs = normalizeStringSet(org.AllowedGuestPresetOrgIDs)
		org.IdentityProviderGuidance = strings.TrimSpace(org.IdentityProviderGuidance)
		org.SecurityBoundaryDescription = strings.TrimSpace(org.SecurityBoundaryDescription)
	}
	for i := range cfg.CollaborationGuides {
		cfg.CollaborationGuides[i].Scenario = strings.TrimSpace(cfg.CollaborationGuides[i].Scenario)
		cfg.CollaborationGuides[i].Steps = normalizeStringSet(cfg.CollaborationGuides[i].Steps)
	}
	return cfg, nil
}

func normalizeConsumerModeGovernanceConfig(cfg ConsumerModeGovernanceConfig) (ConsumerModeGovernanceConfig, error) {
	cfg.Warning = ConsumerModeGovernanceWarning
	seen := map[string]struct{}{}
	for i := range cfg.Organizations {
		org := &cfg.Organizations[i]
		org.OrganizationID = strings.TrimSpace(org.OrganizationID)
		org.OrganizationSlug = strings.TrimSpace(org.OrganizationSlug)
		if org.OrganizationID == "" && org.OrganizationSlug == "" {
			return ConsumerModeGovernanceConfig{}, fmt.Errorf("consumer organization requires organization_id or organization_slug")
		}
		key := strings.ToLower(org.OrganizationID)
		if key == "" {
			key = "slug:" + strings.ToLower(org.OrganizationSlug)
		}
		if _, ok := seen[key]; ok {
			return ConsumerModeGovernanceConfig{}, fmt.Errorf("consumer organizations must be unique")
		}
		seen[key] = struct{}{}
		org.ConsumerGroupIDs = normalizeStringSet(org.ConsumerGroupIDs)
		org.AllowedApplicationIDs = normalizeStringSet(org.AllowedApplicationIDs)
		org.ConsumerFacingResourcePrefixes = normalizeStringSet(org.ConsumerFacingResourcePrefixes)
		org.NavigationRestrictions = normalizeStringSet(org.NavigationRestrictions)
		org.MonitoringSignals = normalizeStringSet(org.MonitoringSignals)
		org.RequiredApplicationPolicyID = strings.TrimSpace(org.RequiredApplicationPolicyID)
		org.DefaultApplicationURL = strings.TrimSpace(org.DefaultApplicationURL)
	}
	for i := range cfg.AuthPatterns {
		cfg.AuthPatterns[i].Pattern = strings.TrimSpace(cfg.AuthPatterns[i].Pattern)
		cfg.AuthPatterns[i].Controls = normalizeStringSet(cfg.AuthPatterns[i].Controls)
	}
	return cfg, nil
}

func normalizeIdentityCacheConfig(cfg IdentityCacheConfig, previous IdentityCacheConfig) (IdentityCacheConfig, error) {
	cfg.Warning = IdentityCacheWarning
	if cfg.DefaultTTLSeconds <= 0 {
		cfg.DefaultTTLSeconds = 300
	}
	if cfg.AttributeTTLSeconds <= 0 {
		cfg.AttributeTTLSeconds = cfg.DefaultTTLSeconds
	}
	if cfg.GroupTTLSeconds <= 0 {
		cfg.GroupTTLSeconds = cfg.DefaultTTLSeconds
	}
	if cfg.InactivityTTLSeconds <= 0 {
		cfg.InactivityTTLSeconds = cfg.DefaultTTLSeconds
	}
	if cfg.ObjectPolicyTTLSeconds <= 0 {
		cfg.ObjectPolicyTTLSeconds = 60
	}
	cfg.HighRiskGroups = normalizeStringSet(cfg.HighRiskGroups)
	cfg.HighRiskAttributes = normalizeStringSet(cfg.HighRiskAttributes)
	if cfg.Invalidations == nil {
		cfg.Invalidations = append([]IdentityCacheInvalidation(nil), previous.Invalidations...)
	}
	return cfg, nil
}

func (h *ControlPanel) EvaluateCrossOrganization(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body CrossOrganizationEvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	h.mu.RLock()
	cfg := h.settings.CrossOrganization
	h.mu.RUnlock()
	resp := evaluateCrossOrganization(cfg, body, claims)
	writeJSON(w, http.StatusOK, resp)
}

func (h *ControlPanel) EvaluateConsumerMode(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body ConsumerModeEvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	h.mu.RLock()
	cfg := h.settings.ConsumerModeGovernance
	h.mu.RUnlock()
	writeJSON(w, http.StatusOK, evaluateConsumerMode(cfg, body, claims))
}

func (h *ControlPanel) IdentityCacheDecisionContext(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	var body IdentityCacheDecisionContextRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	h.mu.RLock()
	cfg := h.settings.IdentityCache
	h.mu.RUnlock()
	writeJSON(w, http.StatusOK, identityCacheDecisionContext(cfg, body, time.Now().UTC()))
}

func (h *ControlPanel) InvalidateIdentityCache(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireControlPanelWrite(w, r)
	if !ok {
		return
	}
	var body IdentityCacheInvalidateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if strings.TrimSpace(body.SubjectID) == "" && strings.TrimSpace(body.GroupID) == "" && strings.TrimSpace(body.Attribute) == "" {
		writeJSONErr(w, http.StatusBadRequest, "subject_id, group_id, or attribute is required")
		return
	}
	now := time.Now().UTC()
	actor := applicationAccessActor(claims)
	reason := strings.TrimSpace(body.Reason)
	if reason == "" {
		reason = "manual invalidation"
	}
	inv := IdentityCacheInvalidation{ID: fmt.Sprintf("ici-%d", now.UnixNano()), SubjectID: strings.TrimSpace(body.SubjectID), GroupID: strings.TrimSpace(body.GroupID), Attribute: strings.TrimSpace(body.Attribute), Reason: reason, Actor: actor, CreatedAt: now}
	h.mu.Lock()
	h.settings.IdentityCache.Invalidations = append(h.settings.IdentityCache.Invalidations, inv)
	h.settings.UpdatedBy = &actor
	h.settings.UpdatedAt = now
	cfg := h.settings.IdentityCache
	h.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]any{"invalidation": inv, "identity_cache": cfg})
}

func evaluateCrossOrganization(cfg CrossOrganizationConfig, body CrossOrganizationEvaluateRequest, claims *authmw.Claims) CrossOrganizationEvaluateResponse {
	cfg, err := normalizeCrossOrganizationConfig(cfg)
	if err != nil {
		cfg = defaultCrossOrganizationConfig()
	}
	host := strings.TrimSpace(body.HostOrganizationID)
	if host == "" && claims != nil && claims.OrgID != nil {
		host = claims.OrgID.String()
	}
	primary := strings.TrimSpace(body.PrimaryOrganizationID)
	if primary == "" && claims != nil && claims.OrgID != nil {
		primary = claims.OrgID.String()
	}
	boundary, _ := findCrossOrgBoundary(cfg, host)
	guest := claims != nil && (claims.IsGuestSession() || (primary != "" && host != "" && !strings.EqualFold(primary, host)))
	visible := append([]string(nil), boundary.VisibleGroupIDs...)
	hidden := []string{}
	if guest {
		hidden = append(hidden, boundary.HiddenInternalGroupIDs...)
	} else {
		visible = append(visible, boundary.HiddenInternalGroupIDs...)
	}
	target := strings.TrimSpace(body.TargetOrganizationID)
	canDiscover := target == "" || strings.EqualFold(target, host) || containsFold(boundary.DiscoverableOrganizationIDs, target)
	canResource := body.ResourcePath == "" || hasAnyPrefix(body.ResourcePath, boundary.SharedResourcePrefixes)
	presetOrg := strings.TrimSpace(body.RequestedPresetOrgID)
	canPreset := presetOrg == "" || (!guest && strings.EqualFold(presetOrg, host)) || (guest && containsFold(boundary.AllowedGuestPresetOrgIDs, presetOrg) && strings.EqualFold(presetOrg, primary))
	return CrossOrganizationEvaluateResponse{Warning: CrossOrganizationWarning, HostOrganizationID: host, PrimaryOrganizationID: primary, GuestSession: guest, CanDiscoverTargetOrg: canDiscover, CanAccessSharedResource: canResource, CanApplyRequestedPreset: canPreset, VisibleGroupIDs: normalizeStringSet(visible), HiddenGroupIDs: normalizeStringSet(hidden), RequiredBoundary: boundary.SecurityBoundaryDescription}
}

func evaluateConsumerMode(cfg ConsumerModeGovernanceConfig, body ConsumerModeEvaluateRequest, claims *authmw.Claims) ConsumerModeEvaluateResponse {
	cfg, err := normalizeConsumerModeGovernanceConfig(cfg)
	if err != nil {
		cfg = defaultConsumerModeGovernanceConfig()
	}
	orgID := strings.TrimSpace(body.OrganizationID)
	if orgID == "" && claims != nil && claims.OrgID != nil {
		orgID = claims.OrgID.String()
	}
	org, found := findConsumerOrg(cfg, orgID)
	appAllowed := !found || body.ApplicationID == "" || containsFold(org.AllowedApplicationIDs, body.ApplicationID)
	resourceAllowed := !found || body.ResourcePath == "" || hasAnyPrefix(body.ResourcePath, org.ConsumerFacingResourcePrefixes)
	return ConsumerModeEvaluateResponse{Warning: ConsumerModeGovernanceWarning, OrganizationID: orgID, ConsumerMode: found, PlatformAccessRestricted: found && org.PlatformAccessRestricted, DiscoveryHidden: found && org.UserDiscoveryHidden && org.GroupDiscoveryHidden, ApplicationAllowed: appAllowed, ResourceGrantConfigured: resourceAllowed, RequiredApplicationPolicyID: org.RequiredApplicationPolicyID, NavigationRestrictions: org.NavigationRestrictions, MonitoringSignals: org.MonitoringSignals}
}

func identityCacheDecisionContext(cfg IdentityCacheConfig, body IdentityCacheDecisionContextRequest, now time.Time) IdentityCacheDecisionContextResponse {
	cfg, err := normalizeIdentityCacheConfig(cfg, cfg)
	if err != nil {
		cfg = defaultIdentityCacheConfig()
	}
	computed := body.DecisionComputedAt
	if computed.IsZero() {
		computed = now
	}
	highRisk, expired := []string{}, []string{}
	for _, g := range body.GroupIDs {
		if containsFold(cfg.HighRiskGroups, g) {
			highRisk = append(highRisk, "group:"+g)
		}
	}
	for _, a := range body.Attributes {
		if containsFold(cfg.HighRiskAttributes, a) {
			highRisk = append(highRisk, "attribute:"+a)
		}
	}
	if now.Sub(computed) > time.Duration(cfg.DefaultTTLSeconds)*time.Second {
		expired = append(expired, "decision")
	}
	if !body.ObjectPolicyEvaluatedAt.IsZero() && now.Sub(body.ObjectPolicyEvaluatedAt) > time.Duration(cfg.ObjectPolicyTTLSeconds)*time.Second {
		expired = append(expired, "object_policy")
	}
	matched := false
	for _, inv := range cfg.Invalidations {
		if strings.EqualFold(inv.SubjectID, body.SubjectID) || containsFold(body.GroupIDs, inv.GroupID) || containsFold(body.Attributes, inv.Attribute) {
			matched = true
			break
		}
	}
	next := computed.Add(time.Duration(cfg.DefaultTTLSeconds) * time.Second)
	action := "decision is within configured cache TTL"
	if len(highRisk) > 0 || len(expired) > 0 || matched {
		action = "refresh identity context or invalidate cache before relying on this policy decision"
	}
	return IdentityCacheDecisionContextResponse{Warning: IdentityCacheWarning, MayBeCached: cfg.Enabled && (len(expired) == 0), HighRiskInputs: normalizeStringSet(highRisk), ExpiredInputs: normalizeStringSet(expired), RecommendedAction: action, NextRefreshAt: &next, RecentInvalidationMatched: matched}
}

func findCrossOrgBoundary(cfg CrossOrganizationConfig, orgID string) (CrossOrganizationBoundary, bool) {
	for _, org := range cfg.Organizations {
		if strings.EqualFold(org.OrganizationID, orgID) || strings.EqualFold(org.OrganizationSlug, orgID) {
			return org, true
		}
	}
	return CrossOrganizationBoundary{}, false
}
func findConsumerOrg(cfg ConsumerModeGovernanceConfig, orgID string) (ConsumerOrganizationConfig, bool) {
	for _, org := range cfg.Organizations {
		if strings.EqualFold(org.OrganizationID, orgID) || strings.EqualFold(org.OrganizationSlug, orgID) {
			return org, true
		}
	}
	return ConsumerOrganizationConfig{}, false
}
func hasAnyPrefix(value string, prefixes []string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}
