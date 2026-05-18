package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func (h *RBAC) ListThirdPartyApplications(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireThirdPartyApplicationRead(w, r); !ok {
		return
	}
	apps, err := h.Repo.ListThirdPartyApplications(r.Context(), r.URL.Query().Get("include_revoked") == "true")
	if err != nil {
		slog.Error("list third-party applications", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":   apps,
		"total":   len(apps),
		"warning": models.ThirdPartyApplicationRegistrationWarning,
	})
}

func (h *RBAC) CreateThirdPartyApplication(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationAdmin(w, r)
	if !ok {
		return
	}
	var body models.CreateThirdPartyApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	app, serviceUser, clientSecret, clientSecretHash, err := buildThirdPartyApplicationFromCreate(body, claims)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !canManageThirdPartyApplicationOrganization(claims, app.ManagingOrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "missing Manage OAuth 2.0 clients permission for managing organization")
		return
	}
	created, err := h.Repo.CreateThirdPartyApplication(r.Context(), &app, serviceUser, clientSecretHash)
	if err != nil {
		slog.Error("create third-party application", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, models.CreateThirdPartyApplicationResponse{
		Application:  *created,
		ClientSecret: clientSecret,
		Warning:      models.ThirdPartyApplicationRegistrationWarning,
	})
}

