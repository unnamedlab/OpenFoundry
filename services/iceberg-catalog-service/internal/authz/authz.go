// Package authz is the ABAC engine consumed by every catalog handler
// that mutates or projects a markable resource.
//
// Rust uses Cedar; Go has no first-class Cedar binding so this port
// inlines the bundled iceberg-policy decision logic. The resulting
// `Engine.Enforce` returns a typed `*DenyError` on deny — handlers
// translate that into a 403 + audit event. The decision rules are:
//
//   1. The principal's tenant must equal the resource's tenant. When
//      the resource carries no tenant (legacy rows) we fall back to
//      `defaultTenant`.
//   2. Mutating actions (write/alter/drop/create/manage_markings)
//      require `api:iceberg-write`.
//   3. The principal must clear every marking on the resource.
//      Clearances come from `iceberg-clearance:<name>` scopes; the
//      `role:admin` and wildcard `iceberg-clearance:*` scopes expand
//      the full ladder (public/confidential/pii/restricted/secret).
//   4. Manage-markings + drop additionally require an elevated role
//      (`role:admin` or `role:editor`).
package authz

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/audit"
	"github.com/google/uuid"
)

// PrincipalKind matches the Cedar entity types declared in the bundled
// iceberg policies. The bearer extractor decides between them based on
// whether any scope starts with `svc:`.
type PrincipalKind int

const (
	// PrincipalUser denotes a human Foundry user — they carry roles +
	// clearances.
	PrincipalUser PrincipalKind = iota
	// PrincipalServicePrincipal denotes an `oauth-integration-service`
	// client — they carry an rid + project_scope_rids set, no roles.
	PrincipalServicePrincipal
)

func (k PrincipalKind) String() string {
	switch k {
	case PrincipalUser:
		return "User"
	case PrincipalServicePrincipal:
		return "ServicePrincipal"
	default:
		return "User"
	}
}

// PrincipalKindFromScopes infers the kind from the scope set. Service
// tokens carry at least one `svc:*` scope; everything else is a user.
func PrincipalKindFromScopes(scopes map[string]struct{}) PrincipalKind {
	for s := range scopes {
		if strings.HasPrefix(s, "svc:") {
			return PrincipalServicePrincipal
		}
	}
	return PrincipalUser
}

// Principal is the engine's view of the bearer extractor's
// authenticated principal. Held by value so the engine never mutates
// caller-owned state.
type Principal struct {
	Subject string
	Scopes  map[string]struct{}
	Kind    PrincipalKind
	Tenant  string
}

// HasScope reports whether the principal carries `scope` verbatim.
func (p *Principal) HasScope(scope string) bool {
	if p == nil {
		return false
	}
	_, ok := p.Scopes[scope]
	return ok
}

// HasAnyScope reports whether at least one of the supplied scopes is
// present. Convenient for "admin or editor" checks.
func (p *Principal) HasAnyScope(scopes ...string) bool {
	if p == nil {
		return false
	}
	for _, s := range scopes {
		if _, ok := p.Scopes[s]; ok {
			return true
		}
	}
	return false
}

// NamespaceAttrs is the engine's view of an iceberg namespace.
type NamespaceAttrs struct {
	RID        string
	ProjectRID string
	Tenant     string
	Name       string
	Markings   []string
}

// TableAttrs is the engine's view of an iceberg table. `Markings` is
// the effective union; `ExplicitMarkings` is the operator-managed
// subset (kept separate so policies can audit overrides).
type TableAttrs struct {
	RID              string
	NamespaceRID     string
	Tenant           string
	FormatVersion    int32
	Markings         []string
	ExplicitMarkings []string
}

// Resource is a discriminated union: only one of Namespace/Table is
// populated for any given Enforce call. Mirrors the Rust
// `AuthzResource` enum.
type Resource struct {
	Namespace *NamespaceAttrs
	Table     *TableAttrs
}

// NamespaceResource is a tiny constructor so handler call sites read
// nicely.
func NamespaceResource(attrs NamespaceAttrs) Resource {
	return Resource{Namespace: &attrs}
}

// TableResource mirrors NamespaceResource for tables.
func TableResource(attrs TableAttrs) Resource {
	return Resource{Table: &attrs}
}

func (r *Resource) tenant() string {
	switch {
	case r.Namespace != nil:
		return r.Namespace.Tenant
	case r.Table != nil:
		return r.Table.Tenant
	}
	return ""
}

func (r *Resource) markings() []string {
	switch {
	case r.Namespace != nil:
		return r.Namespace.Markings
	case r.Table != nil:
		return r.Table.Markings
	}
	return nil
}

func (r *Resource) targetRID() string {
	switch {
	case r.Namespace != nil:
		return r.Namespace.RID
	case r.Table != nil:
		return r.Table.RID
	}
	return ""
}

// DenialReason classifies a 403. Mirrors `audit::access_denied` reason
// labels so dashboards can split deny rates by cause.
type DenialReason int

