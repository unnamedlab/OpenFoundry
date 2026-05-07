package cedarauthz

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/cedar-policy/cedar-go/types"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	authzcedar "github.com/openfoundry/openfoundry-go/libs/authz-cedar-go"
)

// ─── Action UIDs ─────────────────────────────────────────────────────
//
// Mirrors the Rust action_marker! macro invocations. Statically-known
// EntityUIDs ready to pass into AdminGuard.

// ActionRotateJwks is the Action::"rotate_jwks" UID.
var ActionRotateJwks = types.NewEntityUID("Action", "rotate_jwks")

// ActionRetireJwks is the Action::"retire_jwks" UID.
var ActionRetireJwks = types.NewEntityUID("Action", "retire_jwks")

// ActionScimProvisionUser is the Action::"scim_provision_user" UID.
var ActionScimProvisionUser = types.NewEntityUID("Action", "scim_provision_user")

// ActionScimDeprovisionUser is the Action::"scim_deprovision_user" UID.
// Note: the Rust source intentionally maps both
// scim_deprovision_user and scim_provision_user to the same Cedar
// action id ("scim_provision_user") — verbatim port preserves
// that.
var ActionScimDeprovisionUser = types.NewEntityUID("Action", "scim_provision_user")

// ActionScimProvisionGroup is the Action::"scim_provision_group" UID.
var ActionScimProvisionGroup = types.NewEntityUID("Action", "scim_provision_group")

// ─── Resource extractors ─────────────────────────────────────────────

// JwksKeyResource produces the stand-in resource for JWKS rotation /
// retirement — admin endpoints operate on the *current* signing
// bundle, not on a single kid.
func JwksKeyResource(_ *http.Request) (cedar.EntityUID, []cedar.Entity, error) {
	uid := types.NewEntityUID("JwksKey", "_active")
	entity := cedar.Entity{
		UID: uid,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"kid":    cedar.String("_active"),
			"status": cedar.String("active"),
		}),
	}
	return uid, []cedar.Entity{entity}, nil
}

// ScimUserResource produces the stand-in resource for SCIM user
// provisioning / deprovisioning.
func ScimUserResource(_ *http.Request) (cedar.EntityUID, []cedar.Entity, error) {
	uid := types.NewEntityUID("ScimUser", "_pool")
	entity := cedar.Entity{
		UID: uid,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"id": cedar.String("_pool"),
		}),
	}
	return uid, []cedar.Entity{entity}, nil
}

// ScimGroupResource produces the stand-in resource for SCIM group
// provisioning.
func ScimGroupResource(_ *http.Request) (cedar.EntityUID, []cedar.Entity, error) {
	uid := types.NewEntityUID("ScimGroup", "_pool")
	entity := cedar.Entity{
		UID: uid,
		Attributes: cedar.NewRecord(cedar.RecordMap{
			"id": cedar.String("_pool"),
		}),
	}
	return uid, []cedar.Entity{entity}, nil
}

// ─── AdminAuthzGuard middleware ──────────────────────────────────────

// guardErrorBody is the JSON shape used for guard rejections —
// mirrors the auth-middleware error envelope so API clients see a
// single schema across the auth chain.
type guardErrorBody struct {
	Error string `json:"error"`
}

func writeGuardError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(guardErrorBody{Error: msg})
}

