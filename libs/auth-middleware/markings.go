package authmw

import (
	"fmt"

	"github.com/openfoundry/openfoundry-go/libs/core-models/security"
)

// CallerClearances is the per-request set of marking ids the caller
// is cleared for. Mirrors libs/auth-middleware/src/markings.rs.
//
// Built via [CallerClearancesFromClaims] (resolver-backed) or
// [CallerClearancesFromClaimsNamesOnly] (legacy string ladder).
//
// Why the indirection: [Claims.AllowedMarkings] returns string
// names (legacy ladder `public ⊂ confidential ⊂ pii`), but the
// source-of-truth `dataset_markings` table stores [security.MarkingID]s.
// [MarkingNameResolver] bridges the two until the JWT issuer
// (identity-federation-service) emits typed UUIDs natively.
type CallerClearances struct {
	ids map[security.MarkingID]struct{}
	// names is the original lowercase string set — kept so legacy
	// handlers that haven't migrated to typed ids can still call
	// `claims.allows_marking(&str)`-style checks via this struct.
	names map[string]struct{}
	// admin bypasses enforcement entirely (mirrors
	// [Claims.AllowsMarking]).
	admin bool
}

// MarkingNameResolver maps a marking *name* (case-insensitive) to
// its catalogued [security.MarkingID]. Implementations typically
// cache this mapping in memory since the markings table is tiny
// and rarely changes.
type MarkingNameResolver interface {
	Resolve(name string) (security.MarkingID, bool)
}

// CallerClearancesFromClaims builds clearances from claims,
// resolving each marking name into its [security.MarkingID] via
// resolver. Names the resolver doesn't know are still kept in the
// name set so a legacy string-only enforcement can still grant
// access.
func CallerClearancesFromClaims(claims *Claims, resolver MarkingNameResolver) CallerClearances {
	cc := CallerClearances{
		ids:   make(map[security.MarkingID]struct{}),
		names: make(map[string]struct{}),
		admin: claims.HasRole("admin"),
	}
	for _, n := range claims.AllowedMarkings() {
		lower := asciiToLower(n)
		cc.names[lower] = struct{}{}
		if id, ok := resolver.Resolve(lower); ok {
			cc.ids[id] = struct{}{}
		}
	}
	return cc
}

// CallerClearancesFromClaimsNamesOnly is the resolver-free
// constructor — keeps the string ladder only. Useful for tests and
// for call sites that don't want to plumb a resolver.
func CallerClearancesFromClaimsNamesOnly(claims *Claims) CallerClearances {
	cc := CallerClearances{
		ids:   make(map[security.MarkingID]struct{}),
		names: make(map[string]struct{}),
		admin: claims.HasRole("admin"),
	}
	for _, n := range claims.AllowedMarkings() {
		cc.names[asciiToLower(n)] = struct{}{}
	}
	return cc
}

// IsAdmin reports whether the subject's role bypasses enforcement.
func (c *CallerClearances) IsAdmin() bool { return c.admin }

// IDs returns the cleared marking-id set as a fresh slice (caller
// owns the result; modifying it does not affect the receiver).
func (c *CallerClearances) IDs() []security.MarkingID {
	out := make([]security.MarkingID, 0, len(c.ids))
	for id := range c.ids {
		out = append(out, id)
	}
	return out
}

// Names returns the cleared lowercase marking-name set as a fresh
// slice.
func (c *CallerClearances) Names() []string {
	out := make([]string, 0, len(c.names))
	for n := range c.names {
		out = append(out, n)
	}
	return out
}

// AllowsID reports whether the caller is cleared for markingID.
func (c *CallerClearances) AllowsID(markingID security.MarkingID) bool {
	if c.admin {
		return true
	}
	_, ok := c.ids[markingID]
	return ok
}

// AllowsName reports whether the caller is cleared for the marking
// name (case-insensitive). Useful while the JWT only carries names.
func (c *CallerClearances) AllowsName(markingName string) bool {
	if c.admin {
		return true
	}
	_, ok := c.names[asciiToLower(markingName)]
	return ok
}

// StaticMarkingNameResolver is a fixed name → id map suitable for
// tests and bootstrap.
type StaticMarkingNameResolver struct {
	m map[string]security.MarkingID
}

// NewStaticMarkingNameResolver builds a resolver from `entries`.
// Names are lowercased on insertion so [Resolve] is
// case-insensitive without having to re-walk the map.
func NewStaticMarkingNameResolver(entries map[string]security.MarkingID) *StaticMarkingNameResolver {
	out := make(map[string]security.MarkingID, len(entries))
	for k, v := range entries {
		out[asciiToLower(k)] = v
	}
	return &StaticMarkingNameResolver{m: out}
}

// Resolve returns the catalogued id for `name` (case-insensitive).
func (r *StaticMarkingNameResolver) Resolve(name string) (security.MarkingID, bool) {
	id, ok := r.m[asciiToLower(name)]
	return id, ok
}

// MarkingEnforcementError is returned by [EnforceMarkings] when the
// caller is missing one or more markings required by the dataset.
//
// Missing carries the offending [security.MarkingID]s — the handler
// should map this to HTTP 403 and log the set for audit.
type MarkingEnforcementError struct {
	Missing []security.MarkingID
}

func (e *MarkingEnforcementError) Error() string {
	names := make([]string, len(e.Missing))
	for i, id := range e.Missing {
		names[i] = id.String()
	}
	return fmt.Sprintf("forbidden: missing markings %v", names)
}

// EnforceMarkings rejects when `effectiveMarkings - clearances ≠ ∅`.
//
// Returns nil when the caller is cleared for *every* marking on
// the dataset (direct + inherited). The check is intentionally
// strict: inheriting RESTRICTED from any upstream gates the entire
// dataset.
func EnforceMarkings(effectiveMarkings []security.MarkingID, clearances *CallerClearances) error {
	if clearances.IsAdmin() {
		return nil
	}
	var missing []security.MarkingID
	for _, id := range effectiveMarkings {
		if _, ok := clearances.ids[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &MarkingEnforcementError{Missing: missing}
}
