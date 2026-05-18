package handlers

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func (h *RBAC) memberDiscoveryConfig() MemberDiscoveryConfig {
	if h == nil || h.ControlPanel == nil {
		return defaultMemberDiscoveryConfig()
	}
	h.ControlPanel.mu.RLock()
	defer h.ControlPanel.mu.RUnlock()
	cfg, err := normalizeMemberDiscoveryConfig(h.ControlPanel.settings.MemberDiscovery)
	if err != nil {
		return defaultMemberDiscoveryConfig()
	}
	return cfg
}

func (h *RBAC) allowUserDiscovery(w http.ResponseWriter, r *http.Request, organizationID *uuid.UUID) bool {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return false
	}
	if isMemberDiscoveryAdministrator(claims, "users") {
		return true
	}
	orgID := memberDiscoveryOrgID(claims, organizationID)
	if memberDiscoveryAllowsUsers(h.memberDiscoveryConfig(), orgID) {
		return true
	}
	writeMemberDiscoveryDenied(w, "user", orgID)
	return false
}

func (h *RBAC) allowGroupDiscovery(w http.ResponseWriter, r *http.Request, organizationID *uuid.UUID) bool {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return false
	}
	if isMemberDiscoveryAdministrator(claims, "groups") {
		return true
	}
	orgID := memberDiscoveryOrgID(claims, organizationID)
	if memberDiscoveryAllowsGroups(h.memberDiscoveryConfig(), orgID) {
		return true
	}
	writeMemberDiscoveryDenied(w, "group", orgID)
	return false
}

func (h *RBAC) allowUserDetailDiscovery(w http.ResponseWriter, r *http.Request, userID uuid.UUID, organizationID *uuid.UUID) bool {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "missing claims")
		return false
	}
	if claims.Sub == userID || isMemberDiscoveryAdministrator(claims, "users") {
		return true
	}
	orgID := memberDiscoveryOrgID(claims, organizationID)
	if memberDiscoveryAllowsUsers(h.memberDiscoveryConfig(), orgID) {
		return true
	}
	writeMemberDiscoveryDenied(w, "user", orgID)
	return false
}

func memberDiscoveryOrgID(claims *authmw.Claims, target *uuid.UUID) string {
	if target != nil && *target != uuid.Nil {
		return target.String()
	}
	if claims != nil && claims.OrgID != nil && *claims.OrgID != uuid.Nil {
		return claims.OrgID.String()
	}
	return ""
}

func isMemberDiscoveryAdministrator(claims *authmw.Claims, domain string) bool {
	if claims == nil {
		return false
	}
	if claims.HasRole("admin") || claims.HasRole("organization_admin") || claims.HasRole("org_admin") || claims.HasRole("user_experience_admin") {
		return true
	}
	if claims.HasPermission("control_panel", "read") || claims.HasPermission("control_panel", "write") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "users", "user":
		return claims.HasPermission("users", "read") || claims.HasPermission("users", "write")
	case "groups", "group":
		return claims.HasPermission("groups", "read") || claims.HasPermission("groups", "write")
	default:
		return false
	}
}

func writeMemberDiscoveryDenied(w http.ResponseWriter, resource, organizationID string) {
	if strings.TrimSpace(resource) == "" {
		resource = "member"
	}
	writeJSON(w, http.StatusForbidden, map[string]any{
		"error":           resource + " discovery is disabled for this organization",
		"code":            "member_discovery_disabled",
		"resource":        resource,
		"organization_id": organizationID,
		"warning":         MemberDiscoveryWarning,
	})
}
