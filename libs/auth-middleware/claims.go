package authmw

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SessionScope mirrors the Rust SessionScope struct. Field order and
// JSON tags are preserved verbatim — every JWT issued by either Rust
// or Go services round-trips through this type unchanged.
type SessionScope struct {
	AllowedMethods          []string    `json:"allowed_methods,omitempty"`
	AllowedPathPrefixes     []string    `json:"allowed_path_prefixes,omitempty"`
	AllowedSubjectIDs       []string    `json:"allowed_subject_ids,omitempty"`
	AllowedOrgIDs           []uuid.UUID `json:"allowed_org_ids,omitempty"`
	Workspace               *string     `json:"workspace,omitempty"`
	ClassificationClearance *string     `json:"classification_clearance,omitempty"`
	AllowedMarkings         []string    `json:"allowed_markings,omitempty"`
	RestrictedViewIDs       []uuid.UUID `json:"restricted_view_ids,omitempty"`
	ConsumerMode            bool        `json:"consumer_mode,omitempty"`
	GuestEmail              *string     `json:"guest_email,omitempty"`
	GuestDisplayName        *string     `json:"guest_display_name,omitempty"`
}

// Claims is the canonical JWT payload. Same field set + JSON tags as
// the Rust `auth_middleware::claims::Claims`.
type Claims struct {
	Sub          uuid.UUID       `json:"sub"`
	IAT          int64           `json:"iat"`
	EXP          int64           `json:"exp"`
	ISS          *string         `json:"iss,omitempty"`
	AUD          *string         `json:"aud,omitempty"`
	JTI          uuid.UUID       `json:"jti"`
	Email        string          `json:"email"`
	Name         string          `json:"name"`
	Roles        []string        `json:"roles"`
	Permissions  []string        `json:"permissions,omitempty"`
	OrgID        *uuid.UUID      `json:"org_id,omitempty"`
	Attributes   json.RawMessage `json:"attributes,omitempty"`
	AuthMethods  []string        `json:"auth_methods,omitempty"`
	TokenUse     *string         `json:"token_use,omitempty"`
	APIKeyID     *uuid.UUID      `json:"api_key_id,omitempty"`
	SessionKind  *string         `json:"session_kind,omitempty"`
	SessionScope *SessionScope   `json:"session_scope,omitempty"`
}

// IsExpired returns true when c.EXP has already passed.
func (c *Claims) IsExpired() bool { return c.EXP < time.Now().Unix() }

// HasRole reports whether the subject carries `role` in its roles claim.
func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// HasAnyRole reports whether the subject carries at least one of `roles`.
func (c *Claims) HasAnyRole(roles []string) bool {
	for _, r := range roles {
		if c.HasRole(r) {
			return true
		}
	}
	return false
}

// HasPermissionKey resolves the same wildcard rules as the Rust impl:
//   - exact match on `resource:action`
//   - global wildcard `*:*`
//   - per-resource wildcard `<resource>:*`
//   - admin role short-circuit
func (c *Claims) HasPermissionKey(permission string) bool {
	if c.HasRole("admin") {
		return true
	}
	resourceWildcard := ""
	if idx := strings.Index(permission, ":"); idx > 0 {
		resourceWildcard = permission[:idx] + ":*"
	}
	for _, candidate := range c.Permissions {
		if candidate == permission || candidate == "*:*" ||
			(resourceWildcard != "" && candidate == resourceWildcard) {
			return true
		}
	}
	return false
}

// HasPermission is a convenience wrapper joining resource + action.
func (c *Claims) HasPermission(resource, action string) bool {
	return c.HasPermissionKey(resource + ":" + action)
}

// IsGuestSession reports whether the claims describe a guest session,
// preserving the dual-source rule from the Rust impl.
func (c *Claims) IsGuestSession() bool {
	if c.SessionKind != nil && *c.SessionKind == "guest_session" {
		return true
	}
	if c.SessionScope != nil && c.SessionScope.GuestEmail != nil && *c.SessionScope.GuestEmail != "" {
		return true
	}
	return false
}

// ClassificationClearance returns the effective clearance, preferring
// the session-scoped value over arbitrary attributes (matches Rust).
func (c *Claims) ClassificationClearance() (string, bool) {
	if c.SessionScope != nil && c.SessionScope.ClassificationClearance != nil {
		return *c.SessionScope.ClassificationClearance, true
	}
	if len(c.Attributes) == 0 {
		return "", false
	}
	var attrs map[string]any
	if err := json.Unmarshal(c.Attributes, &attrs); err != nil {
		return "", false
	}
	v, ok := attrs["classification_clearance"].(string)
	return v, ok
}

// HasActiveMarkingScope reports whether this token carries an explicit
// scoped-session marking subset. When true, the subset is authoritative:
// callers must not widen access from role, clearance, or stored membership.
func (c *Claims) HasActiveMarkingScope() bool {
	return c != nil && c.SessionScope != nil && len(c.SessionScope.AllowedMarkings) > 0
}

// AllowedMarkings returns the effective marking allowlist. A scoped-session
// subset wins over the normal classification/admin cascade so downstream
// services can make one decision for filesystem, Ontology, API, and analytics
// reads.
func (c *Claims) AllowedMarkings() []string {
	if c == nil {
		return nil
	}
	if c.SessionScope != nil && len(c.SessionScope.AllowedMarkings) > 0 {
		out := make([]string, len(c.SessionScope.AllowedMarkings))
		copy(out, c.SessionScope.AllowedMarkings)
		return out
	}
	clearance, _ := c.ClassificationClearance()
	switch clearance {
	case "pii":
		return []string{"public", "confidential", "pii"}
	case "confidential":
		return []string{"public", "confidential"}
	case "public":
		return []string{"public"}
	}
	if c.HasRole("admin") {
		return []string{"public", "confidential", "pii"}
	}
	return []string{"public"}
}

