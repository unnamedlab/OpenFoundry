package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

type ControlPanel struct {
	mu               sync.RWMutex
	settings         ControlPanelSettings
	streamingProfiles []StreamingProfile
}

type ControlPanelSettings struct {
	PlatformName               string                  `json:"platform_name"`
	SupportEmail               string                  `json:"support_email"`
	DocsURL                    string                  `json:"docs_url"`
	StatusPageURL              string                  `json:"status_page_url"`
	AnnouncementBanner         string                  `json:"announcement_banner"`
	MaintenanceMode            bool                    `json:"maintenance_mode"`
	ReleaseChannel             string                  `json:"release_channel"`
	DefaultRegion              string                  `json:"default_region"`
	DeploymentMode             string                  `json:"deployment_mode"`
	AllowSelfSignup            bool                    `json:"allow_self_signup"`
	SupportedLocales           []string                `json:"supported_locales"`
	DefaultLocale              string                  `json:"default_locale"`
	AllowedEmailDomains        []string                `json:"allowed_email_domains"`
	DefaultAppBranding         json.RawMessage         `json:"default_app_branding"`
	RestrictedOperations       []string                `json:"restricted_operations"`
	IdentityProviderMappings   json.RawMessage         `json:"identity_provider_mappings"`
	ResourceManagementPolicies json.RawMessage         `json:"resource_management_policies"`
	UpgradeAssistant           json.RawMessage         `json:"upgrade_assistant"`
	ScopedSessions             ScopedSessionConfig     `json:"scoped_sessions"`
	ApplicationAccess          ApplicationAccessConfig `json:"application_access"`
	MemberDiscovery            MemberDiscoveryConfig   `json:"member_discovery"`
	FileAccessPresets          FileAccessPresetConfig  `json:"file_access_presets"`
	UpdatedBy                  *string                 `json:"updated_by"`
	UpdatedAt                  time.Time               `json:"updated_at"`
}

type ScopedSessionConfig struct {
	Enabled              bool                  `json:"enabled"`
	AllowNoScopedSession bool                  `json:"allow_no_scoped_session"`
	AlwaysShowSelector   bool                  `json:"always_show_selector"`
	AllowedBypassGroups  []string              `json:"allowed_bypass_groups"`
	Presets              []ScopedSessionPreset `json:"presets"`
}

type ScopedSessionPreset struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	RequiredMarkings []string `json:"required_markings"`
	AllowedMarkings  []string `json:"allowed_markings"`
	Enabled          bool     `json:"enabled"`
}

const FileAccessPresetWarning = "File access presets only pre-fill supported resource-creation security controls. Users can see a preset only when they have Apply marking permission for every marking in the preset; selecting a preset never grants access to marked data."

const FileAccessPresetGuestPrimaryOrganization = "primary_organization"

type FileAccessPresetConfig struct {
	Enabled                   bool                           `json:"enabled"`
	Warning                   string                         `json:"warning"`
	GuestOrganizationBehavior string                         `json:"guest_organization_behavior"`
	Presets                   []FileAccessPreset             `json:"presets"`
	History                   []FileAccessPresetHistoryEvent `json:"history"`
}

type FileAccessPreset struct {
	ID                     string                               `json:"id"`
	Title                  string                               `json:"title"`
	Description            string                               `json:"description,omitempty"`
	MarkingIDs             []string                             `json:"marking_ids"`
	LocalAccessControls    []FileAccessPresetLocalAccessControl `json:"local_access_controls"`
	OrganizationIDs        []string                             `json:"organization_ids"`
	SupportedResourceKinds []string                             `json:"supported_resource_kinds"`
	DefaultOrder           int                                  `json:"default_order"`
	Enabled                bool                                 `json:"enabled"`
	CreatedBy              *string                              `json:"created_by,omitempty"`
	CreatedAt              *time.Time                           `json:"created_at,omitempty"`
	UpdatedBy              *string                              `json:"updated_by,omitempty"`
	UpdatedAt              *time.Time                           `json:"updated_at,omitempty"`
}

type FileAccessPresetLocalAccessControl struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Label    string          `json:"label"`
	Values   []string        `json:"values"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

type FileAccessPresetHistoryEvent struct {
	ID                        string    `json:"id"`
	Actor                     string    `json:"actor"`
	Timestamp                 time.Time `json:"timestamp"`
	Action                    string    `json:"action"`
	Summary                   string    `json:"summary"`
	PresetCount               int       `json:"preset_count"`
	Enabled                   bool      `json:"enabled"`
	GuestOrganizationBehavior string    `json:"guest_organization_behavior"`
	Warning                   string    `json:"warning"`
}

type FileAccessPresetVisibilityRequest struct {
	OrganizationID        string `json:"organization_id,omitempty"`
	PrimaryOrganizationID string `json:"primary_organization_id,omitempty"`
	ResourceKind          string `json:"resource_kind,omitempty"`
}

type FileAccessPresetVisibilityResponse struct {
	Warning                   string             `json:"warning"`
	GuestOrganizationBehavior string             `json:"guest_organization_behavior"`
	EffectiveOrganizationID   string             `json:"effective_organization_id,omitempty"`
	DefaultPresetID           string             `json:"default_preset_id,omitempty"`
	FilteredPresetCount       int                `json:"filtered_preset_count"`
	Presets                   []FileAccessPreset `json:"presets"`
}

const ApplicationAccessScopeWarning = "Application access controls application launcher, sidebar, and navigation visibility only; server-side permissions still govern every resource and API request."

type ApplicationAccessConfig struct {
	Enabled           bool                             `json:"enabled"`
	DefaultVisibility string                           `json:"default_visibility"`
	Warning           string                           `json:"warning"`
	Applications      []ApplicationAccessApplication   `json:"applications"`
	Rules             []ApplicationAccessRule          `json:"rules"`
	ApprovalPolicy    ApplicationAccessApprovalPolicy  `json:"approval_policy"`
	ChangeRequests    []ApplicationAccessChangeRequest `json:"change_requests"`
	History           []ApplicationAccessHistoryEvent  `json:"history"`
}

type ApplicationAccessApplication struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Category       string `json:"category"`
	LifecycleStage string `json:"lifecycle_stage"`
	Enabled        bool   `json:"enabled"`
}

type ApplicationAccessRule struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Effect          string   `json:"effect"`
	ApplicationIDs  []string `json:"application_ids"`
	OrganizationIDs []string `json:"organization_ids"`
	UserIDs         []string `json:"user_ids"`
	GroupIDs        []string `json:"group_ids"`
	LifecycleStages []string `json:"lifecycle_stages"`
	Enabled         bool     `json:"enabled"`
	Reason          string   `json:"reason,omitempty"`
}

type ApplicationAccessApprovalPolicy struct {
	Mode                             string   `json:"mode"`
	ReviewerUserIDs                  []string `json:"reviewer_user_ids"`
	ReviewerGroupIDs                 []string `json:"reviewer_group_ids"`
	RequireDistinctReviewerForPolicy bool     `json:"require_distinct_reviewer_for_policy"`
	Instructions                     string   `json:"instructions,omitempty"`
}

type ApplicationAccessChangeRequest struct {
	ID             string                  `json:"id"`
	Kind           string                  `json:"kind"`
	Status         string                  `json:"status"`
	Summary        string                  `json:"summary"`
	Warning        string                  `json:"warning"`
	RequestedBy    string                  `json:"requested_by"`
	RequestedAt    time.Time               `json:"requested_at"`
	DecidedBy      *string                 `json:"decided_by,omitempty"`
	DecidedAt      *time.Time              `json:"decided_at,omitempty"`
	AppliedAt      *time.Time              `json:"applied_at,omitempty"`
	Comment        *string                 `json:"comment,omitempty"`
	ProposedConfig ApplicationAccessConfig `json:"proposed_config"`
}

type ApplicationAccessHistoryEvent struct {
	ID               string    `json:"id"`
	RequestID        string    `json:"request_id"`
	Kind             string    `json:"kind"`
	Action           string    `json:"action"`
	Actor            string    `json:"actor"`
	Timestamp        time.Time `json:"timestamp"`
	Summary          string    `json:"summary"`
	Warning          string    `json:"warning"`
	RuleCount        int       `json:"rule_count"`
	ApplicationCount int       `json:"application_count"`
}

type ApplicationAccessDecisionRequest struct {
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
}

type ApplicationAccessEvaluateRequest struct {
	ApplicationID  string   `json:"application_id,omitempty"`
	ApplicationIDs []string `json:"application_ids,omitempty"`
	UserID         string   `json:"user_id,omitempty"`
	GroupIDs       []string `json:"group_ids,omitempty"`
	OrganizationID string   `json:"organization_id,omitempty"`
	LifecycleStage string   `json:"lifecycle_stage,omitempty"`
}

type ApplicationAccessEvaluateResponse struct {
	Warning   string                      `json:"warning"`
	Decisions []ApplicationAccessDecision `json:"decisions"`
}

type ApplicationAccessDecision struct {
	ApplicationID     string   `json:"application_id"`
	Visible           bool     `json:"visible"`
	Decision          string   `json:"decision"`
	Reason            string   `json:"reason"`
	LifecycleStage    string   `json:"lifecycle_stage"`
	MatchedRuleIDs    []string `json:"matched_rule_ids"`
	MatchedRuleNames  []string `json:"matched_rule_names"`
	DefaultVisibility string   `json:"default_visibility"`
	UXScopeOnly       bool     `json:"ux_scope_only"`
}

const MemberDiscoveryWarning = "User and group visibility controls only restrict discovery surfaces. Existing permissions and access rights remain unchanged, but user-defined logic that depends on user or group lookup may fail when discovery is disabled."

type MemberDiscoveryConfig struct {
	DefaultDiscoverUsers  bool                                `json:"default_discover_users"`
	DefaultDiscoverGroups bool                                `json:"default_discover_groups"`
	Warning               string                              `json:"warning"`
	Organizations         []MemberDiscoveryOrganizationConfig `json:"organizations"`
	History               []MemberDiscoveryHistoryEvent       `json:"history"`
}

type MemberDiscoveryOrganizationConfig struct {
	OrganizationID       string     `json:"organization_id"`
	OrganizationSlug     string     `json:"organization_slug,omitempty"`
	DiscoverUsers        bool       `json:"discover_users"`
	DiscoverGroups       bool       `json:"discover_groups"`
	ConsumerModeBoundary bool       `json:"consumer_mode_boundary"`
	Notes                string     `json:"notes,omitempty"`
	UpdatedBy            *string    `json:"updated_by,omitempty"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
}

