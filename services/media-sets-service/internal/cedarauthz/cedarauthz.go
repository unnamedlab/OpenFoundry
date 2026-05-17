// Package cedarauthz wires Cedar policy enforcement for
// media-sets-service.
//   - Entity hydration helpers for MediaSet + MediaItem (the schema
//     for these entities is already declared in libs/authz-cedar-go's
//     bundled cedarschema; this package only ships the policies).
//   - CheckMediaSet / CheckMediaItem operations the handlers call
//     before any state mutation. CheckMediaItem composes the parent-set
//     view check so a viewer-blocked set cascades to every item.
//   - EffectiveItemMarkings: lowercased union of parent + own markings.
//
// The package depends on libs/authz-cedar-go for the engine + schema +
// PrincipalEntityFromClaims.
package cedarauthz

import (
	"context"
	_ "embed"
	"fmt"
	"sort"

	cedar "github.com/cedar-policy/cedar-go"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	cedarauthz "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

// MediaSetsPoliciesSource is the Cedar source for the 6 default
// policies. Bundled at compile time.
//
//go:embed media_sets.cedar
var MediaSetsPoliciesSource string

// BundledPolicyRecords parses MediaSetsPoliciesSource into PolicyRecords
// keyed by their @id annotation. Mirrors the helper used by the
// identity-federation cedarauthz package.
func BundledPolicyRecords() ([]cedarauthz.PolicyRecord, error) {
	parsed, err := cedar.NewPolicySetFromBytes("media_sets.cedar", []byte(MediaSetsPoliciesSource))
	if err != nil {
		return nil, fmt.Errorf("parse media_sets.cedar: %w", err)
	}
	out := make([]cedarauthz.PolicyRecord, 0, len(parsed.Map()))
	for synthID, policy := range parsed.Map() {
		id := string(policy.Annotations()["id"])
		if id == "" {
			id = string(synthID)
		}
		// Re-marshal the policy back to text so the PolicyRecord
		// round-trip stays validatable by the store.
		out = append(out, cedarauthz.PolicyRecord{
			ID:      id,
			Version: 1,
			Source:  string(policy.MarshalCedar()),
		})
	}
	// Stable order so tests are deterministic.
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ── Action UIDs ────────────────────────────────────────────────────

// ActionView returns the Cedar UID for media_set::view.
func ActionView() cedar.EntityUID {
	return cedarauthz.MustEntityUID("Action", "media_set::view")
}

// ActionManage returns the Cedar UID for media_set::manage.
func ActionManage() cedar.EntityUID {
	return cedarauthz.MustEntityUID("Action", "media_set::manage")
}

// ActionDeleteSet returns the Cedar UID for media_set::delete.
func ActionDeleteSet() cedar.EntityUID {
	return cedarauthz.MustEntityUID("Action", "media_set::delete")
}

// ActionItemRead returns the Cedar UID for media_item::read.
func ActionItemRead() cedar.EntityUID {
	return cedarauthz.MustEntityUID("Action", "media_item::read")
}

// ActionItemWrite returns the Cedar UID for media_item::write.
func ActionItemWrite() cedar.EntityUID {
	return cedarauthz.MustEntityUID("Action", "media_item::write")
}

// ActionItemDelete returns the Cedar UID for media_item::delete.
func ActionItemDelete() cedar.EntityUID {
	return cedarauthz.MustEntityUID("Action", "media_item::delete")
}

// ── Entity hydration ──────────────────────────────────────────────

// BuildMediaSetEntity hydrates the Cedar entity for a MediaSet row.
// Markings are emitted as Marking::"<lowercased>" UIDs to match the
// User.clearances side and the bundled schema's Set<Marking> typing.
func BuildMediaSetEntity(set *models.MediaSet, tenant string) cedar.Entity {
	uid := cedarauthz.MustEntityUID("MediaSet", set.RID)
	markings := make([]cedar.Value, 0, len(set.Markings))
	for _, m := range set.Markings {
		markings = append(markings, cedarauthz.MustEntityUID("Marking", lower(m)))
	}
	attrs := cedar.NewRecord(cedar.RecordMap{
		"rid":                cedar.String(set.RID),
		"tenant":             cedar.String(tenant),
		"project_rid":        cedar.String(set.ProjectRID),
		"transaction_policy": cedar.String(set.TransactionPolicy),
		"virtual":            cedar.Boolean(set.Virtual),
		"markings":           cedar.NewSet(markings...),
	})
	return cedar.Entity{UID: uid, Attributes: attrs}
}

// BuildMediaItemEntity hydrates the MediaItem entity, unioning parent
// set markings into the effective item markings (Foundry "default
// inheritance"). The entity's parent UID is the parent MediaSet so
// `it in MediaSet::"…"` policies work without an attribute walk.
func BuildMediaItemEntity(item *models.MediaItem, parent *models.MediaSet, tenant string) cedar.Entity {
	uid := cedarauthz.MustEntityUID("MediaItem", item.RID)
	parentUID := cedarauthz.MustEntityUID("MediaSet", item.MediaSetRID)
	effective := EffectiveItemMarkings(parent, item)
	markings := make([]cedar.Value, 0, len(effective))
	for _, m := range effective {
		markings = append(markings, cedarauthz.MustEntityUID("Marking", m))
	}
	attrs := cedar.NewRecord(cedar.RecordMap{
		"media_set_rid": cedar.String(item.MediaSetRID),
		"tenant":        cedar.String(tenant),
		"mime_type":     cedar.String(item.MimeType),
		"size_bytes":    cedar.Long(item.SizeBytes),
		"markings":      cedar.NewSet(markings...),
	})
	return cedar.Entity{UID: uid, Attributes: attrs, Parents: cedar.NewEntityUIDSet(parentUID)}
}

// buildMarkingEntity is the synthetic Marking entity the engine needs
// in scope so principal.clearances and resource.markings UIDs resolve.
func buildMarkingEntity(name string) cedar.Entity {
	uid := cedarauthz.MustEntityUID("Marking", name)
	attrs := cedar.NewRecord(cedar.RecordMap{"name": cedar.String(name)})
	return cedar.Entity{UID: uid, Attributes: attrs}
}

// ── Effective markings ────────────────────────────────────────────

// EffectiveItemMarkings returns the lowercased, deduplicated, sorted
// union of `parent.markings ∪ item.markings`. Surfaces the same set
// the policy engine evaluates against.
func EffectiveItemMarkings(parent *models.MediaSet, item *models.MediaItem) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parent.Markings)+len(item.Markings))
	for _, m := range parent.Markings {
		l := lower(m)
		if _, ok := seen[l]; !ok {
			seen[l] = struct{}{}
			out = append(out, l)
		}
	}
	for _, m := range item.Markings {
		l := lower(m)
		if _, ok := seen[l]; !ok {
			seen[l] = struct{}{}
			out = append(out, l)
		}
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

// ── Engine wrapper ────────────────────────────────────────────────

// Engine bundles the policy engine + tenant resolver. Tests can swap
// out the underlying engine with NewEngineNoopAudit; production wires
// it to the kafka/slog audit sink in main.
type Engine struct {
	Engine *cedarauthz.AuthzEngine
}

// NewEngine returns a wrapper over an existing AuthzEngine.
func NewEngine(engine *cedarauthz.AuthzEngine) *Engine { return &Engine{Engine: engine} }

// CheckMediaSet runs the Cedar gate for `action` over `set`. Returns
// nil on Allow, &ErrForbidden{Missing: …} on Deny.
func (e *Engine) CheckMediaSet(ctx context.Context, claims *authmw.Claims, action cedar.EntityUID, set *models.MediaSet) error {
	principal := cedarauthz.PrincipalEntityFromClaims(claims)
	tenant := tenantFromClaims(claims)
	resource := BuildMediaSetEntity(set, tenant)
	clearances := callerClearances(claims)
	entities := buildEntitySet(principal, []cedar.Entity{resource}, set.Markings, clearances)

	outcome, err := e.Engine.Authorize(ctx, principal.UID, action, resource.UID, cedar.Record{}, entities)
	if err != nil {
		return fmt.Errorf("authz: %w", err)
	}
	if outcome.IsAllow() {
		return nil
	}
	return forbiddenWithMissing(set.Markings, clearances, claims)
}

// CheckMediaItem runs the Cedar gate over an item, after enforcing the
// parent-set view check. Mirrors the Rust contract:
//
//	(User can view it.media_set) && user.clearances containsAll it.markings
func (e *Engine) CheckMediaItem(ctx context.Context, claims *authmw.Claims, action cedar.EntityUID, item *models.MediaItem, parent *models.MediaSet) error {
	if err := e.CheckMediaSet(ctx, claims, ActionView(), parent); err != nil {
		return err
	}
	principal := cedarauthz.PrincipalEntityFromClaims(claims)
	tenant := tenantFromClaims(claims)
	setEntity := BuildMediaSetEntity(parent, tenant)
	itemEntity := BuildMediaItemEntity(item, parent, tenant)
	clearances := callerClearances(claims)

	all := append(parent.Markings, item.Markings...)
	entities := buildEntitySet(principal, []cedar.Entity{setEntity, itemEntity}, all, clearances)
	outcome, err := e.Engine.Authorize(ctx, principal.UID, action, itemEntity.UID, cedar.Record{}, entities)
	if err != nil {
		return fmt.Errorf("authz: %w", err)
	}
	if outcome.IsAllow() {
		return nil
	}
	effective := EffectiveItemMarkings(parent, item)
	return forbiddenWithMissing(effective, clearances, claims)
}

// buildEntitySet composes the Cedar entity set, materialising the
// principal, the resources, plus a Marking entity per (lowercased)
// marking referenced by either resource markings or caller clearances.
func buildEntitySet(principal cedar.Entity, resources []cedar.Entity, markingLists ...[]string) cedar.EntityMap {
	all := make([]cedar.Entity, 0, 1+len(resources)+8)
	all = append(all, principal)
	all = append(all, resources...)
	seen := map[string]struct{}{}
	for _, list := range markingLists {
		for _, m := range list {
			l := lower(m)
			if _, ok := seen[l]; ok {
				continue
			}
			seen[l] = struct{}{}
			all = append(all, buildMarkingEntity(l))
		}
	}
	out := cedar.EntityMap{}
	for _, e := range all {
		out[e.UID] = e
	}
	return out
}

func tenantFromClaims(claims *authmw.Claims) string {
	if claims.OrgID == nil {
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

// ── Forbidden error with precise reason ───────────────────────────

// ErrForbidden carries the missing markings so the HTTP layer surfaces
// a precise 403 ("missing clearance: SECRET") rather than a generic
// "denied".
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