// AllowsMarking reports whether the effective active session permits `marking`.
// Admin role bypass applies only when there is no explicit scoped-session
// marking subset. This prevents an active scoped session from cross-pollinating
// into data the same user could normally access outside the selected scope.
func (c *Claims) AllowsMarking(marking string) bool {
	if c == nil {
		return false
	}
	if !c.HasActiveMarkingScope() && c.HasRole("admin") {
		return true
	}
	for _, m := range c.AllowedMarkings() {
		if asciiEqualFold(m, marking) {
			return true
		}
	}
	return false
}

// MissingAllowedMarkings returns the subset of `markings` that the caller's
// current session does not permit. Empty/blank requirements are ignored.
func (c *Claims) MissingAllowedMarkings(markings []string) []string {
	missing := make([]string, 0)
	for _, marking := range markings {
		marking = strings.TrimSpace(marking)
		if marking == "" {
			continue
		}
		if !c.AllowsMarking(marking) {
			missing = append(missing, marking)
		}
	}
	return missing
}

// AllowsAllMarkings reports whether every supplied marking is allowed by the
// caller's current session.
func (c *Claims) AllowsAllMarkings(markings []string) bool {
	return len(c.MissingAllowedMarkings(markings)) == 0
}

// AllowsHTTPMethod returns true when the session scope permits `method`.
func (c *Claims) AllowsHTTPMethod(method string) bool {
	if c.SessionScope == nil || len(c.SessionScope.AllowedMethods) == 0 {
		return true
	}
	for _, m := range c.SessionScope.AllowedMethods {
		if asciiEqualFold(m, method) || m == "*" {
			return true
		}
	}
	return false
}

// AllowedOrgIDs returns the effective org-id allowlist. When the
// session scope carries an explicit list, that list wins; otherwise
// the function falls back to a single-element slice with c.OrgID
// (or empty when OrgID is nil) — same shape as the Rust source.
func (c *Claims) AllowedOrgIDs() []uuid.UUID {
	if c.SessionScope != nil && len(c.SessionScope.AllowedOrgIDs) > 0 {
		out := make([]uuid.UUID, len(c.SessionScope.AllowedOrgIDs))
		copy(out, c.SessionScope.AllowedOrgIDs)
		return out
	}
	if c.OrgID != nil {
		return []uuid.UUID{*c.OrgID}
	}
	return nil
}

// AllowsOrgID reports whether the subject may access the requested
// organization boundary.
//
// Admins are always allowed. Otherwise: if the effective allowlist
// (see [AllowedOrgIDs]) is empty the call passes through. With a
// non-empty allowlist a concrete resourceOrg must be a member; nil
// resourceOrg passes only when the session is not a guest session
// (mirrors Rust's `org_id: None` branch).
func (c *Claims) AllowsOrgID(resourceOrg *uuid.UUID) bool {
	if c.HasRole("admin") {
		return true
	}
	allowed := c.AllowedOrgIDs()
	if len(allowed) == 0 {
		return true
	}
	if resourceOrg == nil {
		return !c.IsGuestSession()
	}
	for _, id := range allowed {
		if id == *resourceOrg {
			return true
		}
	}
	return false
}

// AllowsSubjectID reports whether the session scope permits the
// given subject identifier. A nil scope or an empty allowlist
// matches everything; otherwise the candidate must be present and
// non-nil.
func (c *Claims) AllowsSubjectID(subjectID *string) bool {
	if c.SessionScope == nil || len(c.SessionScope.AllowedSubjectIDs) == 0 {
		return true
	}
	if subjectID == nil {
		return false
	}
	for _, id := range c.SessionScope.AllowedSubjectIDs {
		if id == *subjectID {
			return true
		}
	}
	return false
}

// Attribute fetches an attribute from the JWT's free-form attributes
// claim. Returns the decoded value (any) and true when present; the
// second return is false when Attributes is empty, malformed, or
// missing the key.
func (c *Claims) Attribute(key string) (any, bool) {
	if len(c.Attributes) == 0 {
		return nil, false
	}
	var attrs map[string]any
	if err := json.Unmarshal(c.Attributes, &attrs); err != nil {
		return nil, false
	}
	v, ok := attrs[key]
	return v, ok
}

// IssuedAt returns the token's `iat` claim as a UTC time.Time.
func (c *Claims) IssuedAt() time.Time {
	return time.Unix(c.IAT, 0).UTC()
}

// ExpiresAt returns the token's `exp` claim as a UTC time.Time.
func (c *Claims) ExpiresAt() time.Time {
	return time.Unix(c.EXP, 0).UTC()
}

// RestrictedViewIDs returns scope.RestrictedViewIDs (empty when scope is nil).
func (c *Claims) RestrictedViewIDs() []uuid.UUID {
	if c.SessionScope == nil {
		return nil
	}
	out := make([]uuid.UUID, len(c.SessionScope.RestrictedViewIDs))
	copy(out, c.SessionScope.RestrictedViewIDs)
	return out
}

// ConsumerModeEnabled mirrors `Claims::consumer_mode_enabled` from Rust.
func (c *Claims) ConsumerModeEnabled() bool {
	return c.SessionScope != nil && c.SessionScope.ConsumerMode
}

// AllowsPath returns true when `path` is under one of the scope's
// allow-listed prefixes (or no scope is set).
func (c *Claims) AllowsPath(path string) bool {
	if c.SessionScope == nil || len(c.SessionScope.AllowedPathPrefixes) == 0 {
		return true
	}
	for _, prefix := range c.SessionScope.AllowedPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
