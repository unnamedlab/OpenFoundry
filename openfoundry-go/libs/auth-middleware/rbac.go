package authmw

import (
	"net/http"
	"strings"
)

// Well-known role names. Mirror libs/auth-middleware/src/rbac.rs::roles.
const (
	RoleAdmin   = "admin"
	RoleEditor  = "editor"
	RoleViewer  = "viewer"
	RoleService = "service"
)

// RequireRoles is HTTP middleware that requires the authenticated
// user to carry at least one of `required` in its `roles` claim.
// On missing claims it returns 401; on present-but-mismatched it
// returns 403.
//
// Mount under [Middleware] (which stashes [*Claims] into the
// request context) for this to take effect.
func RequireRoles(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := FromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "missing authentication")
				return
			}
			if !c.HasAnyRole(required) {
				writeAuthError(
					w,
					http.StatusForbidden,
					"requires one of: "+strings.Join(required, ", "),
				)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin is HTTP middleware that requires the [RoleAdmin]
// role. Convenience wrapper around [RequireRoles].
func RequireAdmin() func(http.Handler) http.Handler {
	return RequireRoles(RoleAdmin)
}

// RequirePermissions is HTTP middleware that requires the
// authenticated subject to hold at least one of the given
// permission keys (`resource:action` form). Wildcard matching
// follows [Claims.HasPermissionKey] semantics — admin bypass and
// `*:*` / `<resource>:*` are honoured.
func RequirePermissions(required ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := FromContext(r.Context())
			if !ok {
				writeAuthError(w, http.StatusUnauthorized, "missing authentication")
				return
			}
			matched := false
			for _, p := range required {
				if c.HasPermissionKey(p) {
					matched = true
					break
				}
			}
			if !matched {
				writeAuthError(
					w,
					http.StatusForbidden,
					"requires one of: "+strings.Join(required, ", "),
				)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