type MemberDiscoveryHistoryEvent struct {
	ID                   string    `json:"id"`
	OrganizationID       string    `json:"organization_id"`
	OrganizationSlug     string    `json:"organization_slug,omitempty"`
	Actor                string    `json:"actor"`
	Timestamp            time.Time `json:"timestamp"`
	DiscoverUsers        bool      `json:"discover_users"`
	DiscoverGroups       bool      `json:"discover_groups"`
	ConsumerModeBoundary bool      `json:"consumer_mode_boundary"`
	Warning              string    `json:"warning"`
}

type UpdateControlPanelRequest struct {
	PlatformName               *string                  `json:"platform_name"`
	SupportEmail               *string                  `json:"support_email"`
	DocsURL                    *string                  `json:"docs_url"`
	StatusPageURL              *string                  `json:"status_page_url"`
	AnnouncementBanner         *string                  `json:"announcement_banner"`
	MaintenanceMode            *bool                    `json:"maintenance_mode"`
	ReleaseChannel             *string                  `json:"release_channel"`
	DefaultRegion              *string                  `json:"default_region"`
	DeploymentMode             *string                  `json:"deployment_mode"`
	AllowSelfSignup            *bool                    `json:"allow_self_signup"`
	SupportedLocales           *[]string                `json:"supported_locales"`
	DefaultLocale              *string                  `json:"default_locale"`
	AllowedEmailDomains        *[]string                `json:"allowed_email_domains"`
	DefaultAppBranding         *json.RawMessage         `json:"default_app_branding"`
	RestrictedOperations       *[]string                `json:"restricted_operations"`
	IdentityProviderMappings   *json.RawMessage         `json:"identity_provider_mappings"`
	ResourceManagementPolicies *json.RawMessage         `json:"resource_management_policies"`
	UpgradeAssistant           *json.RawMessage         `json:"upgrade_assistant"`
	ScopedSessions             *ScopedSessionConfig     `json:"scoped_sessions"`
	ApplicationAccess          *ApplicationAccessConfig `json:"application_access"`
	MemberDiscovery            *MemberDiscoveryConfig   `json:"member_discovery"`
	FileAccessPresets          *FileAccessPresetConfig  `json:"file_access_presets"`
}

type UpgradeReadinessResponse struct {
	CurrentVersion             string                  `json:"current_version"`
	TargetVersion              string                  `json:"target_version"`
	ReleaseChannel             string                  `json:"release_channel"`
	Readiness                  string                  `json:"readiness"`
	Checks                     []UpgradeReadinessCheck `json:"checks"`
	Blockers                   []string                `json:"blockers"`
	RecommendedActions         []string                `json:"recommended_actions"`
	NextStage                  *UpgradeAssistantStage  `json:"next_stage"`
	CompletedStageCount        int                     `json:"completed_stage_count"`
	TotalStageCount            int                     `json:"total_stage_count"`
	PreflightReadyCount        int                     `json:"preflight_ready_count"`
	PreflightTotalCount        int                     `json:"preflight_total_count"`
	CompletedRolloutPercentage int                     `json:"completed_rollout_percentage"`
	GeneratedAt                time.Time               `json:"generated_at"`
}

type UpgradeReadinessCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

type UpgradeAssistantStage struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	RolloutPercentage int    `json:"rollout_percentage"`
	Status            string `json:"status"`
}

type IdentityProviderMappingPreviewRequest struct {
	ProviderSlug string         `json:"provider_slug"`
	Email        string         `json:"email"`
	RawClaims    map[string]any `json:"raw_claims"`
}

type IdentityProviderMappingPreviewResponse struct {
	ProviderSlug            string          `json:"provider_slug"`
	Email                   string          `json:"email"`
	MappingFound            bool            `json:"mapping_found"`
	MatchedRuleName         *string         `json:"matched_rule_name"`
	OrganizationID          *string         `json:"organization_id"`
	Workspace               *string         `json:"workspace"`
	ClassificationClearance *string         `json:"classification_clearance"`
	RoleNames               []string        `json:"role_names"`
	TenantTier              *string         `json:"tenant_tier"`
	ResourcePolicyName      *string         `json:"resource_policy_name"`
	Quota                   json.RawMessage `json:"quota"`
	Notes                   []string        `json:"notes"`
}

func NewControlPanel() *ControlPanel {
	now := time.Now().UTC()
	return &ControlPanel{settings: ControlPanelSettings{
		PlatformName:               "OpenFoundry",
		SupportEmail:               "support@openfoundry.dev",
		DocsURL:                    "https://docs.openfoundry.dev",
		StatusPageURL:              "https://status.openfoundry.dev",
		ReleaseChannel:             "stable",
		DefaultRegion:              "eu-west-1",
		DeploymentMode:             "self_hosted",
		SupportedLocales:           []string{"en", "es"},
		DefaultLocale:              "en",
		DefaultAppBranding:         json.RawMessage(`{"display_name":"OpenFoundry","primary_color":"#0f766e","accent_color":"#2563eb","logo_url":null,"favicon_url":null,"show_powered_by":true}`),
		IdentityProviderMappings:   json.RawMessage(`[]`),
		ResourceManagementPolicies: json.RawMessage(`[]`),
		UpgradeAssistant:           json.RawMessage(`{"current_version":"dev","target_version":"dev","maintenance_window":"manual","rollback_channel":"stable","preflight_checks":[],"rollout_stages":[],"rollback_steps":[]}`),
		ScopedSessions:             defaultScopedSessionConfig(),
		ApplicationAccess:          defaultApplicationAccessConfig(),
		MemberDiscovery:            defaultMemberDiscoveryConfig(),
		FileAccessPresets:          defaultFileAccessPresetConfig(),
		UpdatedAt:                  now,
	}}
}

func (h *ControlPanel) Get(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	writeJSON(w, http.StatusOK, h.settings)
}

func (h *ControlPanel) Update(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireControlPanelWrite(w, r)
	if !ok {
		return
	}
	var body UpdateControlPanelRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := applyControlPanelUpdate(&h.settings, &body, claims); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	updatedBy := claims.Email
	if strings.TrimSpace(updatedBy) == "" {
		updatedBy = claims.Sub.String()
	}
	h.settings.UpdatedBy = &updatedBy
	h.settings.UpdatedAt = time.Now().UTC()
	writeJSON(w, http.StatusOK, h.settings)
}

