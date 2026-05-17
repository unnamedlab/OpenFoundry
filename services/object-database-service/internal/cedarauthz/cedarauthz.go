// Package cedarauthz wires Cedar policy enforcement for the ontology
// runtime endpoints in object-database-service.
//
// The bundled `cedar_schema.cedarschema` in libs/authz-cedar-go already
// declares an `Object` entity with `type`, `tenant`, `markings`; this
// package reuses that declaration and exposes a tiny per-action gate
// (`object_type::read|write|delete|link_read|link_write`) that handlers
// call before any read or mutation. Resource identity is the
// object-type RID (a stable, type-level handle) so callers can grant
// the same policy to every instance of a given object type without
// enumerating ids — same shape Foundry's runtime uses for its
// `Ontology Object Type Permission` ACLs.
//
// The gate is **optional**: when the engine is nil (dev/test/no JWT),
// `CheckObjectType` is a no-op. Production wiring loads the engine in
// main.go from `pg-policy.cedar_policies` per ADR-0027.
package cedarauthz

import (
	"context"
	"fmt"
	"sort"

	cedar "github.com/cedar-policy/cedar-go"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthzlib "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// Action UIDs — single source of truth so handlers don't sprinkle
// string literals.

// ActionRead returns the Cedar UID for `Action::"object_type::read"`.
// Covers list / get / search / traverse on the type's instances.
func ActionRead() cedar.EntityUID {
	return cedarauthzlib.MustEntityUID("Action", "object_type::read")
}

// ActionWrite returns the Cedar UID for `Action::"object_type::write"`.
// Covers create + update on the type's instances.
func ActionWrite() cedar.EntityUID {
	return cedarauthzlib.MustEntityUID("Action", "object_type::write")
}

// ActionDelete returns the Cedar UID for
// `Action::"object_type::delete"`. Distinct from write so a "writer"
// role can be granted without delete authority (matches Foundry's
// "edit" vs "manage" split for object types).
func ActionDelete() cedar.EntityUID {
	return cedarauthzlib.MustEntityUID("Action", "object_type::delete")
}

// ActionLinkRead returns the Cedar UID for `Action::"object_type::link_read"`,
// fired by TraverseLinks against the source type.
func ActionLinkRead() cedar.EntityUID {
	return cedarauthzlib.MustEntityUID("Action", "object_type::link_read")
}

// ActionLinkWrite returns the Cedar UID for `Action::"object_type::link_write"`,
// fired when a link is inserted between two objects.
func ActionLinkWrite() cedar.EntityUID {
	return cedarauthzlib.MustEntityUID("Action", "object_type::link_write")
}

// BuildObjectTypeEntity hydrates the Cedar `Object` entity for an
// object-type RID. The schema's `Object` entity has `{type, tenant,
// markings}`; for the per-type guard we set:
//   - `type`     = the RID itself (so policies can match
//     `resource.type == "ri.ontology.main.object-type.{uuid}"`),
//   - `tenant`   = caller tenant (or empty for legacy in-memory mode),
//   - `markings` = the type-level marking allowlist (the union of
//     marking requirements declared on the type's properties). When
//     unknown to this service (no client wiring yet) the set is empty
//     and the clearance check trivially passes.
func BuildObjectTypeEntity(objectTypeRID, tenant string, markings []string) cedar.Entity {
	uid := cedarauthzlib.MustEntityUID("Object", objectTypeRID)
	values := make([]cedar.Value, 0, len(markings))
	for _, m := range dedupeLower(markings) {
		values = append(values, cedarauthzlib.MustEntityUID("Marking", m))
	}
	attrs := cedar.NewRecord(cedar.RecordMap{
		"type":     cedar.String(objectTypeRID),
		"tenant":   cedar.String(tenant),
		"markings": cedar.NewSet(values...),
	})
	return cedar.Entity{UID: uid, Attributes: attrs}
}

// Engine wraps the shared Cedar policy engine for object-type checks.
// All methods are no-ops when the receiver is nil so callers can pass
// `nil` in tests or in environments that don't wire policies.
type Engine struct {
	Engine *cedarauthzlib.AuthzEngine
}

// NewEngine returns a wrapper. nil-safe.
func NewEngine(engine *cedarauthzlib.AuthzEngine) *Engine {
	if engine == nil {
		return nil
	}
	return &Engine{Engine: engine}
}

// CheckObjectType evaluates `action` over the given object-type
// resource. Returns nil on Allow (and on a nil receiver / nil claims —
// gating is opt-in). Returns &ErrForbidden on Deny.
func (e *Engine) CheckObjectType(
	ctx context.Context,
	claims *authmw.Claims,
	action cedar.EntityUID,
	objectTypeRID string,
	markings []string,
) error {
	if e == nil || e.Engine == nil {
		return nil
	}
	if claims == nil {
		// No identity → no decision; let the existing gateway layer
		// reject anonymous requests. Returning Allow here keeps the
		// no-auth dev path working.
		return nil
	}
	principal := cedarauthzlib.PrincipalEntityFromClaims(claims)
	tenant := tenantFromClaims(claims)
	resource := BuildObjectTypeEntity(objectTypeRID, tenant, markings)
	clearances := callerClearances(claims)

	entities := buildEntitySet(principal, resource, markings, clearances)
	outcome, err := e.Engine.Authorize(ctx, principal.UID, action, resource.UID, cedar.Record{}, entities)
	if err != nil {
		return fmt.Errorf("authz: %w", err)
	}
	if outcome.IsAllow() {
		return nil
	}
	return forbiddenWithMissing(markings, clearances, claims)
}

// ── helpers ──────────────────────────────────────────────────────

func buildEntitySet(principal cedar.Entity, resource cedar.Entity, markingLists ...[]string) cedar.EntityMap {
	out := cedar.EntityMap{principal.UID: principal, resource.UID: resource}
	seen := map[string]struct{}{}
	for _, list := range markingLists {
		for _, m := range list {
			l := lower(m)
			if l == "" {
				continue
			}
			if _, ok := seen[l]; ok {
				continue
			}
			seen[l] = struct{}{}
			uid := cedarauthzlib.MustEntityUID("Marking", l)
			out[uid] = cedar.Entity{
				UID:        uid,
				Attributes: cedar.NewRecord(cedar.RecordMap{"name": cedar.String(l)}),
			}
		}
	}
	return out
}

func tenantFromClaims(claims *authmw.Claims) string {
	if claims == nil || claims.OrgID == nil {
		return ""
	}
	return claims.OrgID.String()
}

func callerClearances(claims *authmw.Claims) []string {
	if claims == nil {
		return nil
	}
	return append([]string(nil), claims.AllowedMarkings()...)
}

func dedupeLower(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, m := range in {
		l := lower(m)
		if l == "" {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func lower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		out[i] = c
	}
	return string(out)
}

// ErrForbidden surfaces the missing markings to the HTTP layer so the
// 403 carries a precise reason ("missing clearance: SECRET") rather
// than a generic "denied". Mirrors the media-sets-service shape.
type ErrForbidden struct {
	Missing []string
	Generic bool
}

func (e *ErrForbidden) Error() string {
	if e.Generic || len(e.Missing) == 0 {
		return "denied by policy"
	}
	out := "missing clearance: "
	for i, m := range e.Missing {
		if i > 0 {
			out += ", "
		}
		out += upper(m)
	}
	return out
}

func forbiddenWithMissing(required, clearances []string, claims *authmw.Claims) error {
	if claims != nil && claims.HasRole("admin") && !claims.HasActiveMarkingScope() {
		return &ErrForbidden{Generic: true}
	}
	owned := map[string]struct{}{}
	for _, c := range clearances {
		owned[lower(c)] = struct{}{}
	}
	missing := make([]string, 0, len(required))
	for _, r := range required {
		l := lower(r)
		if _, ok := owned[l]; !ok {
			missing = append(missing, l)
		}
	}
	sort.Strings(missing)
	if len(missing) == 0 {
		return &ErrForbidden{Generic: true}
	}
	return &ErrForbidden{Missing: missing}
}

func upper(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c = c - ('a' - 'A')
		}
		out[i] = c
	}
	return string(out)
}
