package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

type ScopedSessionRepo interface {
	FindUserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	ListUserGroups(ctx context.Context, userID uuid.UUID) ([]models.Group, error)
	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]models.Role, error)
}

type ScopedSessionIssuer interface {
	IssueTokensWithScope(ctx context.Context, user *models.User, authMethods []string, scope *authmw.SessionScope) (string, string, error)
	AccessTokenTTL() time.Duration
}

type ScopedSessions struct {
	Panel  *ControlPanel
	Repo   ScopedSessionRepo
	Issuer ScopedSessionIssuer
}

type ScopedSessionOptionsResponse struct {
	Enabled                  bool                  `json:"enabled"`
	AllowNoScopedSession     bool                  `json:"allow_no_scoped_session"`
	AlwaysShowSelector       bool                  `json:"always_show_selector"`
	NoScopedSessionAvailable bool                  `json:"no_scoped_session_available"`
	BypassAllowed            bool                  `json:"bypass_allowed"`
	ActiveScopedSession      *ScopedSessionActive  `json:"active_scoped_session,omitempty"`
	FullAllowedMarkings      []string              `json:"full_allowed_markings"`
	Presets                  []ScopedSessionOption `json:"presets"`
}

type ScopedSessionOption struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	RequiredMarkings []string `json:"required_markings"`
	AllowedMarkings  []string `json:"allowed_markings"`
	Enabled          bool     `json:"enabled"`
	Selectable       bool     `json:"selectable"`
	MissingMarkings  []string `json:"missing_markings"`
}

type ScopedSessionActive struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	AllowedMarkings []string `json:"allowed_markings"`
}

type SelectScopedSessionRequest struct {
	PresetID *string `json:"preset_id"`
}

func NewScopedSessions(panel *ControlPanel, repo ScopedSessionRepo, issuer ScopedSessionIssuer) *ScopedSessions {
	return &ScopedSessions{Panel: panel, Repo: repo, Issuer: issuer}
}

func (h *ScopedSessions) Options(w http.ResponseWriter, r *http.Request) {
	claims, user, groups, roles, ok := h.loadCaller(w, r)
	if !ok {
		return
	}
	cfg := h.config()
	writeJSON(w, http.StatusOK, h.optionsFor(cfg, claims, user, groups, roles))
}

func (h *ScopedSessions) Select(w http.ResponseWriter, r *http.Request) {
	claims, user, groups, roles, ok := h.loadCaller(w, r)
	if !ok {
		return
	}
	var body SelectScopedSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	cfg := h.config()
	selectedID := ""
	if body.PresetID != nil {
		selectedID = strings.TrimSpace(*body.PresetID)
	}
	if !cfg.Enabled && selectedID != "" {
		writeJSONErr(w, http.StatusConflict, "scoped sessions are disabled")
		return
	}
	fullAllowed := fullAllowedMarkings(claims, user)
	var scope *authmw.SessionScope
	if selectedID == "" || strings.EqualFold(selectedID, "no_scoped_session") {
		if cfg.Enabled && !canUseNoScopedSession(cfg, claims, groups, roles) {
			writeJSONErr(w, http.StatusForbidden, "no scoped session bypass is not allowed")
			return
		}
		if cfg.Enabled {
			// No-scoped-session is still explicit once scoped sessions are enabled:
			// the organization policy already allowed the bypass, and downstream
			// data planes receive the caller's full marking set instead of
			// inferring it from an absent scope.
			scope = &authmw.SessionScope{AllowedMarkings: append([]string(nil), fullAllowed...)}
		}
	} else {
		preset, found := findScopedSessionPreset(cfg, selectedID)
		if !found || !preset.Enabled {
			writeJSONErr(w, http.StatusNotFound, "scoped session preset not found")
			return
		}
		missing := missingMarkings(fullAllowed, preset.RequiredMarkings)
		if len(missing) > 0 {
			writeJSONErr(w, http.StatusForbidden, "user is missing required markings: "+strings.Join(missing, ", "))
			return
		}
		scope = &authmw.SessionScope{AllowedMarkings: append([]string(nil), preset.AllowedMarkings...)}
	}
	access, refresh, err := h.Issuer.IssueTokensWithScope(r.Context(), user, []string{"scoped_session"}, scope)
	if err != nil {
		slog.Error("scoped session: issue tokens", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "failed to issue scoped session")
		return
	}
	writeJSON(w, http.StatusOK, models.TokenResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.Issuer.AccessTokenTTL().Seconds()),
	})
}

func (h *ScopedSessions) loadCaller(w http.ResponseWriter, r *http.Request) (*authmw.Claims, *models.User, []models.Group, []models.Role, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return nil, nil, nil, nil, false
	}
	user, err := h.Repo.FindUserByID(r.Context(), claims.Sub)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load user")
		return nil, nil, nil, nil, false
	}
	if user == nil || !user.IsActive {
		writeJSONErr(w, http.StatusUnauthorized, "user not found or inactive")
		return nil, nil, nil, nil, false
	}
	groups, err := h.Repo.ListUserGroups(r.Context(), user.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load user groups")
		return nil, nil, nil, nil, false
	}
	roles, err := h.Repo.ListUserRoles(r.Context(), user.ID)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed to load user roles")
		return nil, nil, nil, nil, false
	}
	return claims, user, groups, roles, true
}

func (h *ScopedSessions) config() ScopedSessionConfig {
	if h.Panel == nil {
		return defaultScopedSessionConfig()
	}
	h.Panel.mu.RLock()
	defer h.Panel.mu.RUnlock()
	return cloneScopedSessionConfig(h.Panel.settings.ScopedSessions)
}