func (h *ControlPanel) UpgradeReadiness(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	h.mu.RLock()
	settings := h.settings
	h.mu.RUnlock()
	checks := []UpgradeReadinessCheck{
		{ID: "control-panel-settings", Label: "Control panel settings reachable", Status: "pass", Detail: "Admin settings endpoint is responding."},
		{ID: "release-channel", Label: "Release channel selected", Status: "pass", Detail: settings.ReleaseChannel},
	}
	blockers := []string{}
	if settings.MaintenanceMode {
		checks = append(checks, UpgradeReadinessCheck{ID: "maintenance-mode", Label: "Maintenance mode", Status: "warn", Detail: "Maintenance mode is enabled."})
		blockers = append(blockers, "maintenance_mode_enabled")
	}
	writeJSON(w, http.StatusOK, UpgradeReadinessResponse{
		CurrentVersion:             "dev",
		TargetVersion:              "dev",
		ReleaseChannel:             settings.ReleaseChannel,
		Readiness:                  readinessLabel(blockers),
		Checks:                     checks,
		Blockers:                   blockers,
		RecommendedActions:         recommendedUpgradeActions(blockers),
		CompletedStageCount:        0,
		TotalStageCount:            0,
		PreflightReadyCount:        len(checks) - len(blockers),
		PreflightTotalCount:        len(checks),
		CompletedRolloutPercentage: 0,
		GeneratedAt:                time.Now().UTC(),
	})
}

func (h *ControlPanel) PreviewIdentityProviderMapping(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	var body IdentityProviderMappingPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	notes := []string{"No typed mapping rule matched; preview returned defaults only."}
	mappingFound := strings.TrimSpace(body.ProviderSlug) != "" && strings.Contains(body.Email, "@")
	if mappingFound {
		notes = []string{"Provider slug and email are syntactically valid."}
	}
	writeJSON(w, http.StatusOK, IdentityProviderMappingPreviewResponse{
		ProviderSlug: body.ProviderSlug,
		Email:        body.Email,
		MappingFound: mappingFound,
		RoleNames:    []string{},
		Notes:        notes,
		Quota:        json.RawMessage(`null`),
	})
}

func (h *ControlPanel) ApplicationAccessChangeRequests(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireControlPanelRead(w, r); !ok {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"change_requests": h.settings.ApplicationAccess.ChangeRequests,
		"history":         h.settings.ApplicationAccess.History,
		"warning":         h.settings.ApplicationAccess.Warning,
	})
}

func (h *ControlPanel) DecideApplicationAccessChangeRequest(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireControlPanelWrite(w, r)
	if !ok {
		return
	}
	requestID := strings.TrimSpace(chi.URLParam(r, "id"))
	if requestID == "" {
		writeJSONErr(w, http.StatusBadRequest, "change request id is required")
		return
	}
	var body ApplicationAccessDecisionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := decideApplicationAccessChangeRequest(&h.settings, requestID, body, claims); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	updatedBy := applicationAccessActor(claims)
	h.settings.UpdatedBy = &updatedBy
	h.settings.UpdatedAt = time.Now().UTC()
	writeJSON(w, http.StatusOK, h.settings.ApplicationAccess)
}

func (h *ControlPanel) EvaluateApplicationAccess(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body ApplicationAccessEvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	h.mu.RLock()
	cfg := cloneApplicationAccessConfig(h.settings.ApplicationAccess)
	h.mu.RUnlock()
	resp, err := evaluateApplicationAccess(cfg, body, claims)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *ControlPanel) VisibleFileAccessPresets(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return
	}
	var body FileAccessPresetVisibilityRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	h.mu.RLock()
	cfg := cloneFileAccessPresetConfig(h.settings.FileAccessPresets)
	h.mu.RUnlock()
	resp, err := visibleFileAccessPresets(cfg, body, claims)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func applyControlPanelUpdate(settings *ControlPanelSettings, body *UpdateControlPanelRequest, claims *authmw.Claims) error {
	if body.PlatformName != nil {
		settings.PlatformName = *body.PlatformName
	}
	if body.SupportEmail != nil {
		settings.SupportEmail = *body.SupportEmail
	}
	if body.DocsURL != nil {
		settings.DocsURL = *body.DocsURL
	}
	if body.StatusPageURL != nil {
		settings.StatusPageURL = *body.StatusPageURL
	}
	if body.AnnouncementBanner != nil {
		settings.AnnouncementBanner = *body.AnnouncementBanner
	}
	if body.MaintenanceMode != nil {
		settings.MaintenanceMode = *body.MaintenanceMode
	}
	if body.ReleaseChannel != nil {
		settings.ReleaseChannel = *body.ReleaseChannel
	}
	if body.DefaultRegion != nil {
		settings.DefaultRegion = *body.DefaultRegion
	}
	if body.DeploymentMode != nil {
		settings.DeploymentMode = *body.DeploymentMode
	}
	if body.AllowSelfSignup != nil {
		settings.AllowSelfSignup = *body.AllowSelfSignup
	}
	if body.SupportedLocales != nil {
		settings.SupportedLocales = append([]string(nil), (*body.SupportedLocales)...)
	}
	if body.DefaultLocale != nil {
		settings.DefaultLocale = *body.DefaultLocale
	}
	if body.AllowedEmailDomains != nil {
		settings.AllowedEmailDomains = append([]string(nil), (*body.AllowedEmailDomains)...)
	}
	if body.DefaultAppBranding != nil {
		settings.DefaultAppBranding = cloneRaw(*body.DefaultAppBranding, `{}`)
	}
	if body.RestrictedOperations != nil {
		settings.RestrictedOperations = append([]string(nil), (*body.RestrictedOperations)...)
	}
	if body.IdentityProviderMappings != nil {
		settings.IdentityProviderMappings = cloneRaw(*body.IdentityProviderMappings, `[]`)
	}
	if body.ResourceManagementPolicies != nil {
		settings.ResourceManagementPolicies = cloneRaw(*body.ResourceManagementPolicies, `[]`)
	}
	if body.UpgradeAssistant != nil {
		settings.UpgradeAssistant = cloneRaw(*body.UpgradeAssistant, `{}`)
	}
	if body.ScopedSessions != nil {
		cfg, err := normalizeScopedSessionConfig(*body.ScopedSessions)
		if err != nil {
			return err
		}
		settings.ScopedSessions = cfg
	}
	if body.ApplicationAccess != nil {
		if err := applyApplicationAccessUpdate(settings, *body.ApplicationAccess, claims); err != nil {
			return err
		}
	}
	if body.MemberDiscovery != nil {
		cfg, err := normalizeMemberDiscoveryConfig(*body.MemberDiscovery)
		if err != nil {
			return err
		}
		cfg = stampMemberDiscoveryChanges(settings.MemberDiscovery, cfg, applicationAccessActor(claims), time.Now().UTC())
		settings.MemberDiscovery = cfg
	}
	if body.FileAccessPresets != nil {
		if err := applyFileAccessPresetUpdate(settings, *body.FileAccessPresets, claims); err != nil {
			return err
		}
	}
	return nil
}

func cloneRaw(raw json.RawMessage, fallback string) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage(fallback)
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

func defaultFileAccessPresetConfig() FileAccessPresetConfig {
	return FileAccessPresetConfig{
		Enabled:                   true,
		Warning:                   FileAccessPresetWarning,
		GuestOrganizationBehavior: FileAccessPresetGuestPrimaryOrganization,
		Presets:                   []FileAccessPreset{},
		History:                   []FileAccessPresetHistoryEvent{},
	}
}

func cloneFileAccessPresetConfig(cfg FileAccessPresetConfig) FileAccessPresetConfig {
	out := cfg
	out.Presets = make([]FileAccessPreset, 0, len(cfg.Presets))
	for _, preset := range cfg.Presets {
		cp := cloneFileAccessPreset(preset)
		out.Presets = append(out.Presets, cp)
	}
	out.History = append([]FileAccessPresetHistoryEvent(nil), cfg.History...)
	return out
}

func cloneFileAccessPreset(preset FileAccessPreset) FileAccessPreset {
	out := preset
	out.MarkingIDs = append([]string(nil), preset.MarkingIDs...)
	out.OrganizationIDs = append([]string(nil), preset.OrganizationIDs...)
	out.SupportedResourceKinds = append([]string(nil), preset.SupportedResourceKinds...)
	out.LocalAccessControls = make([]FileAccessPresetLocalAccessControl, 0, len(preset.LocalAccessControls))
	for _, control := range preset.LocalAccessControls {
		cp := control
		cp.Values = append([]string(nil), control.Values...)
		cp.Metadata = cloneRaw(control.Metadata, `{}`)
		out.LocalAccessControls = append(out.LocalAccessControls, cp)
	}
	return out
}

