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
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
)

// RBAC wires the management endpoints (slice 6).
//
// All handlers are bearer-JWT-protected at the router; admin-role
// enforcement is the gateway's job (`x-openfoundry-tenant-tier=enterprise`
// + role checks). Slice 8 layers Cedar policy enforcement on top.
type RBAC struct {
	Repo         *repo.Repo
	ControlPanel *ControlPanel
	OAuthIssuer  OAuthAccessTokenIssuer
}

// ─── Users ──────────────────────────────────────────────────────────────

func (h *RBAC) ListUsers(w http.ResponseWriter, r *http.Request) {
	if !h.allowUserDiscovery(w, r, nil) {
		return
	}
	users, err := h.Repo.ListUsers(r.Context())
	if err != nil {
		slog.Error("list users", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *RBAC) GetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	u, err := h.Repo.FindUserByID(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	if !h.allowUserDetailDiscovery(w, r, u.ID, u.OrganizationID) {
		return
	}
	writeJSON(w, http.StatusOK, u)
}

// Me returns the profile for the authenticated caller, derived from the
// JWT subject. The frontend hits GET /users/me; without this handler chi
// matches /users/{id} with id="me" and parseID rejects it as "invalid id".
func (h *RBAC) Me(w http.ResponseWriter, r *http.Request) {
	claims, _ := authmw.FromContext(r.Context())
	id := authCallerID(r)
	if id == uuid.Nil {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	u, err := h.Repo.FindUserByID(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	roles, rerr := h.Repo.ListUserRoles(r.Context(), id)
	if rerr != nil {
		slog.Warn("me: list roles", slog.String("error", rerr.Error()))
	}
	roleNames := make([]string, 0, len(roles))
	for _, role := range roles {
		roleNames = append(roleNames, role.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":              u.ID,
		"email":           u.Email,
		"name":            u.Name,
		"is_active":       u.IsActive,
		"auth_source":     u.AuthSource,
		"mfa_enforced":    u.MFAEnforced,
		"mfa_enabled":     false,
		"organization_id": u.OrganizationID,
		"attributes":      u.Attributes,
		"roles":           roleNames,
		"groups":          []string{},
		"permissions":     []string{},
		"session_kind":    sessionKindFromClaims(claims),
		"session_scope":   sessionScopeFromClaims(claims),
		"created_at":      u.CreatedAt,
	})
}

func sessionKindFromClaims(claims *authmw.Claims) any {
	if claims == nil || claims.SessionKind == nil {
		return nil
	}
	return *claims.SessionKind
}

func sessionScopeFromClaims(claims *authmw.Claims) any {
	if claims == nil {
		return nil
	}
	return claims.SessionScope
}

func (h *RBAC) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	// SG.4: snapshot the previous is_active so we can detect a
	// false transition and revoke refresh tokens. The repo layer
	// could do this too, but the handler is the natural place to
	// audit the policy "deactivation revokes tokens".
	previous, _ := h.Repo.FindUserByID(r.Context(), id)
	u, err := h.Repo.UpdateUser(r.Context(), id, &body)
	if err != nil {
		slog.Error("update user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if u == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	if previous != nil && previous.IsActive && !u.IsActive {
		count, revokeErr := h.Repo.RevokeAllUserRefreshTokens(r.Context(), id, time.Now().UTC())
		if revokeErr != nil {
			slog.Warn("revoke tokens on deactivation",
				slog.String("user_id", id.String()),
				slog.String("error", revokeErr.Error()))
		} else if count > 0 {
			slog.Info("revoked refresh tokens on deactivation",
				slog.String("user_id", id.String()),
				slog.Int64("revoked", count))
		}
	}
	writeJSON(w, http.StatusOK, u)
}

// DeleteUser performs a soft delete by default: sets deleted_at and
// inactivates the user, and revokes every active refresh token. The
// hard-delete escape hatch is `?hard=true` for compliance flows that
// need a true row removal.
func (h *RBAC) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("hard") == "true" {
		if err := h.Repo.DeleteUser(r.Context(), id); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.Repo.SoftDeleteUser(r.Context(), id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) ListUserRoles(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	roles, err := h.Repo.ListUserRoles(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (h *RBAC) AssignUserRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseID(w, r)
	if !ok {
		return
	}
	roleID, err := uuid.Parse(chi.URLParam(r, "role_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	if err := h.Repo.AssignRoleToUser(r.Context(), userID, roleID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) RevokeUserRole(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseID(w, r)
	if !ok {
		return
	}
	roleID, err := uuid.Parse(chi.URLParam(r, "role_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	if err := h.Repo.RevokeRoleFromUser(r.Context(), userID, roleID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Roles ──────────────────────────────────────────────────────────────

func (h *RBAC) ListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.Repo.ListRoles(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roles)
}

func (h *RBAC) GetRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	role, err := h.Repo.GetRole(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if role == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (h *RBAC) CreateRole(w http.ResponseWriter, r *http.Request) {
	var body models.CreateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name required")
		return
	}
	role, err := h.Repo.CreateRole(r.Context(), &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, role)
}

func (h *RBAC) UpdateRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.UpdateRoleRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	role, err := h.Repo.UpdateRole(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (h *RBAC) DeleteRole(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.Repo.DeleteRole(r.Context(), id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Permissions ────────────────────────────────────────────────────────

func (h *RBAC) ListPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := h.Repo.ListPermissions(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, perms)
}

func (h *RBAC) CreatePermission(w http.ResponseWriter, r *http.Request) {
	var body models.CreatePermissionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Resource == "" || body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, "resource and action required")
		return
	}
	p, err := h.Repo.CreatePermission(r.Context(), &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (h *RBAC) DeletePermission(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.Repo.DeletePermission(r.Context(), id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) AssignRolePermission(w http.ResponseWriter, r *http.Request) {
	roleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	permID, err := uuid.Parse(chi.URLParam(r, "permission_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid permission id")
		return
	}
	if err := h.Repo.AssignPermissionToRole(r.Context(), roleID, permID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) RevokeRolePermission(w http.ResponseWriter, r *http.Request) {
	roleID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid role id")
		return
	}
	permID, err := uuid.Parse(chi.URLParam(r, "permission_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid permission id")
		return
	}
	if err := h.Repo.RevokePermissionFromRole(r.Context(), roleID, permID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Groups ─────────────────────────────────────────────────────────────

func (h *RBAC) ListGroups(w http.ResponseWriter, r *http.Request) {
	if !h.allowGroupDiscovery(w, r, nil) {
		return
	}
	groups, err := h.Repo.ListGroups(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func (h *RBAC) GetGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	g, err := h.Repo.GetGroup(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if g == nil {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	if !h.allowGroupDiscovery(w, r, g.OrganizationID) {
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (h *RBAC) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var body models.CreateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name required")
		return
	}
	g, err := h.Repo.CreateGroup(r.Context(), &body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (h *RBAC) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.UpdateGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	g, err := h.Repo.UpdateGroup(r.Context(), id, &body)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, g)
}

func (h *RBAC) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.Repo.DeleteGroup(r.Context(), id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) AddGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	// SG.5: PUT body is optional. When present, it carries an
	// expires_at for time-bounded membership. Empty body keeps the
	// existing semantics (permanent membership).
	var body models.AddGroupMemberRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid body")
			return
		}
	}
	var addedBy *uuid.UUID
	if caller := authCallerID(r); caller != uuid.Nil {
		c := caller
		addedBy = &c
	}
	if err := h.Repo.AddGroupMember(r.Context(), groupID, userID, addedBy, body.ExpiresAt); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) RemoveGroupMember(w http.ResponseWriter, r *http.Request) {
	groupID, ok := parseID(w, r)
	if !ok {
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if err := h.Repo.RemoveGroupMember(r.Context(), groupID, userID); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── API keys ───────────────────────────────────────────────────────────

const developerAPIKeyMaxTTL = 30 * 24 * time.Hour

var apiKeyTokenPattern = regexp.MustCompile(`ofapikey_[A-Za-z0-9_-]{20,}`)

func (h *RBAC) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	c := authCallerID(r)
	keys, err := h.Repo.ListAPIKeys(r.Context(), c)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, keys)
}

func (h *RBAC) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	c := authCallerID(r)
	if c == uuid.Nil {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body models.CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		writeJSONErr(w, http.StatusBadRequest, "name required")
		return
	}
	expiresAt, err := validateDeveloperAPIKeyExpiry(body.ExpiresAt, time.Now().UTC())
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	roles, permissions, err := h.Repo.ListUserSecuritySnapshot(r.Context(), c)
	if err != nil {
		slog.Error("api key security snapshot", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	scopes, err := deriveAPIKeyScopes(body.Scopes, roles, permissions)
	if err != nil {
		writeJSONErr(w, http.StatusForbidden, err.Error())
		return
	}
	plaintext, err := newAPIKeyPlaintext()
	if err != nil {
		slog.Error("api key mint", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	hash := hashAPIKey(plaintext)
	prefix := visibleAPIKeyPrefix(plaintext)
	k, err := h.Repo.CreateAPIKey(r.Context(), c, body.Name, hash, prefix, scopes, scopes, roles, expiresAt)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, models.CreateAPIKeyResponse{
		ID:        k.ID,
		Name:      k.Name,
		Prefix:    k.Prefix,
		Token:     plaintext,
		Scopes:    k.Scopes,
		ExpiresAt: expiresAt,
		CreatedAt: k.CreatedAt,
		Warning:   k.Warning,
	})
}

func (h *RBAC) RevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	c := authCallerID(r)
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if err := h.Repo.RevokeAPIKey(r.Context(), c, id, time.Now().UTC()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) ScanAPIKeyLeaks(w http.ResponseWriter, r *http.Request) {
	c := authCallerID(r)
	if c == uuid.Nil {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body models.APIKeyLeakScanRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	keys, err := h.Repo.ListAPIKeys(r.Context(), c)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.APIKeyLeakScanResponse{
		Warnings: detectAPIKeyLeakWarnings(body.Content, body.Source, keys),
		Patterns: []string{apiKeyTokenPattern.String()},
	})
}

// ─── helpers ────────────────────────────────────────────────────────────

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

// authCallerID extracts the JWT subject from the bearer middleware.
func authCallerID(r *http.Request) uuid.UUID {
	if c, ok := authmw.FromContext(r.Context()); ok {
		return c.Sub
	}
	return uuid.Nil
}

// newAPIKeyPlaintext returns a 32-byte URL-safe base64 token.
func newAPIKeyPlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ofapikey_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func visibleAPIKeyPrefix(plaintext string) string {
	const max = len("ofapikey_") + 16
	if len(plaintext) <= max {
		return plaintext
	}
	return plaintext[:max]
}

func validateDeveloperAPIKeyExpiry(expiresAt *time.Time, now time.Time) (time.Time, error) {
	if expiresAt == nil {
		return time.Time{}, fmt.Errorf("expires_at is required for temporary developer API tokens")
	}
	exp := expiresAt.UTC()
	if !exp.After(now) {
		return time.Time{}, fmt.Errorf("expires_at must be in the future")
	}
	if exp.After(now.Add(developerAPIKeyMaxTTL)) {
		return time.Time{}, fmt.Errorf("expires_at must be within 30 days")
	}
	return exp, nil
}

func deriveAPIKeyScopes(requested, roles, permissions []string) ([]string, error) {
	requested = normalizeAPIKeyStringSet(requested)
	roles = normalizeAPIKeyStringSet(roles)
	permissions = normalizeAPIKeyStringSet(permissions)
	if len(requested) == 0 {
		if containsString(roles, "admin") {
			return []string{"*:*"}, nil
		}
		return permissions, nil
	}
	if containsString(roles, "admin") || containsString(permissions, "*:*") {
		return requested, nil
	}
	missing := make([]string, 0)
	for _, scope := range requested {
		if !scopeAllowedByPermissions(scope, permissions) {
			missing = append(missing, scope)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("requested scopes exceed caller permissions: %s", strings.Join(missing, ", "))
	}
	return requested, nil
}

func scopeAllowedByPermissions(scope string, permissions []string) bool {
	if containsString(permissions, scope) || containsString(permissions, "*:*") {
		return true
	}
	if i := strings.Index(scope, ":"); i > 0 {
		return containsString(permissions, scope[:i]+":*")
	}
	return false
}

func normalizeAPIKeyStringSet(values []string) []string {
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

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func detectAPIKeyLeakWarnings(content, source string, keys []models.APIKey) []models.APIKeyLeakWarning {
	matches := apiKeyTokenPattern.FindAllString(content, -1)
	if len(matches) == 0 {
		return nil
	}
	known := make(map[string]models.APIKey, len(keys))
	for _, key := range keys {
		if key.Prefix != "" {
			known[key.Prefix] = key
		}
	}
	seen := make(map[string]struct{}, len(matches))
	warnings := make([]models.APIKeyLeakWarning, 0, len(matches))
	for _, match := range matches {
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		prefix := visibleAPIKeyPrefix(match)
		warning := models.APIKeyLeakWarning{
			Source:   strings.TrimSpace(source),
			Prefix:   prefix,
			Redacted: redactAPIKey(match),
			Severity: "high",
			Message:  "Possible committed or shared developer API token. Revoke the token and rotate any derived access credentials.",
		}
		if key, ok := known[prefix]; ok {
			id := key.ID
			warning.APIKeyID = &id
			warning.Severity = "critical"
			warning.Message = "This content appears to include one of your developer API tokens. Revoke it immediately and remove it from shared history."
		}
		warnings = append(warnings, warning)
	}
	return warnings
}

func redactAPIKey(token string) string {
	if len(token) <= len("ofapikey_")+8 {
		return token
	}
	return token[:len("ofapikey_")+4] + "..." + token[len(token)-4:]
}

func bearerTokenFromRequest(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}