const (
	// ReasonMissingClearance — resource has a marking the principal
	// doesn't clear.
	ReasonMissingClearance DenialReason = iota
	// ReasonMissingScope — mutating verb without `api:iceberg-write`.
	ReasonMissingScope
	// ReasonMissingRole — manage-markings/drop without `role:admin`
	// or `role:editor`.
	ReasonMissingRole
	// ReasonOutOfTenant — principal tenant differs from resource tenant.
	ReasonOutOfTenant
	// ReasonUnknown — fallthrough; usually means the policy bundle
	// declined to allow without naming a specific cause.
	ReasonUnknown
)

func (r DenialReason) String() string {
	switch r {
	case ReasonMissingClearance:
		return "missing_clearance"
	case ReasonMissingScope:
		return "missing_scope"
	case ReasonMissingRole:
		return "missing_role"
	case ReasonOutOfTenant:
		return "out_of_tenant"
	default:
		return "unknown"
	}
}

// DenyError is what Engine.Enforce returns on deny. Handlers translate
// into 403; the typed shape lets callers inspect the reason without
// string-matching.
type DenyError struct {
	Action string
	Reason DenialReason
}

func (e *DenyError) Error() string {
	return fmt.Sprintf("iceberg authz denied for `%s` (%s)", e.Action, e.Reason)
}

// Engine is the contract handlers depend on. Implemented by
// `PolicyEngine`; tests can substitute fakes that always allow / always
// deny.
type Engine interface {
	Enforce(ctx context.Context, principal *Principal, action string, resource Resource) error
}

// PolicyEngine is the bundled iceberg policy decision tree, ported from
// the Rust Cedar policies. Stateless once constructed.
type PolicyEngine struct {
	defaultTenant string
}

// NewPolicyEngine builds a PolicyEngine that falls back to `defaultTenant`
// for resources whose tenant column is empty (legacy rows seeded before
// tenancy landed).
func NewPolicyEngine(defaultTenant string) *PolicyEngine {
	if defaultTenant == "" {
		defaultTenant = "default"
	}
	return &PolicyEngine{defaultTenant: defaultTenant}
}

// Enforce evaluates the policy bundle against the supplied tuple. On
// allow it returns nil; on deny it emits an audit event + returns
// *DenyError so handlers can map to 403 (errors.As).
func (e *PolicyEngine) Enforce(ctx context.Context, principal *Principal, action string, resource Resource) error {
	_ = ctx
	if principal == nil {
		// No authenticated principal can never satisfy any policy.
		return &DenyError{Action: action, Reason: ReasonUnknown}
	}
	resourceTenant := resource.tenant()
	if resourceTenant == "" {
		resourceTenant = e.defaultTenant
	}
	principalTenant := principal.Tenant
	if principalTenant == "" {
		principalTenant = e.defaultTenant
	}

	allowed := true
	reason := ReasonUnknown

	if principalTenant != resourceTenant {
		allowed = false
		reason = ReasonOutOfTenant
	}

	if allowed && isMutatingAction(action) {
		if !principal.HasScope("api:iceberg-write") {
			allowed = false
			reason = ReasonMissingScope
		}
	}

	if allowed {
		cleared := principalClearances(principal)
		for _, m := range resource.markings() {
			if !contains(cleared, m) {
				allowed = false
				reason = ReasonMissingClearance
				break
			}
		}
	}

	if allowed && requiresElevatedRole(action) {
		if !principal.HasAnyScope("role:admin", "role:editor") {
			allowed = false
			reason = ReasonMissingRole
		}
	}

	if allowed {
		return nil
	}

	actor := uuid.Nil
	if id, err := uuid.Parse(principal.Subject); err == nil {
		actor = id
	}
	audit.AccessDenied(actor, resource.targetRID(), action, reason.String())
	return &DenyError{Action: action, Reason: reason}
}

// isMutatingAction returns true when an action carries any of the
// substring markers reserved for state-changing verbs.
func isMutatingAction(action string) bool {
	for _, marker := range []string{"write", "alter", "drop", "create", "manage_markings"} {
		if strings.Contains(action, marker) {
			return true
		}
	}
	return false
}

// requiresElevatedRole — manage-markings + drop are gated on
// admin/editor regardless of clearance ladder.
func requiresElevatedRole(action string) bool {
	return strings.Contains(action, "manage_markings") || strings.Contains(action, "drop")
}

// principalClearances unions the explicit `iceberg-clearance:<name>`
// scopes with the implicit ladder unlocked by `role:admin` /
// `iceberg-clearance:*`. Result is sorted + deduped so denial
// comparisons are stable.
func principalClearances(p *Principal) []string {
	out := make([]string, 0, len(p.Scopes))
	wildcard := false
	for s := range p.Scopes {
		if name, ok := strings.CutPrefix(s, "iceberg-clearance:"); ok {
			out = append(out, name)
		}
		if s == "role:admin" || s == "iceberg-clearance:*" {
			wildcard = true
		}
	}
	if wildcard {
		out = append(out, "public", "confidential", "pii", "restricted", "secret")
	}
	sort.Strings(out)
	return dedupSorted(out)
}

// contains is a linear-scan helper. Marking lists are tiny (≤ low
// double-digits) so a hashmap would cost more than it saves.
func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func dedupSorted(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