func normalizeFileAccessPresetConfig(cfg FileAccessPresetConfig) (FileAccessPresetConfig, error) {
	cfg.Warning = FileAccessPresetWarning
	cfg.GuestOrganizationBehavior = strings.ToLower(strings.TrimSpace(cfg.GuestOrganizationBehavior))
	if cfg.GuestOrganizationBehavior == "" {
		cfg.GuestOrganizationBehavior = FileAccessPresetGuestPrimaryOrganization
	}
	if cfg.GuestOrganizationBehavior != FileAccessPresetGuestPrimaryOrganization {
		return FileAccessPresetConfig{}, errBadFileAccessPresetConfig("file access preset guest_organization_behavior must be primary_organization")
	}
	presets := make([]FileAccessPreset, 0, len(cfg.Presets))
	seen := map[string]struct{}{}
	for i, preset := range cfg.Presets {
		preset.ID = strings.TrimSpace(preset.ID)
		preset.Title = strings.TrimSpace(preset.Title)
		preset.Description = strings.TrimSpace(preset.Description)
		if preset.ID == "" {
			return FileAccessPresetConfig{}, errBadFileAccessPresetConfig("file access preset id is required")
		}
		key := strings.ToLower(preset.ID)
		if _, ok := seen[key]; ok {
			return FileAccessPresetConfig{}, errBadFileAccessPresetConfig("file access preset ids must be unique")
		}
		seen[key] = struct{}{}
		if preset.Title == "" {
			preset.Title = preset.ID
		}
		preset.MarkingIDs = normalizeStringSet(preset.MarkingIDs)
		preset.OrganizationIDs = normalizeStringSet(preset.OrganizationIDs)
		preset.SupportedResourceKinds = normalizeFileAccessResourceKinds(preset.SupportedResourceKinds)
		if preset.DefaultOrder <= 0 {
			preset.DefaultOrder = i + 1
		}
		controls := make([]FileAccessPresetLocalAccessControl, 0, len(preset.LocalAccessControls))
		for j, control := range preset.LocalAccessControls {
			control.ID = strings.TrimSpace(control.ID)
			control.Kind = strings.ToLower(strings.TrimSpace(control.Kind))
			control.Label = strings.TrimSpace(control.Label)
			if control.ID == "" {
				control.ID = fmt.Sprintf("%s-control-%d", preset.ID, j+1)
			}
			if control.Kind == "" {
				control.Kind = "cbac"
			}
			if control.Label == "" {
				control.Label = control.ID
			}
			control.Values = normalizeStringSet(control.Values)
			control.Metadata = cloneRaw(control.Metadata, `{}`)
			controls = append(controls, control)
		}
		preset.LocalAccessControls = controls
		presets = append(presets, preset)
	}
	sort.SliceStable(presets, func(i, j int) bool {
		if presets[i].DefaultOrder == presets[j].DefaultOrder {
			return strings.ToLower(presets[i].Title) < strings.ToLower(presets[j].Title)
		}
		return presets[i].DefaultOrder < presets[j].DefaultOrder
	})
	cfg.Presets = presets
	cfg.History = append([]FileAccessPresetHistoryEvent(nil), cfg.History...)
	return cfg, nil
}

func normalizeFileAccessResourceKinds(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return normalizeStringSet(out)
}

func applyFileAccessPresetUpdate(settings *ControlPanelSettings, input FileAccessPresetConfig, claims *authmw.Claims) error {
	current, err := normalizeFileAccessPresetConfig(settings.FileAccessPresets)
	if err != nil {
		current = defaultFileAccessPresetConfig()
	}
	proposed, err := normalizeFileAccessPresetConfig(input)
	if err != nil {
		return err
	}
	proposed = stampFileAccessPresetChanges(current, proposed, applicationAccessActor(claims), time.Now().UTC())
	settings.FileAccessPresets = proposed
	return nil
}

func stampFileAccessPresetChanges(previous, next FileAccessPresetConfig, actor string, now time.Time) FileAccessPresetConfig {
	prevByID := map[string]FileAccessPreset{}
	for _, preset := range previous.Presets {
		prevByID[strings.ToLower(preset.ID)] = preset
	}
	for i := range next.Presets {
		prev, existed := prevByID[strings.ToLower(next.Presets[i].ID)]
		changed := !existed || !sameFileAccessPresetPublicConfig(prev, next.Presets[i])
		if existed {
			next.Presets[i].CreatedBy = prev.CreatedBy
			next.Presets[i].CreatedAt = prev.CreatedAt
			next.Presets[i].UpdatedBy = prev.UpdatedBy
			next.Presets[i].UpdatedAt = prev.UpdatedAt
		}
		if !existed {
			next.Presets[i].CreatedBy = &actor
			next.Presets[i].CreatedAt = &now
		}
		if changed {
			next.Presets[i].UpdatedBy = &actor
			next.Presets[i].UpdatedAt = &now
		}
	}
	next.Warning = FileAccessPresetWarning
	next.History = append([]FileAccessPresetHistoryEvent(nil), previous.History...)
	if !sameFileAccessPresetConfig(previous, next) {
		next.History = append(next.History, FileAccessPresetHistoryEvent{
			ID:                        fmt.Sprintf("faph-%d", now.UnixNano()),
			Actor:                     actor,
			Timestamp:                 now,
			Action:                    "updated",
			Summary:                   fmt.Sprintf("Updated file access presets (%d presets)", len(next.Presets)),
			PresetCount:               len(next.Presets),
			Enabled:                   next.Enabled,
			GuestOrganizationBehavior: next.GuestOrganizationBehavior,
			Warning:                   FileAccessPresetWarning,
		})
	}
	return next
}