func (h *RBAC) GetThirdPartyApplication(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationRead(w, r)
	if !ok {
		return
	}
	app, ok := h.loadThirdPartyApplication(w, r)
	if !ok {
		return
	}
	if !canReadThirdPartyApplication(claims, app) {
		writeJSONErr(w, http.StatusForbidden, "forbidden")
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func (h *RBAC) UpdateThirdPartyApplication(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationAdmin(w, r)
	if !ok {
		return
	}
	current, ok := h.loadThirdPartyApplication(w, r)
	if !ok {
		return
	}
	if !canManageThirdPartyApplicationOrganization(claims, current.ManagingOrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "missing Manage OAuth 2.0 clients permission for managing organization")
		return
	}
	var body models.UpdateThirdPartyApplicationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	updated, serviceUser, err := applyThirdPartyApplicationUpdate(*current, body, claims)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := h.Repo.UpdateThirdPartyApplication(r.Context(), &updated, serviceUser)
	if err != nil {
		slog.Error("update third-party application", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (h *RBAC) DeleteThirdPartyApplication(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationAdmin(w, r)
	if !ok {
		return
	}
	app, ok := h.loadThirdPartyApplication(w, r)
	if !ok {
		return
	}
	if !canManageThirdPartyApplicationOrganization(claims, app.ManagingOrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "missing Manage OAuth 2.0 clients permission for managing organization")
		return
	}
	if err := h.Repo.RevokeThirdPartyApplication(r.Context(), app.ID, claims.Sub, time.Now().UTC()); err != nil {
		slog.Error("revoke third-party application", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) RotateThirdPartyApplicationSecret(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationAdmin(w, r)
	if !ok {
		return
	}
	app, ok := h.loadThirdPartyApplication(w, r)
	if !ok {
		return
	}
	if app.ClientType != models.ThirdPartyClientTypeConfidential {
		writeJSONErr(w, http.StatusBadRequest, "public clients do not have client secrets")
		return
	}
	if !canManageThirdPartyApplicationOrganization(claims, app.ManagingOrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "missing Manage OAuth 2.0 clients permission for managing organization")
		return
	}
	secret, err := newOAuthClientSecret()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	saved, err := h.Repo.RotateThirdPartyApplicationSecret(
		r.Context(),
		app.ID,
		hashOAuthClientSecret(secret),
		visibleOAuthClientSecretPrefix(secret),
		claims.Sub,
		time.Now().UTC(),
	)
	if err != nil {
		slog.Error("rotate third-party application secret", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, models.RotateThirdPartyApplicationSecretResponse{
		Application:  *saved,
		ClientSecret: secret,
		Warning:      "Copy the new client secret now; it will not be shown again.",
	})
}

func (h *RBAC) UpsertThirdPartyApplicationEnablement(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationAdmin(w, r)
	if !ok {
		return
	}
	app, ok := h.loadThirdPartyApplication(w, r)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "organization_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !canManageThirdPartyApplicationOrganization(claims, orgID) && !canManageThirdPartyApplicationOrganization(claims, app.ManagingOrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "missing Manage OAuth 2.0 clients permission for organization")
		return
	}
	var body models.UpsertThirdPartyApplicationEnablementRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.ProjectResourceIDs = normalizeThirdPartyStringSet(body.ProjectResourceIDs)
	body.MarkingIDs = normalizeThirdPartyStringSet(body.MarkingIDs)
	saved, err := h.Repo.UpsertThirdPartyApplicationEnablement(r.Context(), app.ID, orgID, body, claims.Sub)
	if err != nil {
		slog.Error("upsert third-party application enablement", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (h *RBAC) DisableThirdPartyApplicationEnablement(w http.ResponseWriter, r *http.Request) {
	claims, ok := requireThirdPartyApplicationAdmin(w, r)
	if !ok {
		return
	}
	app, ok := h.loadThirdPartyApplication(w, r)
	if !ok {
		return
	}
	orgID, err := uuid.Parse(chi.URLParam(r, "organization_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid organization id")
		return
	}
	if !canManageThirdPartyApplicationOrganization(claims, orgID) && !canManageThirdPartyApplicationOrganization(claims, app.ManagingOrganizationID) {
		writeJSONErr(w, http.StatusForbidden, "missing Manage OAuth 2.0 clients permission for organization")
		return
	}
	saved, err := h.Repo.DisableThirdPartyApplicationEnablement(r.Context(), app.ID, orgID, claims.Sub)
	if err != nil {
		slog.Error("disable third-party application enablement", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, saved)
}

func (h *RBAC) loadThirdPartyApplication(w http.ResponseWriter, r *http.Request) (*models.ThirdPartyApplication, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return nil, false
	}
	app, err := h.Repo.GetThirdPartyApplication(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if app == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return nil, false
	}
	return app, true
}

func buildThirdPartyApplicationFromCreate(body models.CreateThirdPartyApplicationRequest, claims *authmw.Claims) (models.ThirdPartyApplication, *models.ThirdPartyAppServiceUserSeed, *string, *string, error) {
	name := strings.TrimSpace(body.Name)
	if name == "" {
		return models.ThirdPartyApplication{}, nil, nil, nil, fmt.Errorf("name is required")
	}
	managingOrg := body.ManagingOrganizationID
	if managingOrg == nil {
		managingOrg = claims.OrgID
	}
	if managingOrg == nil {
		return models.ThirdPartyApplication{}, nil, nil, nil, fmt.Errorf("managing_organization_id is required")
	}
	app := models.ThirdPartyApplication{
		ID:                          uuid.New(),
		ClientID:                    newOAuthClientID(),
		Name:                        name,
		Description:                 trimOptionalString(body.Description),
		LogoURL:                     trimOptionalString(body.LogoURL),
		ClientType:                  strings.TrimSpace(body.ClientType),
		EnabledGrantTypes:           normalizeThirdPartyStringSet(body.EnabledGrantTypes),
		RedirectURIs:                normalizeThirdPartyStringSet(body.RedirectURIs),
		Scopes:                      normalizeThirdPartyStringSet(body.Scopes),
		OwnerUserIDs:                normalizeUUIDSet(body.OwnerUserIDs),
		ManagingOrganizationID:      *managingOrg,
		DiscoverableOrganizationIDs: normalizeUUIDSet(body.DiscoverableOrganizationIDs),
		PreferredManagementSurface:  models.ThirdPartyManagementDeveloperConsole,
		ControlPanelFallback:        true,
		CreatedBy:                   &claims.Sub,
		UpdatedBy:                   &claims.Sub,
	}
	if app.ClientType == "" {
		app.ClientType = models.ThirdPartyClientTypeConfidential
	}
	if len(app.EnabledGrantTypes) == 0 {
		app.EnabledGrantTypes = []string{models.ThirdPartyGrantAuthorizationCode}
	}
	if len(app.OwnerUserIDs) == 0 {
		app.OwnerUserIDs = []uuid.UUID{claims.Sub}
	}
	if len(app.DiscoverableOrganizationIDs) == 0 {
		app.DiscoverableOrganizationIDs = []uuid.UUID{app.ManagingOrganizationID}
	}
	if body.PreferredManagementSurface != nil && strings.TrimSpace(*body.PreferredManagementSurface) != "" {
		app.PreferredManagementSurface = strings.TrimSpace(*body.PreferredManagementSurface)
	}
	if body.ControlPanelFallback != nil {
		app.ControlPanelFallback = *body.ControlPanelFallback
	}
	for _, orgID := range normalizeUUIDSet(body.EnablementOrganizationIDs) {
		app.Enablements = append(app.Enablements, models.ThirdPartyApplicationEnablement{
			ApplicationID:  app.ID,
			OrganizationID: orgID,
			Enabled:        true,
			UpdatedBy:      &claims.Sub,
		})
	}
	if err := validateThirdPartyApplicationRegistration(&app); err != nil {
		return models.ThirdPartyApplication{}, nil, nil, nil, err
	}
	var clientSecret *string
	var clientSecretHash *string
	if app.ClientType == models.ThirdPartyClientTypeConfidential {
		secret, err := newOAuthClientSecret()
		if err != nil {
			return models.ThirdPartyApplication{}, nil, nil, nil, err
		}
		clientSecret = &secret
		hash := hashOAuthClientSecret(secret)
		clientSecretHash = &hash
		prefix := visibleOAuthClientSecretPrefix(secret)
		now := time.Now().UTC()
		app.ClientSecretPrefix = &prefix
		app.ClientSecretCreatedAt = &now
	}
	serviceUser := serviceUserSeedForThirdPartyApplication(&app, claims.Sub)
	return app, serviceUser, clientSecret, clientSecretHash, nil
}

func applyThirdPartyApplicationUpdate(current models.ThirdPartyApplication, body models.UpdateThirdPartyApplicationRequest, claims *authmw.Claims) (models.ThirdPartyApplication, *models.ThirdPartyAppServiceUserSeed, error) {
	if body.Name != nil {
		current.Name = strings.TrimSpace(*body.Name)
	}
	if body.Description != nil {
		current.Description = trimOptionalString(*body.Description)
	}
	if body.LogoURL != nil {
		current.LogoURL = trimOptionalString(*body.LogoURL)
	}
	if body.ClientType != nil {
		nextClientType := strings.TrimSpace(*body.ClientType)
		if nextClientType != current.ClientType {
			return models.ThirdPartyApplication{}, nil, fmt.Errorf("client_type cannot be changed after registration")
		}
	}
	if body.EnabledGrantTypes != nil {
		current.EnabledGrantTypes = normalizeThirdPartyStringSet(*body.EnabledGrantTypes)
	}
	if body.RedirectURIs != nil {
		current.RedirectURIs = normalizeThirdPartyStringSet(*body.RedirectURIs)
	}
	if body.Scopes != nil {
		current.Scopes = normalizeThirdPartyStringSet(*body.Scopes)
	}
	if body.OwnerUserIDs != nil {
		current.OwnerUserIDs = normalizeUUIDSet(*body.OwnerUserIDs)
	}
	if body.DiscoverableOrganizationIDs != nil {
		current.DiscoverableOrganizationIDs = normalizeUUIDSet(*body.DiscoverableOrganizationIDs)
	}
	if body.PreferredManagementSurface != nil && strings.TrimSpace(*body.PreferredManagementSurface) != "" {
		current.PreferredManagementSurface = strings.TrimSpace(*body.PreferredManagementSurface)
	}
	if body.ControlPanelFallback != nil {
		current.ControlPanelFallback = *body.ControlPanelFallback
	}
	if len(current.OwnerUserIDs) == 0 {
		current.OwnerUserIDs = []uuid.UUID{claims.Sub}
	}
	if len(current.DiscoverableOrganizationIDs) == 0 {
		current.DiscoverableOrganizationIDs = []uuid.UUID{current.ManagingOrganizationID}
	}
	current.UpdatedBy = &claims.Sub
	if err := validateThirdPartyApplicationRegistration(&current); err != nil {
		return models.ThirdPartyApplication{}, nil, err
	}
	return current, serviceUserSeedForThirdPartyApplication(&current, claims.Sub), nil
}

func validateThirdPartyApplicationRegistration(app *models.ThirdPartyApplication) error {
	if app.Name == "" {
		return fmt.Errorf("name is required")
	}
	switch app.ClientType {
	case models.ThirdPartyClientTypeConfidential, models.ThirdPartyClientTypePublic:
	default:
		return fmt.Errorf("client_type must be confidential or public")
	}
	grants := normalizeThirdPartyStringSet(app.EnabledGrantTypes)
	if len(grants) == 0 {
		return fmt.Errorf("at least one grant type is required")
	}
	for _, grant := range grants {
		switch grant {
		case models.ThirdPartyGrantAuthorizationCode, models.ThirdPartyGrantClientCredentials:
		default:
			return fmt.Errorf("unsupported grant type %q", grant)
		}
	}
	app.EnabledGrantTypes = grants
	if app.ClientType == models.ThirdPartyClientTypePublic && containsThirdPartyString(grants, models.ThirdPartyGrantClientCredentials) {
		return fmt.Errorf("public clients cannot use client_credentials")
	}
	if containsThirdPartyString(grants, models.ThirdPartyGrantAuthorizationCode) && len(app.RedirectURIs) == 0 {
		return fmt.Errorf("authorization_code grant requires at least one redirect URI")
	}
	for _, redirectURI := range app.RedirectURIs {
		if err := validateOAuthRedirectURI(redirectURI); err != nil {
			return err
		}
	}
	switch app.PreferredManagementSurface {
	case models.ThirdPartyManagementDeveloperConsole, models.ThirdPartyManagementControlPanel:
	default:
		return fmt.Errorf("preferred_management_surface must be developer_console or control_panel_fallback")
	}
	app.RequiresPKCE = app.ClientType == models.ThirdPartyClientTypePublic
	return nil
}

func serviceUserSeedForThirdPartyApplication(app *models.ThirdPartyApplication, actor uuid.UUID) *models.ThirdPartyAppServiceUserSeed {
	if app.ClientType != models.ThirdPartyClientTypeConfidential || !containsThirdPartyString(app.EnabledGrantTypes, models.ThirdPartyGrantClientCredentials) {
		return nil
	}
	serviceUserID := app.ServiceUserID
	if serviceUserID == nil || *serviceUserID == uuid.Nil {
		id := uuid.New()
		serviceUserID = &id
		app.ServiceUserID = serviceUserID
	}
	attrs, _ := json.Marshal(map[string]any{
		"service_user":    true,
		"oauth_client_id": app.ClientID,
		"application_id":  app.ID.String(),
	})
	return &models.ThirdPartyAppServiceUserSeed{
		ID:             *serviceUserID,
		Email:          app.ClientID + "@service.openfoundry.local",
		Username:       app.ClientID,
		Name:           app.Name + " service user",
		OrganizationID: app.ManagingOrganizationID,
		Attributes:     attrs,
		CreatedBy:      actor,
	}
}

func validateOAuthRedirectURI(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("redirect URI %q must be absolute", raw)
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("redirect URI %q must not include a fragment", raw)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLocalOAuthRedirectHost(parsed.Hostname())) {
		return fmt.Errorf("redirect URI %q must use https unless it targets localhost", raw)
	}
	return nil
}

func isLocalOAuthRedirectHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func requireThirdPartyApplicationRead(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if canReadThirdPartyApplicationsGlobally(claims) || canManageOAuthClients(claims) {
		return claims, true
	}
	writeJSONErr(w, http.StatusForbidden, "third-party application administrator or OAuth client management permission required")
	return nil, false
}

func requireThirdPartyApplicationAdmin(w http.ResponseWriter, r *http.Request) (*authmw.Claims, bool) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	if canManageOAuthClients(claims) {
		return claims, true
	}
	writeJSONErr(w, http.StatusForbidden, "third-party application administrator or Manage OAuth 2.0 clients permission required")
	return nil, false
}

func canReadThirdPartyApplication(claims *authmw.Claims, app *models.ThirdPartyApplication) bool {
	return canReadThirdPartyApplicationsGlobally(claims) || canManageThirdPartyApplicationOrganization(claims, app.ManagingOrganizationID)
}

func canReadThirdPartyApplicationsGlobally(claims *authmw.Claims) bool {
	return claims.HasRole("admin") ||
		claims.HasPermissionKey("oauth_clients:read") ||
		claims.HasPermissionKey("third_party_applications:read")
}

func canManageThirdPartyApplicationOrganization(claims *authmw.Claims, orgID uuid.UUID) bool {
	if claims.HasRole("admin") ||
		claims.HasRole("third_party_application_admin") ||
		claims.HasRole("third_party_application_administrator") ||
		claims.HasPermissionKey("oauth_clients:manage") ||
		claims.HasPermissionKey("third_party_applications:manage") {
		return true
	}
	if claims.OrgID == nil || *claims.OrgID != orgID {
		return false
	}
	return canManageOAuthClients(claims)
}

func canManageOAuthClients(claims *authmw.Claims) bool {
	return claims.HasRole("admin") ||
		claims.HasRole("third_party_application_admin") ||
		claims.HasRole("third_party_application_administrator") ||
		claims.HasPermissionKey("oauth_clients:manage") ||
		claims.HasPermissionKey("third_party_applications:manage")
}

func newOAuthClientID() string {
	if id, err := uuid.NewV7(); err == nil {
		return "of3pa_" + strings.ReplaceAll(id.String(), "-", "")
	}
	return "of3pa_" + strings.ReplaceAll(uuid.New().String(), "-", "")
}

func newOAuthClientSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "of3pa_secret_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashOAuthClientSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func visibleOAuthClientSecretPrefix(secret string) string {
	const max = len("of3pa_secret_") + 10
	if len(secret) <= max {
		return secret
	}
	return secret[:max]
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeThirdPartyStringSet(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeUUIDSet(values []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(values))
	out := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

func containsThirdPartyString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
