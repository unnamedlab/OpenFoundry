package authmw

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// RLSContext is the row-level security context derived from user
// claims. Services use this to scope DB queries to the user's
// org / permissions. Mirrors libs/auth-middleware/src/row_level_security.rs.
type RLSContext struct {
	UserID      uuid.UUID
	OrgID       *uuid.UUID
	Roles       []string
	Permissions []string
	// Attributes preserves the raw JWT attribute payload as JSON
	// bytes so callers can decode it on demand without forcing a
	// concrete shape onto every consumer.
	Attributes json.RawMessage
}

// RLSContextFromClaims is the equivalent of `impl From<&Claims>
// for RlsContext` in the Rust source.
func RLSContextFromClaims(c *Claims) RLSContext {
	roles := make([]string, len(c.Roles))
	copy(roles, c.Roles)
	perms := make([]string, len(c.Permissions))
	copy(perms, c.Permissions)
	attrs := json.RawMessage(nil)
	if len(c.Attributes) > 0 {
		attrs = make(json.RawMessage, len(c.Attributes))
		copy(attrs, c.Attributes)
	}
	return RLSContext{
		UserID:      c.Sub,
		OrgID:       c.OrgID,
		Roles:       roles,
		Permissions: perms,
		Attributes:  attrs,
	}
}

// IsAdmin reports whether the user is an admin (bypasses
// row-level checks).
func (r *RLSContext) IsAdmin() bool {
	for _, role := range r.Roles {
		if role == "admin" {
			return true
		}
	}
	return false
}

// HasPermission reports whether a permission key is present
// (admin short-circuit included).
func (r *RLSContext) HasPermission(permission string) bool {
	if r.IsAdmin() {
		return true
	}
	for _, candidate := range r.Permissions {
		if candidate == permission {
			return true
		}
	}
	return false
}

// OrgFilter renders a SQL fragment that scopes a query by org id.
// Returns "TRUE" for admins, `<column> = '<org>'` when an org id
// is bound, or `<column> IS NULL` otherwise.
//
// The org id is interpolated directly into the fragment because
// it comes from a validated UUID — no SQL-injection vector. Do
// NOT pass user-controlled strings as `column`.
func (r *RLSContext) OrgFilter(column string) string {
	if r.IsAdmin() {
		return "TRUE"
	}
	if r.OrgID != nil {
		return fmt.Sprintf("%s = '%s'", column, r.OrgID.String())
	}
	return fmt.Sprintf("%s IS NULL", column)
}

// OwnerOrOrgFilter renders a SQL fragment that scopes access to
// either the row's owner column or its organization column. Admin
// or `rows:all` permission yields "TRUE".
func (r *RLSContext) OwnerOrOrgFilter(ownerColumn, orgColumn string) string {
	if r.IsAdmin() || r.HasPermission("rows:all") {
		return "TRUE"
	}
	if r.OrgID != nil {
		return fmt.Sprintf(
			"(%s = '%s' OR %s = '%s')",
			ownerColumn, r.UserID.String(),
			orgColumn, r.OrgID.String(),
		)
	}
	return fmt.Sprintf("%s = '%s'", ownerColumn, r.UserID.String())
}