func sameFileAccessPresetConfig(a, b FileAccessPresetConfig) bool {
	a = comparableFileAccessPresetConfig(a)
	b = comparableFileAccessPresetConfig(b)
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func comparableFileAccessPresetConfig(cfg FileAccessPresetConfig) FileAccessPresetConfig {
	cfg = cloneFileAccessPresetConfig(cfg)
	cfg.History = nil
	for i := range cfg.Presets {
		cfg.Presets[i].CreatedBy = nil
		cfg.Presets[i].CreatedAt = nil
		cfg.Presets[i].UpdatedBy = nil
		cfg.Presets[i].UpdatedAt = nil
	}
	return cfg
}

func sameFileAccessPresetPublicConfig(a, b FileAccessPreset) bool {
	a.CreatedBy, a.CreatedAt, a.UpdatedBy, a.UpdatedAt = nil, nil, nil, nil
	b.CreatedBy, b.CreatedAt, b.UpdatedBy, b.UpdatedAt = nil, nil, nil, nil
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func visibleFileAccessPresets(cfg FileAccessPresetConfig, body FileAccessPresetVisibilityRequest, claims *authmw.Claims) (FileAccessPresetVisibilityResponse, error) {
	cfg, err := normalizeFileAccessPresetConfig(cfg)
	if err != nil {
		return FileAccessPresetVisibilityResponse{}, err
	}
	effectiveOrgID := effectiveFileAccessPresetOrganizationID(body, claims, cfg.GuestOrganizationBehavior)
	resourceKind := strings.ToLower(strings.TrimSpace(body.ResourceKind))
	resp := FileAccessPresetVisibilityResponse{
		Warning:                   FileAccessPresetWarning,
		GuestOrganizationBehavior: cfg.GuestOrganizationBehavior,
		EffectiveOrganizationID:   effectiveOrgID,
		Presets:                   []FileAccessPreset{},
	}
	if !cfg.Enabled {
		return resp, nil
	}
	for _, preset := range cfg.Presets {
		if !preset.Enabled {
			continue
		}
		if len(preset.OrganizationIDs) > 0 && !containsFold(preset.OrganizationIDs, effectiveOrgID) {
			resp.FilteredPresetCount++
			continue
		}
		if resourceKind != "" && len(preset.SupportedResourceKinds) > 0 && !containsFold(preset.SupportedResourceKinds, resourceKind) {
			resp.FilteredPresetCount++
			continue
		}
		if missing := missingFileAccessPresetApplyMarkings(preset, claims); len(missing) > 0 {
			resp.FilteredPresetCount++
			continue
		}
		visible := cloneFileAccessPreset(preset)
		resp.Presets = append(resp.Presets, visible)
	}
	if len(resp.Presets) > 0 {
		resp.DefaultPresetID = resp.Presets[0].ID
	}
	return resp, nil
}

func effectiveFileAccessPresetOrganizationID(body FileAccessPresetVisibilityRequest, claims *authmw.Claims, guestBehavior string) string {
	orgID := strings.TrimSpace(body.OrganizationID)
	if orgID == "" && claims != nil && claims.OrgID != nil {
		orgID = claims.OrgID.String()
	}
	if claims != nil && claims.IsGuestSession() && guestBehavior == FileAccessPresetGuestPrimaryOrganization {
		if primary := strings.TrimSpace(body.PrimaryOrganizationID); primary != "" {
			return primary
		}
		if claims.SessionScope != nil && len(claims.SessionScope.AllowedOrgIDs) > 0 {
			return claims.SessionScope.AllowedOrgIDs[0].String()
		}
	}
	return orgID
}

func missingFileAccessPresetApplyMarkings(preset FileAccessPreset, claims *authmw.Claims) []string {
	if len(preset.MarkingIDs) == 0 {
		return []string{}
	}
	if claims != nil && (claims.HasPermissionKey("markings:apply") || claims.HasPermissionKey("markings:write") || claims.HasPermissionKey("markings:manage")) {
		return []string{}
	}
	allowed := markingApplyIDsFromClaims(claims)
	missing := []string{}
	for _, markingID := range preset.MarkingIDs {
		if !containsFold(allowed, markingID) {
			missing = append(missing, markingID)
		}
	}
	return missing
}

func markingApplyIDsFromClaims(claims *authmw.Claims) []string {
	if claims == nil {
		return []string{}
	}
	values := []string{}
	for _, permission := range claims.Permissions {
		permission = strings.TrimSpace(permission)
		parts := strings.Split(permission, ":")
		if len(parts) == 3 && (parts[0] == "marking" || parts[0] == "markings") && parts[2] == "apply" {
			values = append(values, parts[1])
			continue
		}
		if strings.HasPrefix(permission, "markings:apply:") {
			values = append(values, strings.TrimPrefix(permission, "markings:apply:"))
		}
	}
	if len(claims.Attributes) > 0 {
		var attrs map[string]any
		if err := json.Unmarshal(claims.Attributes, &attrs); err == nil {
			for _, key := range []string{"apply_marking_ids", "marking_apply_ids", "appliable_marking_ids", "allowed_apply_marking_ids"} {
				values = appendAttributeStrings(values, attrs[key])
			}
		}
	}
	return normalizeStringSet(values)
}

func appendAttributeStrings(values []string, raw any) []string {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
	case []string:
		values = append(values, v...)
	case string:
		values = append(values, v)
	}
	return values
}

func defaultScopedSessionConfig() ScopedSessionConfig {
	return ScopedSessionConfig{
		Enabled:              false,
		AllowNoScopedSession: true,
		AlwaysShowSelector:   false,
		AllowedBypassGroups:  []string{},
		Presets:              []ScopedSessionPreset{},
	}
}

func cloneScopedSessionConfig(cfg ScopedSessionConfig) ScopedSessionConfig {
	out := cfg
	out.AllowedBypassGroups = append([]string(nil), cfg.AllowedBypassGroups...)
	out.Presets = make([]ScopedSessionPreset, 0, len(cfg.Presets))
	for _, preset := range cfg.Presets {
		cp := preset
		cp.RequiredMarkings = append([]string(nil), preset.RequiredMarkings...)
		cp.AllowedMarkings = append([]string(nil), preset.AllowedMarkings...)
		out.Presets = append(out.Presets, cp)
	}
	return out
}

func normalizeScopedSessionConfig(cfg ScopedSessionConfig) (ScopedSessionConfig, error) {
	cfg.AllowedBypassGroups = normalizeStringSet(cfg.AllowedBypassGroups)
	presets := make([]ScopedSessionPreset, 0, len(cfg.Presets))
	seen := map[string]struct{}{}
	for _, preset := range cfg.Presets {
		preset.ID = strings.TrimSpace(preset.ID)
		preset.Name = strings.TrimSpace(preset.Name)
		preset.Description = strings.TrimSpace(preset.Description)
		if preset.ID == "" {
			return ScopedSessionConfig{}, errBadScopedSessionConfig("scoped session preset id is required")
		}
		key := strings.ToLower(preset.ID)
		if _, ok := seen[key]; ok {
			return ScopedSessionConfig{}, errBadScopedSessionConfig("scoped session preset ids must be unique")
		}
		seen[key] = struct{}{}
		if preset.Name == "" {
			return ScopedSessionConfig{}, errBadScopedSessionConfig("scoped session preset name is required")
		}
		preset.RequiredMarkings = normalizeStringSet(preset.RequiredMarkings)
		preset.AllowedMarkings = normalizeStringSet(preset.AllowedMarkings)
		if len(preset.AllowedMarkings) == 0 {
			preset.AllowedMarkings = append([]string(nil), preset.RequiredMarkings...)
		}
		if len(preset.RequiredMarkings) == 0 {
			preset.RequiredMarkings = append([]string(nil), preset.AllowedMarkings...)
		}
		if len(preset.AllowedMarkings) == 0 {
			return ScopedSessionConfig{}, errBadScopedSessionConfig("scoped session presets must include at least one marking")
		}
		presets = append(presets, preset)
	}
	cfg.Presets = presets
	return cfg, nil
}

func normalizeStringSet(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func defaultMemberDiscoveryConfig() MemberDiscoveryConfig {
	return MemberDiscoveryConfig{
		DefaultDiscoverUsers:  true,
		DefaultDiscoverGroups: true,
		Warning:               MemberDiscoveryWarning,
		Organizations:         []MemberDiscoveryOrganizationConfig{},
		History:               []MemberDiscoveryHistoryEvent{},
	}
}

func normalizeMemberDiscoveryConfig(cfg MemberDiscoveryConfig) (MemberDiscoveryConfig, error) {
	cfg.Warning = MemberDiscoveryWarning
	orgs := make([]MemberDiscoveryOrganizationConfig, 0, len(cfg.Organizations))
	seen := map[string]struct{}{}
	for _, org := range cfg.Organizations {
		org.OrganizationID = strings.TrimSpace(org.OrganizationID)
		org.OrganizationSlug = strings.TrimSpace(org.OrganizationSlug)
		org.Notes = strings.TrimSpace(org.Notes)
		if org.OrganizationID == "" && org.OrganizationSlug == "" {
			return MemberDiscoveryConfig{}, errBadMemberDiscoveryConfig("member discovery organization_id or organization_slug is required")
		}
		key := strings.ToLower(org.OrganizationID)
		if key == "" {
			key = "slug:" + strings.ToLower(org.OrganizationSlug)
		}
		if _, ok := seen[key]; ok {
			return MemberDiscoveryConfig{}, errBadMemberDiscoveryConfig("member discovery organization entries must be unique")
		}
		seen[key] = struct{}{}
		orgs = append(orgs, org)
	}
	cfg.Organizations = orgs
	cfg.History = append([]MemberDiscoveryHistoryEvent(nil), cfg.History...)
	return cfg, nil
}

func stampMemberDiscoveryChanges(previous, next MemberDiscoveryConfig, actor string, now time.Time) MemberDiscoveryConfig {
	prevByKey := map[string]MemberDiscoveryOrganizationConfig{}
	for _, org := range previous.Organizations {
		prevByKey[memberDiscoveryOrgKey(org.OrganizationID, org.OrganizationSlug)] = org
	}
	history := append([]MemberDiscoveryHistoryEvent(nil), previous.History...)
	for i := range next.Organizations {
		key := memberDiscoveryOrgKey(next.Organizations[i].OrganizationID, next.Organizations[i].OrganizationSlug)
		prev, existed := prevByKey[key]
		changed := !existed ||
			prev.DiscoverUsers != next.Organizations[i].DiscoverUsers ||
			prev.DiscoverGroups != next.Organizations[i].DiscoverGroups ||
			prev.ConsumerModeBoundary != next.Organizations[i].ConsumerModeBoundary ||
			prev.OrganizationSlug != next.Organizations[i].OrganizationSlug ||
			prev.Notes != next.Organizations[i].Notes
		if !changed {
			next.Organizations[i].UpdatedBy = prev.UpdatedBy
			next.Organizations[i].UpdatedAt = prev.UpdatedAt
			continue
		}
		next.Organizations[i].UpdatedBy = &actor
		next.Organizations[i].UpdatedAt = &now
		history = append(history, MemberDiscoveryHistoryEvent{
			ID:                   fmt.Sprintf("mdh-%d-%d", now.UnixNano(), i),
			OrganizationID:       next.Organizations[i].OrganizationID,
			OrganizationSlug:     next.Organizations[i].OrganizationSlug,
			Actor:                actor,
			Timestamp:            now,
			DiscoverUsers:        next.Organizations[i].DiscoverUsers,
			DiscoverGroups:       next.Organizations[i].DiscoverGroups,
			ConsumerModeBoundary: next.Organizations[i].ConsumerModeBoundary,
			Warning:              MemberDiscoveryWarning,
		})
	}
	next.History = history
	next.Warning = MemberDiscoveryWarning
	return next
}

func memberDiscoveryOrgKey(id, slug string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id != "" {
		return id
	}
	return "slug:" + strings.ToLower(strings.TrimSpace(slug))
}

func memberDiscoveryOrganization(cfg MemberDiscoveryConfig, organizationID string) (MemberDiscoveryOrganizationConfig, bool) {
	organizationID = strings.TrimSpace(organizationID)
	if organizationID == "" {
		return MemberDiscoveryOrganizationConfig{}, false
	}
	for _, org := range cfg.Organizations {
		if strings.EqualFold(org.OrganizationID, organizationID) {
			return org, true
		}
	}
	return MemberDiscoveryOrganizationConfig{}, false
}

func memberDiscoveryAllowsUsers(cfg MemberDiscoveryConfig, organizationID string) bool {
	cfg, err := normalizeMemberDiscoveryConfig(cfg)
	if err != nil {
		cfg = defaultMemberDiscoveryConfig()
	}
	if org, ok := memberDiscoveryOrganization(cfg, organizationID); ok {
		return org.DiscoverUsers
	}
	return cfg.DefaultDiscoverUsers
}

func memberDiscoveryAllowsGroups(cfg MemberDiscoveryConfig, organizationID string) bool {
	cfg, err := normalizeMemberDiscoveryConfig(cfg)
	if err != nil {
		cfg = defaultMemberDiscoveryConfig()
	}
	if org, ok := memberDiscoveryOrganization(cfg, organizationID); ok {
		return org.DiscoverGroups
	}
	return cfg.DefaultDiscoverGroups
}

func defaultApplicationAccessConfig() ApplicationAccessConfig {
	cfg := ApplicationAccessConfig{
		Enabled:           true,
		DefaultVisibility: "visible",
		Warning:           ApplicationAccessScopeWarning,
		Applications: []ApplicationAccessApplication{
			{ID: "control-panel", Name: "Control Panel", Category: "Administration", LifecycleStage: "generally_available", Enabled: true},
			{ID: "resource-management", Name: "Resource Management", Category: "Administration", LifecycleStage: "generally_available", Enabled: true},
			{ID: "audit", Name: "Audit", Category: "Security", LifecycleStage: "generally_available", Enabled: true},
			{ID: "datasets", Name: "Datasets", Category: "Data", LifecycleStage: "generally_available", Enabled: true},
			{ID: "lineage", Name: "Lineage", Category: "Data", LifecycleStage: "generally_available", Enabled: true},
			{ID: "pipelines", Name: "Pipelines", Category: "Data", LifecycleStage: "generally_available", Enabled: true},
			{ID: "ontology-manager", Name: "Ontology Manager", Category: "Ontology", LifecycleStage: "generally_available", Enabled: true},
			{ID: "object-explorer", Name: "Object Explorer", Category: "Ontology", LifecycleStage: "generally_available", Enabled: true},
			{ID: "workshop", Name: "Workshop", Category: "Applications", LifecycleStage: "generally_available", Enabled: true},
			{ID: "apps-marketplace", Name: "Applications Marketplace", Category: "Applications", LifecycleStage: "generally_available", Enabled: true},
			{ID: "logic", Name: "Logic", Category: "AIP", LifecycleStage: "beta", Enabled: true},
			{ID: "ai", Name: "AIP", Category: "AIP", LifecycleStage: "beta", Enabled: true},
			{ID: "ml", Name: "Models", Category: "AIP", LifecycleStage: "generally_available", Enabled: true},
			{ID: "notepad", Name: "Notepad", Category: "Analysis", LifecycleStage: "generally_available", Enabled: true},
			{ID: "contour", Name: "Contour", Category: "Analysis", LifecycleStage: "generally_available", Enabled: true},
			{ID: "fusion", Name: "Fusion", Category: "Analysis", LifecycleStage: "generally_available", Enabled: true},
			{ID: "map", Name: "Map", Category: "Analysis", LifecycleStage: "generally_available", Enabled: true},
		},
		Rules: []ApplicationAccessRule{},
		ApprovalPolicy: ApplicationAccessApprovalPolicy{
			Mode:                             "self_approve",
			ReviewerUserIDs:                  []string{},
			ReviewerGroupIDs:                 []string{},
			RequireDistinctReviewerForPolicy: true,
			Instructions:                     "Application access changes are UX-scope controls; reviewers must still rely on backend permission models for data security.",
		},
		ChangeRequests: []ApplicationAccessChangeRequest{},
		History:        []ApplicationAccessHistoryEvent{},
	}
	return cfg
}

func cloneApplicationAccessConfig(cfg ApplicationAccessConfig) ApplicationAccessConfig {
	out := cfg
	out.Applications = append([]ApplicationAccessApplication(nil), cfg.Applications...)
	out.Rules = make([]ApplicationAccessRule, 0, len(cfg.Rules))
	for _, rule := range cfg.Rules {
		cp := rule
		cp.ApplicationIDs = append([]string(nil), rule.ApplicationIDs...)
		cp.OrganizationIDs = append([]string(nil), rule.OrganizationIDs...)
		cp.UserIDs = append([]string(nil), rule.UserIDs...)
		cp.GroupIDs = append([]string(nil), rule.GroupIDs...)
		cp.LifecycleStages = append([]string(nil), rule.LifecycleStages...)
		out.Rules = append(out.Rules, cp)
	}
	out.ApprovalPolicy.ReviewerUserIDs = append([]string(nil), cfg.ApprovalPolicy.ReviewerUserIDs...)
	out.ApprovalPolicy.ReviewerGroupIDs = append([]string(nil), cfg.ApprovalPolicy.ReviewerGroupIDs...)
	out.ChangeRequests = make([]ApplicationAccessChangeRequest, 0, len(cfg.ChangeRequests))
	for _, request := range cfg.ChangeRequests {
		cp := request
		cp.ProposedConfig = cloneApplicationAccessConfigForProposal(request.ProposedConfig)
		out.ChangeRequests = append(out.ChangeRequests, cp)
	}
	out.History = append([]ApplicationAccessHistoryEvent(nil), cfg.History...)
	return out
}

func cloneApplicationAccessConfigForProposal(cfg ApplicationAccessConfig) ApplicationAccessConfig {
	out := cloneApplicationAccessConfig(cfg)
	out.ChangeRequests = []ApplicationAccessChangeRequest{}
	out.History = []ApplicationAccessHistoryEvent{}
	return out
}

func normalizeApplicationAccessConfig(cfg ApplicationAccessConfig) (ApplicationAccessConfig, error) {
	cfg.DefaultVisibility = strings.ToLower(strings.TrimSpace(cfg.DefaultVisibility))
	if cfg.DefaultVisibility == "" {
		cfg.DefaultVisibility = "visible"
	}
	if cfg.DefaultVisibility != "visible" && cfg.DefaultVisibility != "hidden" {
		return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application access default_visibility must be visible or hidden")
	}
	cfg.Warning = ApplicationAccessScopeWarning

	apps := make([]ApplicationAccessApplication, 0, len(cfg.Applications))
	seenApps := map[string]struct{}{}
	for _, app := range cfg.Applications {
		app.ID = strings.TrimSpace(app.ID)
		app.Name = strings.TrimSpace(app.Name)
		app.Description = strings.TrimSpace(app.Description)
		app.Category = strings.TrimSpace(app.Category)
		app.LifecycleStage = normalizeLifecycleStage(app.LifecycleStage)
		if app.ID == "" {
			return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application id is required")
		}
		key := strings.ToLower(app.ID)
		if _, ok := seenApps[key]; ok {
			return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application ids must be unique")
		}
		seenApps[key] = struct{}{}
		if app.Name == "" {
			app.Name = app.ID
		}
		if app.Category == "" {
			app.Category = "Platform"
		}
		if app.LifecycleStage == "" {
			app.LifecycleStage = "generally_available"
		}
		apps = append(apps, app)
	}
	cfg.Applications = apps

	rules := make([]ApplicationAccessRule, 0, len(cfg.Rules))
	seenRules := map[string]struct{}{}
	for _, rule := range cfg.Rules {
		rule.ID = strings.TrimSpace(rule.ID)
		rule.Name = strings.TrimSpace(rule.Name)
		rule.Effect = strings.ToLower(strings.TrimSpace(rule.Effect))
		rule.Reason = strings.TrimSpace(rule.Reason)
		if rule.ID == "" {
			return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application access rule id is required")
		}
		key := strings.ToLower(rule.ID)
		if _, ok := seenRules[key]; ok {
			return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application access rule ids must be unique")
		}
		seenRules[key] = struct{}{}
		if rule.Name == "" {
			rule.Name = rule.ID
		}
		if rule.Effect != "allow" && rule.Effect != "block" {
			return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application access rule effect must be allow or block")
		}
		rule.ApplicationIDs = normalizeStringSet(rule.ApplicationIDs)
		rule.OrganizationIDs = normalizeStringSet(rule.OrganizationIDs)
		rule.UserIDs = normalizeStringSet(rule.UserIDs)
		rule.GroupIDs = normalizeStringSet(rule.GroupIDs)
		rule.LifecycleStages = normalizeLifecycleStages(rule.LifecycleStages)
		rules = append(rules, rule)
	}
	cfg.Rules = rules

	cfg.ApprovalPolicy.Mode = strings.ToLower(strings.TrimSpace(cfg.ApprovalPolicy.Mode))
	if cfg.ApprovalPolicy.Mode == "" {
		cfg.ApprovalPolicy.Mode = "self_approve"
	}
	if cfg.ApprovalPolicy.Mode != "self_approve" && cfg.ApprovalPolicy.Mode != "review_required" {
		return ApplicationAccessConfig{}, errBadApplicationAccessConfig("application access approval policy mode must be self_approve or review_required")
	}
	cfg.ApprovalPolicy.ReviewerUserIDs = normalizeStringSet(cfg.ApprovalPolicy.ReviewerUserIDs)
	cfg.ApprovalPolicy.ReviewerGroupIDs = normalizeStringSet(cfg.ApprovalPolicy.ReviewerGroupIDs)
	cfg.ApprovalPolicy.Instructions = strings.TrimSpace(cfg.ApprovalPolicy.Instructions)
	if cfg.ApprovalPolicy.Mode == "review_required" && len(cfg.ApprovalPolicy.ReviewerUserIDs) == 0 && len(cfg.ApprovalPolicy.ReviewerGroupIDs) == 0 {
		return ApplicationAccessConfig{}, errBadApplicationAccessConfig("review_required application access policy must name reviewer users or groups")
	}
	cfg.ChangeRequests = normalizeApplicationAccessChangeRequests(cfg.ChangeRequests)
	cfg.History = append([]ApplicationAccessHistoryEvent(nil), cfg.History...)
	return cfg, nil
}

func normalizeApplicationAccessChangeRequests(requests []ApplicationAccessChangeRequest) []ApplicationAccessChangeRequest {
	out := make([]ApplicationAccessChangeRequest, 0, len(requests))
	for _, request := range requests {
		request.ID = strings.TrimSpace(request.ID)
		request.Kind = strings.TrimSpace(request.Kind)
		request.Status = strings.TrimSpace(request.Status)
		request.Summary = strings.TrimSpace(request.Summary)
		request.Warning = ApplicationAccessScopeWarning
		request.RequestedBy = strings.TrimSpace(request.RequestedBy)
		request.ProposedConfig = cloneApplicationAccessConfigForProposal(request.ProposedConfig)
		if request.ID != "" {
			out = append(out, request)
		}
	}
	return out
}

func applyApplicationAccessUpdate(settings *ControlPanelSettings, input ApplicationAccessConfig, claims *authmw.Claims) error {
	current, err := normalizeApplicationAccessConfig(settings.ApplicationAccess)
	if err != nil {
		current = defaultApplicationAccessConfig()
	}
	proposed, err := normalizeApplicationAccessConfig(input)
	if err != nil {
		return err
	}
	proposed.ChangeRequests = []ApplicationAccessChangeRequest{}
	proposed.History = []ApplicationAccessHistoryEvent{}

	kind := "configuration"
	if applicationAccessApprovalPolicyChanged(current.ApprovalPolicy, proposed.ApprovalPolicy) {
		kind = "approval_policy"
	}
	actor := applicationAccessActor(claims)
	now := time.Now().UTC()
	request := newApplicationAccessChangeRequest(kind, actor, proposed, now)
	autoApprove := current.ApprovalPolicy.Mode == "self_approve"
	if kind == "approval_policy" && current.ApprovalPolicy.RequireDistinctReviewerForPolicy {
		autoApprove = false
	}
	if autoApprove {
		request.Status = "approved"
		decidedBy := actor
		request.DecidedBy = &decidedBy
		request.DecidedAt = &now
		request.AppliedAt = &now
		proposed.ChangeRequests = append(current.ChangeRequests, request)
		proposed.History = append(current.History, applicationAccessHistoryFromRequest(request, "approved", actor, now))
		settings.ApplicationAccess = proposed
		return nil
	}
	request.Status = "pending"
	current.ChangeRequests = append(current.ChangeRequests, request)
	settings.ApplicationAccess = current
	return nil
}

func decideApplicationAccessChangeRequest(settings *ControlPanelSettings, requestID string, body ApplicationAccessDecisionRequest, claims *authmw.Claims) error {
	current, err := normalizeApplicationAccessConfig(settings.ApplicationAccess)
	if err != nil {
		return err
	}
	decision := strings.ToLower(strings.TrimSpace(body.Decision))
	switch decision {
	case "approve", "approved":
		decision = "approved"
	case "reject", "rejected":
		decision = "rejected"
	default:
		return errBadApplicationAccessConfig("decision must be approved or rejected")
	}
	idx := -1
	for i := range current.ChangeRequests {
		if current.ChangeRequests[i].ID == requestID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return errBadApplicationAccessConfig("application access change request not found")
	}
	request := current.ChangeRequests[idx]
	if request.Status != "pending" {
		return errBadApplicationAccessConfig("application access change request is not pending")
	}
	actor := applicationAccessActor(claims)
	if decision == "approved" && request.Kind == "approval_policy" && current.ApprovalPolicy.RequireDistinctReviewerForPolicy && strings.EqualFold(actor, request.RequestedBy) {
		return errBadApplicationAccessConfig("approval policy changes require a distinct reviewer")
	}
	now := time.Now().UTC()
	request.Status = decision
	request.DecidedBy = &actor
	request.DecidedAt = &now
	if comment := strings.TrimSpace(body.Comment); comment != "" {
		request.Comment = &comment
	}
	if decision == "approved" {
		request.AppliedAt = &now
		proposed, err := normalizeApplicationAccessConfig(request.ProposedConfig)
		if err != nil {
			return err
		}
		proposed.ChangeRequests = append([]ApplicationAccessChangeRequest(nil), current.ChangeRequests...)
		proposed.ChangeRequests[idx] = request
		proposed.History = append(current.History, applicationAccessHistoryFromRequest(request, "approved", actor, now))
		settings.ApplicationAccess = proposed
		return nil
	}
	current.ChangeRequests[idx] = request
	current.History = append(current.History, applicationAccessHistoryFromRequest(request, "rejected", actor, now))
	settings.ApplicationAccess = current
	return nil
}

func newApplicationAccessChangeRequest(kind, actor string, proposed ApplicationAccessConfig, now time.Time) ApplicationAccessChangeRequest {
	return ApplicationAccessChangeRequest{
		ID:             fmt.Sprintf("aacr-%d", now.UnixNano()),
		Kind:           kind,
		Status:         "pending",
		Summary:        applicationAccessSummary(kind, proposed),
		Warning:        ApplicationAccessScopeWarning,
		RequestedBy:    actor,
		RequestedAt:    now,
		ProposedConfig: cloneApplicationAccessConfigForProposal(proposed),
	}
}

func applicationAccessHistoryFromRequest(request ApplicationAccessChangeRequest, action, actor string, now time.Time) ApplicationAccessHistoryEvent {
	return ApplicationAccessHistoryEvent{
		ID:               fmt.Sprintf("aach-%d", now.UnixNano()),
		RequestID:        request.ID,
		Kind:             request.Kind,
		Action:           action,
		Actor:            actor,
		Timestamp:        now,
		Summary:          request.Summary,
		Warning:          ApplicationAccessScopeWarning,
		RuleCount:        len(request.ProposedConfig.Rules),
		ApplicationCount: len(request.ProposedConfig.Applications),
	}
}

func applicationAccessSummary(kind string, cfg ApplicationAccessConfig) string {
	if kind == "approval_policy" {
		return fmt.Sprintf("Update application access approval policy to %s", cfg.ApprovalPolicy.Mode)
	}
	return fmt.Sprintf("Update application access rules (%d applications, %d rules, default %s)", len(cfg.Applications), len(cfg.Rules), cfg.DefaultVisibility)
}

func applicationAccessApprovalPolicyChanged(a, b ApplicationAccessApprovalPolicy) bool {
	if a.Mode != b.Mode || a.RequireDistinctReviewerForPolicy != b.RequireDistinctReviewerForPolicy || a.Instructions != b.Instructions {
		return true
	}
	return !sameStringSet(a.ReviewerUserIDs, b.ReviewerUserIDs) || !sameStringSet(a.ReviewerGroupIDs, b.ReviewerGroupIDs)
}

func evaluateApplicationAccess(cfg ApplicationAccessConfig, body ApplicationAccessEvaluateRequest, claims *authmw.Claims) (ApplicationAccessEvaluateResponse, error) {
	cfg, err := normalizeApplicationAccessConfig(cfg)
	if err != nil {
		return ApplicationAccessEvaluateResponse{}, err
	}
	userID := strings.TrimSpace(body.UserID)
	if userID == "" && claims != nil {
		userID = claims.Sub.String()
	}
	orgID := strings.TrimSpace(body.OrganizationID)
	if orgID == "" && claims != nil && claims.OrgID != nil {
		orgID = claims.OrgID.String()
	}
	groupIDs := normalizeStringSet(body.GroupIDs)
	if len(groupIDs) == 0 && claims != nil {
		groupIDs = groupIDsFromClaims(claims)
	}
	applicationIDs := normalizeStringSet(body.ApplicationIDs)
	if appID := strings.TrimSpace(body.ApplicationID); appID != "" {
		applicationIDs = normalizeStringSet(append(applicationIDs, appID))
	}
	if len(applicationIDs) == 0 {
		for _, app := range cfg.Applications {
			applicationIDs = append(applicationIDs, app.ID)
		}
	}
	if len(applicationIDs) == 0 {
		return ApplicationAccessEvaluateResponse{}, errBadApplicationAccessConfig("application_id or application_ids is required")
	}
	decisions := make([]ApplicationAccessDecision, 0, len(applicationIDs))
	for _, appID := range applicationIDs {
		decisions = append(decisions, evaluateOneApplicationAccess(cfg, appID, userID, orgID, groupIDs, body.LifecycleStage))
	}
	return ApplicationAccessEvaluateResponse{Warning: ApplicationAccessScopeWarning, Decisions: decisions}, nil
}

func evaluateOneApplicationAccess(cfg ApplicationAccessConfig, appID, userID, orgID string, groupIDs []string, lifecycleOverride string) ApplicationAccessDecision {
	app, ok := findApplicationAccessApplication(cfg.Applications, appID)
	lifecycle := normalizeLifecycleStage(lifecycleOverride)
	if lifecycle == "" && ok {
		lifecycle = app.LifecycleStage
	}
	if lifecycle == "" {
		lifecycle = "generally_available"
	}
	decision := ApplicationAccessDecision{
		ApplicationID:     appID,
		Visible:           true,
		Decision:          "visible",
		Reason:            "application access configuration allows this application",
		LifecycleStage:    lifecycle,
		DefaultVisibility: cfg.DefaultVisibility,
		UXScopeOnly:       true,
		MatchedRuleIDs:    []string{},
		MatchedRuleNames:  []string{},
	}
	if !cfg.Enabled {
		decision.Decision = "application_access_disabled"
		decision.Reason = "application access controls are disabled; server-side permissions still apply"
		return decision
	}
	if ok && !app.Enabled {
		decision.Visible = false
		decision.Decision = "application_disabled"
		decision.Reason = "application is disabled in the application access catalog"
		return decision
	}
	allowed := false
	blocked := false
	for _, rule := range cfg.Rules {
		if !applicationAccessRuleMatches(rule, appID, lifecycle, userID, orgID, groupIDs) {
			continue
		}
		decision.MatchedRuleIDs = append(decision.MatchedRuleIDs, rule.ID)
		decision.MatchedRuleNames = append(decision.MatchedRuleNames, rule.Name)
		if rule.Effect == "block" {
			blocked = true
		}
		if rule.Effect == "allow" {
			allowed = true
		}
	}
	if blocked {
		decision.Visible = false
		decision.Decision = "blocked_by_rule"
		decision.Reason = "a matching block rule hides this application from the user experience"
		return decision
	}
	if cfg.DefaultVisibility == "hidden" && !allowed {
		decision.Visible = false
		decision.Decision = "hidden_by_default"
		decision.Reason = "default visibility is hidden and no allow rule matched"
		return decision
	}
	if allowed {
		decision.Decision = "allowed_by_rule"
		decision.Reason = "a matching allow rule makes this application visible"
	}
	return decision
}

func applicationAccessRuleMatches(rule ApplicationAccessRule, appID, lifecycle, userID, orgID string, groupIDs []string) bool {
	if !rule.Enabled {
		return false
	}
	if len(rule.ApplicationIDs) > 0 && !containsFold(rule.ApplicationIDs, appID) {
		return false
	}
	if len(rule.OrganizationIDs) > 0 && !containsFold(rule.OrganizationIDs, orgID) {
		return false
	}
	if len(rule.LifecycleStages) > 0 && !containsFold(rule.LifecycleStages, lifecycle) {
		return false
	}
	hasPrincipalFilter := len(rule.UserIDs) > 0 || len(rule.GroupIDs) > 0
	if !hasPrincipalFilter {
		return true
	}
	if containsFold(rule.UserIDs, userID) {
		return true
	}
	return intersectsFold(rule.GroupIDs, groupIDs)
}

func findApplicationAccessApplication(apps []ApplicationAccessApplication, id string) (ApplicationAccessApplication, bool) {
	for _, app := range apps {
		if strings.EqualFold(app.ID, id) {
			return app, true
		}
	}
	return ApplicationAccessApplication{}, false
}

func groupIDsFromClaims(claims *authmw.Claims) []string {
	if claims == nil || len(claims.Attributes) == 0 {
		return []string{}
	}
	var attrs map[string]any
	if err := json.Unmarshal(claims.Attributes, &attrs); err != nil {
		return []string{}
	}
	values := []string{}
	for _, key := range []string{"group_ids", "groups"} {
		raw, ok := attrs[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					values = append(values, s)
				}
			}
		case []string:
			values = append(values, v...)
		case string:
			values = append(values, v)
		}
	}
	return normalizeStringSet(values)
}