// AdminGuard returns a chi middleware that enforces a single Cedar
// admin action against the resource extracted from the request.
//
// Differs from authzcedar.Guard in one important way: it reads
// `kind` (e.g. "service_account" / "human"), `mfa_age_secs` (Long),
// `groups` (array of strings) and `roles` from claims.Attributes
// and propagates each role / group into the principal's parent UID
// set so policies that say `principal in Group::"…"` /
// `principal in Role::"…"` resolve.
//
// Wiring (matches Rust AuthzGuard<Action, Resource> extractor):
//
//  1. Mount auth-middleware so claims are in context.
//  2. Mount cedarauthz.EngineMiddleware so the engine is in context.
//  3. Mount AdminGuard(action, resourceFn) on the routes that need it.
//
// Behaviour:
//   - 401 Unauthorized when no claims are in context.
//   - 500 Internal Server Error when no engine is in context.
//   - 400 Bad Request when resourceFn returns an error.
//   - 403 Forbidden when the engine denies.
//   - Pass-through (next.ServeHTTP) when allowed.
//
// Mirrors fn AdminAuthzGuard verbatim.
func AdminGuard(action cedar.EntityUID, resourceFn authzcedar.ResourceFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authmw.FromContext(r.Context())
			if !ok {
				writeGuardError(w, http.StatusUnauthorized, "missing Claims")
				return
			}
			engine, ok := authzcedar.EngineFromContext(r.Context())
			if !ok {
				slog.Error("cedarauthz admin guard: no AuthzEngine in context")
				writeGuardError(w, http.StatusInternalServerError, "AuthzEngine not configured")
				return
			}

			resourceUID, resourceEntities, err := resourceFn(r)
			if err != nil {
				writeGuardError(w, http.StatusBadRequest, err.Error())
				return
			}

			principalEntity, parentEntities := PrincipalEntitiesFromClaims(claims)
			ents := cedar.EntityMap{principalEntity.UID: principalEntity}
			for _, e := range parentEntities {
				ents[e.UID] = e
			}
			for _, e := range resourceEntities {
				ents[e.UID] = e
			}

			outcome, err := engine.Authorize(
				r.Context(),
				principalEntity.UID,
				action,
				resourceUID,
				cedar.NewRecord(cedar.RecordMap{}),
				ents,
			)
			if err != nil {
				slog.Error("cedarauthz admin guard: engine error", slog.String("error", err.Error()))
				writeGuardError(w, http.StatusInternalServerError, "authz error: "+err.Error())
				return
			}
			if !outcome.IsAllow() {
				writeGuardError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ─── Principal hydration ─────────────────────────────────────────────

// PrincipalEntitiesFromClaims builds the principal `User` entity
// plus its referenced `Group` / `Role` parent entities from a JWT
// Claims set.
//
// Inputs read off claims:
//   - Sub, OrgID, Roles, SessionScope.AllowedMarkings → baseline
//     ABAC fields (mirrors authzcedar.PrincipalEntityFromClaims).
//   - Attributes.kind        → principal.kind (typically
//     "service_account" or "human").
//   - Attributes.mfa_age_secs→ principal.mfa_age_secs (Long).
//   - Attributes.groups: [..]→ each value becomes a Group::"<id>"
//     parent UID *and* an emitted Group entity so Cedar's
//     `principal in Group::"…"` resolves without external lookups.
//
// Claims.Roles additionally contributes Role::"<role>" parent UIDs
// (and matching Role entities), letting policies write
// `principal in Role::"scim_writer"`.
//
// Mirrors fn principal_entities_from_claims.
func PrincipalEntitiesFromClaims(claims *authmw.Claims) (cedar.Entity, []cedar.Entity) {
	userUID := types.NewEntityUID("User", types.String(claims.Sub.String()))

	tenant := ""
	if claims.OrgID != nil {
		tenant = claims.OrgID.String()
	}

	clearances := make([]cedar.Value, 0)
	if claims.SessionScope != nil {
		for _, m := range claims.SessionScope.AllowedMarkings {
			clearances = append(clearances, types.NewEntityUID("Marking", types.String(m)))
		}
	}

	rolesSet := make([]cedar.Value, 0, len(claims.Roles))
	for _, r := range claims.Roles {
		rolesSet = append(rolesSet, cedar.String(r))
	}

	attrs := cedar.RecordMap{
		"tenant":     cedar.String(tenant),
		"clearances": cedar.NewSet(clearances...),
		"roles":      cedar.NewSet(rolesSet...),
	}

	// Decode claims.Attributes once and pluck the well-known keys.
	rawAttrs := decodeAttributes(claims.Attributes)
	if kind := stringAttr(rawAttrs, "kind"); kind != "" {
		attrs["kind"] = cedar.String(kind)
	}
	if mfaAge, ok := int64Attr(rawAttrs, "mfa_age_secs"); ok {
		attrs["mfa_age_secs"] = cedar.Long(mfaAge)
	}

	claimGroups := stringArrayAttr(rawAttrs, "groups")

	parents := make([]cedar.EntityUID, 0)
	parentEntities := make([]cedar.Entity, 0)
	for _, gid := range claimGroups {
		g := types.NewEntityUID("Group", types.String(gid))
		parents = append(parents, g)
		parentEntities = append(parentEntities, cedar.Entity{
			UID: g,
			Attributes: cedar.NewRecord(cedar.RecordMap{
				"id": cedar.String(gid),
			}),
		})
	}
	for _, role := range claims.Roles {
		rUID := types.NewEntityUID("Role", types.String(role))
		parents = append(parents, rUID)
		parentEntities = append(parentEntities, cedar.Entity{
			UID: rUID,
			Attributes: cedar.NewRecord(cedar.RecordMap{
				"id": cedar.String(role),
			}),
		})
	}

	user := cedar.Entity{
		UID:        userUID,
		Attributes: cedar.NewRecord(attrs),
		Parents:    types.NewEntityUIDSet(parents...),
	}
	return user, parentEntities
}

// decodeAttributes parses claims.Attributes (json.RawMessage) into a
// map[string]any once. Returns an empty map on absent / malformed
// input — the caller's nil checks handle the missing-key case.
func decodeAttributes(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

func stringAttr(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func int64Attr(m map[string]any, key string) (int64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	}
	return 0, false
}

func stringArrayAttr(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// ─── Sentinel ────────────────────────────────────────────────────────

// ErrAuthzEngineNotConfigured is the canonical error for the
// engine-not-in-context branch of AdminGuard. Tests assert against
// it; callers in the wiring code should never observe it.
var ErrAuthzEngineNotConfigured = errors.New("AuthzEngine not configured")

// silence — context unused at file-scope, only via closures.
var _ context.Context