func (h *ScopedSessions) optionsFor(cfg ScopedSessionConfig, claims *authmw.Claims, user *models.User, groups []models.Group, roles []models.Role) ScopedSessionOptionsResponse {
	fullAllowed := fullAllowedMarkings(claims, user)
	presets := make([]ScopedSessionOption, 0, len(cfg.Presets))
	for _, preset := range cfg.Presets {
		missing := missingMarkings(fullAllowed, preset.RequiredMarkings)
		presets = append(presets, ScopedSessionOption{
			ID:               preset.ID,
			Name:             preset.Name,
			Description:      preset.Description,
			RequiredMarkings: append([]string(nil), preset.RequiredMarkings...),
			AllowedMarkings:  append([]string(nil), preset.AllowedMarkings...),
			Enabled:          preset.Enabled,
			Selectable:       cfg.Enabled && preset.Enabled && len(missing) == 0,
			MissingMarkings:  missing,
		})
	}
	return ScopedSessionOptionsResponse{
		Enabled:                  cfg.Enabled,
		AllowNoScopedSession:     cfg.AllowNoScopedSession,
		AlwaysShowSelector:       cfg.AlwaysShowSelector,
		NoScopedSessionAvailable: !cfg.Enabled || canUseNoScopedSession(cfg, claims, groups, roles),
		BypassAllowed:            canUseNoScopedSession(cfg, claims, groups, roles),
		ActiveScopedSession:      activeScopedSession(cfg, claims),
		FullAllowedMarkings:      fullAllowed,
		Presets:                  presets,
	}
}

func activeScopedSession(cfg ScopedSessionConfig, claims *authmw.Claims) *ScopedSessionActive {
	if claims.SessionScope == nil || len(claims.SessionScope.AllowedMarkings) == 0 {
		return nil
	}
	activeMarkings := normalizeStringSet(claims.SessionScope.AllowedMarkings)
	for _, preset := range cfg.Presets {
		if preset.Enabled && sameStringSet(activeMarkings, preset.AllowedMarkings) {
			return &ScopedSessionActive{
				ID:              preset.ID,
				Name:            preset.Name,
				AllowedMarkings: activeMarkings,
			}
		}
	}
	return &ScopedSessionActive{
		ID:              "custom",
		Name:            "Scoped session",
		AllowedMarkings: activeMarkings,
	}
}

func canUseNoScopedSession(cfg ScopedSessionConfig, claims *authmw.Claims, groups []models.Group, roles []models.Role) bool {
	if !cfg.AllowNoScopedSession {
		return false
	}
	if claims.HasRole("admin") || hasRoleName(roles, "admin") {
		return true
	}
	if len(cfg.AllowedBypassGroups) == 0 {
		return true
	}
	allowed := stringSet(cfg.AllowedBypassGroups)
	for _, group := range groups {
		if _, ok := allowed[strings.ToLower(group.ID.String())]; ok {
			return true
		}
		if _, ok := allowed[strings.ToLower(group.Name)]; ok {
			return true
		}
	}
	return false
}

func hasRoleName(roles []models.Role, name string) bool {
	for _, role := range roles {
		if strings.EqualFold(role.Name, name) {
			return true
		}
	}
	return false
}

func findScopedSessionPreset(cfg ScopedSessionConfig, id string) (ScopedSessionPreset, bool) {
	for _, preset := range cfg.Presets {
		if strings.EqualFold(preset.ID, id) {
			return preset, true
		}
	}
	return ScopedSessionPreset{}, false
}

func fullAllowedMarkings(claims *authmw.Claims, user *models.User) []string {
	if user != nil && len(user.Attributes) > 0 {
		if values := markingsFromAttributes(user.Attributes); len(values) > 0 {
			return values
		}
		if clearance, ok := clearanceFromAttributes(user.Attributes); ok {
			return markingsForClearance(clearance)
		}
	}
	if claims.HasRole("admin") {
		return []string{"public", "confidential", "pii"}
	}
	if user != nil && len(user.Attributes) > 0 {
		// Keep session-scoped tokens switchable by falling back to the stored
		// user attributes before consulting claims.AllowedMarkings().
		if clearance, ok := clearanceFromAttributes(user.Attributes); ok {
			return markingsForClearance(clearance)
		}
	}
	return normalizeStringSet(claims.AllowedMarkings())
}

func markingsFromAttributes(raw json.RawMessage) []string {
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil
	}
	for _, key := range []string{"allowed_markings", "marking_memberships", "markings"} {
		if values := stringSliceFromAny(attrs[key]); len(values) > 0 {
			return normalizeStringSet(values)
		}
	}
	return nil
}

func clearanceFromAttributes(raw json.RawMessage) (string, bool) {
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return "", false
	}
	clearance, ok := attrs["classification_clearance"].(string)
	return clearance, ok && strings.TrimSpace(clearance) != ""
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return strings.Split(typed, ",")
	default:
		return nil
	}
}

func markingsForClearance(clearance string) []string {
	switch strings.ToLower(strings.TrimSpace(clearance)) {
	case "pii":
		return []string{"public", "confidential", "pii"}
	case "confidential":
		return []string{"public", "confidential"}
	case "public":
		return []string{"public"}
	default:
		return []string{"public"}
	}
}

func missingMarkings(allowed []string, required []string) []string {
	allowedSet := stringSet(allowed)
	missing := make([]string, 0)
	for _, marking := range required {
		if _, ok := allowedSet[strings.ToLower(marking)]; !ok {
			missing = append(missing, marking)
		}
	}
	return missing
}

func sameStringSet(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := stringSet(a)
	for _, value := range b {
		if _, ok := set[strings.ToLower(value)]; !ok {
			return false
		}
	}
	return true
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out[strings.ToLower(value)] = struct{}{}
	}
	return out
}