func applicationAccessActor(claims *authmw.Claims) string {
	if claims == nil {
		return "unknown"
	}
	if strings.TrimSpace(claims.Email) != "" {
		return claims.Email
	}
	return claims.Sub.String()
}

func normalizeLifecycleStages(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := normalizeLifecycleStage(value); normalized != "" {
			out = append(out, normalized)
		}
	}
	return normalizeStringSet(out)
}

func normalizeLifecycleStage(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "ga", "general_availability", "generally available":
		return "generally_available"
	case "sunsetted":
		return "sunset"
	default:
		return value
	}
}

func containsFold(values []string, needle string) bool {
	if strings.TrimSpace(needle) == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(value, needle) {
			return true
		}
	}
	return false
}

func intersectsFold(a, b []string) bool {
	for _, left := range a {
		if containsFold(b, left) {
			return true
		}
	}
	return false
}

type errBadScopedSessionConfig string

func (e errBadScopedSessionConfig) Error() string { return string(e) }

type errBadApplicationAccessConfig string

func (e errBadApplicationAccessConfig) Error() string { return string(e) }

type errBadMemberDiscoveryConfig string

func (e errBadMemberDiscoveryConfig) Error() string { return string(e) }

type errBadFileAccessPresetConfig string

func (e errBadFileAccessPresetConfig) Error() string { return string(e) }

func readinessLabel(blockers []string) string {
	if len(blockers) > 0 {
		return "blocked"
	}
	return "ready"
}

func recommendedUpgradeActions(blockers []string) []string {
	if len(blockers) == 0 {
		return []string{"Continue monitoring audit history before rollout."}
	}
	return []string{"Disable maintenance mode or schedule the upgrade inside the approved window."}
}

func requireControlPanelRead(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return nil, false
	}
	if claims.HasRole("admin") || claims.HasPermission("control_panel", "read") || claims.HasPermission("control_panel", "write") {
		return claims, true
	}
	writeJSONErr(w, http.StatusForbidden, "missing permission control_panel:read")
	return nil, false
}

func requireControlPanelWrite(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return nil, false
	}
	if claims.HasRole("admin") || claims.HasPermission("control_panel", "write") {
		return claims, true
	}
	writeJSONErr(w, http.StatusForbidden, "missing permission control_panel:write")
	return nil, false
}
